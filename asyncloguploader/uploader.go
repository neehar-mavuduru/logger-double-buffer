package asyncloguploader

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

// Note: GCSUploadConfig is now defined in config.go
// This file uses GCSUploadConfig from the config package

// Uploader handles uploading completed log files to GCS
type Uploader struct {
	config      GCSUploadConfig
	client      *storage.Client
	uploadChan  chan string
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	uploadStats Stats
	statsMu     sync.RWMutex
	chunkMgr    *ChunkManager
}

// Stats tracks upload statistics
type Stats struct {
	TotalFiles     int64
	Successful     int64
	Failed         int64
	TotalBytes     int64
	TotalDuration  time.Duration
	LastUploadTime time.Time
}

// NewUploader creates a new GCS uploader service
func NewUploader(config GCSUploadConfig) (*Uploader, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create GCS client with gRPC pool
	client, err := storage.NewClient(ctx,
		option.WithGRPCConnectionPool(config.GRPCPoolSize),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create storage client: %w", err)
	}

	uploader := &Uploader{
		config:     config,
		client:     client,
		uploadChan: make(chan string, config.ChannelBufferSize),
		ctx:        ctx,
		cancel:     cancel,
		chunkMgr:   NewChunkManager(config.MaxChunksPerCompose),
	}

	return uploader, nil
}

// Start starts the uploader service (reads from channel and uploads files)
func (u *Uploader) Start() {
	u.wg.Add(1)
	go u.uploadWorker()
}

// Stop stops the uploader service gracefully
func (u *Uploader) Stop() {
	close(u.uploadChan)
	u.cancel()
	u.wg.Wait()
	u.client.Close()
}

// GetUploadChannel returns the channel to send file paths for upload
func (u *Uploader) GetUploadChannel() chan<- string {
	return u.uploadChan
}

// GetStats returns current upload statistics
func (u *Uploader) GetStats() Stats {
	u.statsMu.RLock()
	defer u.statsMu.RUnlock()
	return u.uploadStats
}

// uploadWorker reads from channel and uploads files
func (u *Uploader) uploadWorker() {
	defer u.wg.Done()

	for filePath := range u.uploadChan {
		if filePath == "" {
			continue
		}

		// Upload file with retries
		if err := u.uploadFileWithRetry(filePath); err != nil {
			log.Printf("[ERROR] Failed to upload %s after %d retries: %v", filePath, u.config.MaxRetries, err)
			u.statsMu.Lock()
			u.uploadStats.Failed++
			u.statsMu.Unlock()
		} else {
			u.statsMu.Lock()
			u.uploadStats.Successful++
			u.uploadStats.LastUploadTime = time.Now()
			u.statsMu.Unlock()
		}

		u.statsMu.Lock()
		u.uploadStats.TotalFiles++
		u.statsMu.Unlock()
	}
}

// uploadFileWithRetry uploads a file with retry logic
func (u *Uploader) uploadFileWithRetry(filePath string) error {
	var lastErr error
	for attempt := 0; attempt <= u.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-u.ctx.Done():
				return fmt.Errorf("uploader stopped")
			case <-time.After(u.config.RetryDelay):
			}
		}

		start := time.Now()
		err := u.uploadFile(filePath)
		duration := time.Since(start)

		if err == nil {
			// Success - update stats
			fileInfo, statErr := os.Stat(filePath)
			if statErr == nil {
				u.statsMu.Lock()
				u.uploadStats.TotalBytes += fileInfo.Size()
				u.uploadStats.TotalDuration += duration
				u.statsMu.Unlock()
			}
			return nil
		}

		lastErr = err
		if attempt < u.config.MaxRetries {
			log.Printf("[WARNING] Upload attempt %d/%d failed for %s: %v, retrying...", attempt+1, u.config.MaxRetries+1, filePath, err)
		}
	}

	return fmt.Errorf("upload failed after %d attempts: %w", u.config.MaxRetries+1, lastErr)
}

// uploadFile uploads a single file to GCS using parallel chunk upload
func (u *Uploader) uploadFile(filePath string) error {
	// Open file for reading
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Get file size
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}
	fileSize := fileInfo.Size()

	// Read entire file into memory (for parallel chunk upload)
	// Note: For very large files, consider streaming instead
	buf := make([]byte, fileSize)
	if _, err := io.ReadFull(file, buf); err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Generate object name
	objectName := u.generateObjectName(filePath)

	// Upload using parallel chunk upload with chunk manager
	if err := u.uploadParallel(u.ctx, u.client, u.config.Bucket, objectName, buf, u.config.ChunkSize); err != nil {
		return fmt.Errorf("parallel upload failed: %w", err)
	}

	// Delete local file after successful upload
	if err := os.Remove(filePath); err != nil {
		log.Printf("[WARNING] Failed to delete local file %s after upload: %v", filePath, err)
		// Non-fatal - upload succeeded
	}

	return nil
}

// generateObjectName generates the GCS object name from file path
func (u *Uploader) generateObjectName(filePath string) string {
	fileName := filepath.Base(filePath)
	if u.config.ObjectPrefix != "" {
		return fmt.Sprintf("%s%s", u.config.ObjectPrefix, fileName)
	}
	return fileName
}

// uploadParallel uploads chunks in parallel and composes them into the final object
// This is based on the existing gcs_uploader module
func (u *Uploader) uploadParallel(ctx context.Context, client *storage.Client, bucket, object string, buf []byte, chunkSizeBytes int) error {
	// Calculate number of chunks
	numChunks := (len(buf) + chunkSizeBytes - 1) / chunkSizeBytes

	// Generate unique prefix for temporary chunk objects
	uploadID := time.Now().UnixNano()
	tempPrefix := fmt.Sprintf("%s.tmp.%d", object, uploadID)

	// Track chunk uploads
	type chunkResult struct {
		index  int
		object string
		size   int64
		err    error
	}

	results := make([]chunkResult, numChunks)
	var wg sync.WaitGroup

	// Upload chunks in parallel
	for i := 0; i < numChunks; i++ {
		offset := i * chunkSizeBytes
		end := offset + chunkSizeBytes
		if end > len(buf) {
			end = len(buf)
		}

		wg.Add(1)
		go func(chunkIndex int, chunkData []byte) {
			defer wg.Done()

			chunkObject := fmt.Sprintf("%s.chunk.%d", tempPrefix, chunkIndex)

			// Upload this chunk as a separate object
			w := client.Bucket(bucket).Object(chunkObject).NewWriter(ctx)
			w.ChunkSize = chunkSizeBytes
			w.ContentType = "application/octet-stream"

			if _, err := w.Write(chunkData); err != nil {
				results[chunkIndex] = chunkResult{
					index: chunkIndex,
					err:   fmt.Errorf("write error: %w", err),
				}
				return
			}

			if err := w.Close(); err != nil {
				results[chunkIndex] = chunkResult{
					index: chunkIndex,
					err:   fmt.Errorf("close error: %w", err),
				}
				return
			}

			// Get object attributes to verify size
			attrs, err := client.Bucket(bucket).Object(chunkObject).Attrs(ctx)
			if err != nil {
				results[chunkIndex] = chunkResult{
					index: chunkIndex,
					err:   fmt.Errorf("attrs error: %w", err),
				}
				return
			}

			results[chunkIndex] = chunkResult{
				index:  chunkIndex,
				object: chunkObject,
				size:   attrs.Size,
			}
		}(i, buf[offset:end])
	}

	// Wait for all uploads to complete
	wg.Wait()

	// Check for errors
	for _, result := range results {
		if result.err != nil {
			// Cleanup: delete any successfully uploaded chunks
			u.cleanupTempChunks(ctx, client, bucket, tempPrefix, numChunks)
			return fmt.Errorf("chunk %d failed: %w", result.index, result.err)
		}
	}

	// Build list of chunk object names
	chunkObjects := make([]string, numChunks)
	for i := 0; i < numChunks; i++ {
		chunkObjects[i] = fmt.Sprintf("%s.chunk.%d", tempPrefix, i)
	}

	// Use chunk manager to compose (handles 32-chunk limit)
	if err := u.chunkMgr.Compose(ctx, client, bucket, object, chunkObjects); err != nil {
		// Cleanup on failure
		u.cleanupTempChunks(ctx, client, bucket, tempPrefix, numChunks)
		return fmt.Errorf("compose error: %w", err)
	}

	// Verify final object size matches expected size
	attrs, err := client.Bucket(bucket).Object(object).Attrs(ctx)
	if err != nil {
		u.cleanupTempChunks(ctx, client, bucket, tempPrefix, numChunks)
		return fmt.Errorf("failed to get object attributes: %w", err)
	}

	if attrs.Size != int64(len(buf)) {
		// Cleanup and return error
		u.cleanupTempChunks(ctx, client, bucket, tempPrefix, numChunks)
		_ = client.Bucket(bucket).Object(object).Delete(ctx) // Try to delete malformed object
		return fmt.Errorf("size mismatch: expected %d bytes, got %d bytes", len(buf), attrs.Size)
	}

	// Cleanup temporary chunk objects
	if err := u.cleanupTempChunks(ctx, client, bucket, tempPrefix, numChunks); err != nil {
		log.Printf("[WARNING] Failed to cleanup some temp chunks: %v", err)
		// Non-fatal - main upload succeeded
	}

	return nil
}

// cleanupTempChunks deletes temporary chunk objects
func (u *Uploader) cleanupTempChunks(ctx context.Context, client *storage.Client, bucket, prefix string, numChunks int) error {
	var errs []error
	bkt := client.Bucket(bucket)

	for i := 0; i < numChunks; i++ {
		chunkObject := fmt.Sprintf("%s.chunk.%d", prefix, i)
		if err := bkt.Object(chunkObject).Delete(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete %s: %w", chunkObject, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %v", errs)
	}

	return nil
}
