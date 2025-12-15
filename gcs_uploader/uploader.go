package gcsuploader

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"cloud.google.com/go/storage"
)

// UploadParallel uploads chunks in parallel and composes them into the final object.
// chunkSizeBytes specifies the size of each chunk for parallel upload.
// This ensures:
// 1. True parallelism: Multiple goroutines upload chunks simultaneously
// 2. Data integrity: GCS Compose API atomically combines chunks in order
// 3. Correct final file: Chunks are composed in the exact order they were split
func UploadParallel(ctx context.Context, client *storage.Client, bucket, object string, buf []byte, chunkSizeBytes int) error {
	// Calculate number of chunks
	numChunks := (len(buf) + chunkSizeBytes - 1) / chunkSizeBytes
	fmt.Printf("Starting parallel upload with %d chunks (%d MB each)...\n\n", numChunks, chunkSizeBytes/(1024*1024))

	// Generate unique prefix for temporary chunk objects
	uploadID := time.Now().UnixNano()
	tempPrefix := fmt.Sprintf("%s.tmp.%d", object, uploadID)

	// Track chunk uploads
	type chunkResult struct {
		index    int
		object   string
		size     int64
		duration time.Duration
		err      error
	}

	results := make([]chunkResult, numChunks)
	var wg sync.WaitGroup

	// Upload chunks in parallel
	uploadStart := time.Now()
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
			chunkStart := time.Now()

			// Upload this chunk as a separate object
			w := client.Bucket(bucket).Object(chunkObject).NewWriter(ctx)
			w.ChunkSize = chunkSizeBytes
			w.ContentType = "application/octet-stream"

			if _, err := w.Write(chunkData); err != nil {
				results[chunkIndex] = chunkResult{
					index:    chunkIndex,
					err:      fmt.Errorf("write error: %w", err),
					duration: time.Since(chunkStart),
				}
				return
			}

			if err := w.Close(); err != nil {
				results[chunkIndex] = chunkResult{
					index:    chunkIndex,
					err:      fmt.Errorf("close error: %w", err),
					duration: time.Since(chunkStart),
				}
				return
			}

			// Get object attributes to verify size
			attrs, err := client.Bucket(bucket).Object(chunkObject).Attrs(ctx)
			if err != nil {
				results[chunkIndex] = chunkResult{
					index:    chunkIndex,
					err:      fmt.Errorf("attrs error: %w", err),
					duration: time.Since(chunkStart),
				}
				return
			}

			duration := time.Since(chunkStart)
			chunkMB := float64(len(chunkData)) / (1024 * 1024)
			throughput := chunkMB / duration.Seconds()

			fmt.Printf("Chunk %-3d | %6.1f MB | %4d ms | %7.2f MB/s | ✓\n",
				chunkIndex,
				chunkMB,
				duration.Milliseconds(),
				throughput,
			)

			results[chunkIndex] = chunkResult{
				index:    chunkIndex,
				object:   chunkObject,
				size:     attrs.Size,
				duration: duration,
			}
		}(i, buf[offset:end])
	}

	// Wait for all uploads to complete
	wg.Wait()
	uploadDuration := time.Since(uploadStart)

	// Check for errors
	for _, result := range results {
		if result.err != nil {
			// Cleanup: delete any successfully uploaded chunks
			cleanupTempChunks(ctx, client, bucket, tempPrefix, numChunks)
			return fmt.Errorf("chunk %d failed: %w", result.index, result.err)
		}
	}

	fmt.Printf("\nAll %d chunks uploaded in %v\n", numChunks, uploadDuration)
	fmt.Println("Composing chunks into final object...")

	// Compose chunks into final object
	// GCS Compose API ensures atomic operation and correct ordering
	composeStart := time.Now()
	bkt := client.Bucket(bucket)
	dst := bkt.Object(object)

	// Build list of source objects in order
	var sources []*storage.ObjectHandle
	var totalSize int64
	for i := 0; i < numChunks; i++ {
		chunkObject := fmt.Sprintf("%s.chunk.%d", tempPrefix, i)
		sources = append(sources, bkt.Object(chunkObject))
		totalSize += results[i].size
	}

	// Compose: GCS atomically combines all chunks in order
	composer := dst.ComposerFrom(sources...)
	composer.ContentType = "application/octet-stream"

	composedAttrs, err := composer.Run(ctx)
	if err != nil {
		// Cleanup on failure
		cleanupTempChunks(ctx, client, bucket, tempPrefix, numChunks)
		return fmt.Errorf("compose error: %w", err)
	}

	composeDuration := time.Since(composeStart)
	fmt.Printf("Compose completed in %v\n", composeDuration)

	// Verify final object size matches expected size
	if composedAttrs.Size != int64(len(buf)) {
		// Cleanup and return error
		cleanupTempChunks(ctx, client, bucket, tempPrefix, numChunks)
		_ = dst.Delete(ctx) // Try to delete malformed object
		return fmt.Errorf("size mismatch: expected %d bytes, got %d bytes", len(buf), composedAttrs.Size)
	}

	fmt.Println("Verifying final object integrity...")

	// Optional: Read back first and last few bytes to verify integrity
	if err := verifyObjectIntegrity(ctx, client, bucket, object, buf); err != nil {
		// Cleanup on verification failure
		cleanupTempChunks(ctx, client, bucket, tempPrefix, numChunks)
		return fmt.Errorf("integrity check failed: %w", err)
	}

	// Cleanup temporary chunk objects
	fmt.Println("Cleaning up temporary chunks...")
	if err := cleanupTempChunks(ctx, client, bucket, tempPrefix, numChunks); err != nil {
		log.Printf("Warning: Failed to cleanup some temp chunks: %v", err)
		// Non-fatal - main upload succeeded
	}

	fmt.Println("✓ Parallel upload completed successfully!")

	return nil
}

// verifyObjectIntegrity checks that the first and last bytes match the original buffer
func verifyObjectIntegrity(ctx context.Context, client *storage.Client, bucket, object string, original []byte) error {
	checkSize := 1024
	if len(original) < checkSize {
		checkSize = len(original)
	}

	// Check first bytes
	reader, err := client.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		return fmt.Errorf("failed to create reader: %w", err)
	}

	firstBytes := make([]byte, checkSize)
	if _, err := reader.Read(firstBytes); err != nil {
		reader.Close()
		return fmt.Errorf("failed to read first bytes: %w", err)
	}
	reader.Close()

	for i := 0; i < checkSize; i++ {
		if firstBytes[i] != original[i] {
			return fmt.Errorf("first bytes mismatch at offset %d", i)
		}
	}

	// Check last bytes using Range header (more efficient than reading entire file)
	if len(original) > checkSize {
		offset := len(original) - checkSize
		obj := client.Bucket(bucket).Object(object)
		reader, err = obj.NewRangeReader(ctx, int64(offset), int64(checkSize))
		if err != nil {
			return fmt.Errorf("failed to create range reader: %w", err)
		}
		defer reader.Close()

		lastBytes := make([]byte, checkSize)
		if _, err := reader.Read(lastBytes); err != nil {
			return fmt.Errorf("failed to read last bytes: %w", err)
		}

		for i := 0; i < checkSize; i++ {
			if lastBytes[i] != original[offset+i] {
				return fmt.Errorf("last bytes mismatch at offset %d", offset+i)
			}
		}
	}

	return nil
}

// cleanupTempChunks deletes temporary chunk objects
func cleanupTempChunks(ctx context.Context, client *storage.Client, bucket, prefix string, numChunks int) error {
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
