//go:build !linux

package asyncloguploader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SizeFileWriter manages file handles, offset tracking, and size-based rotation for non-Linux systems
type SizeFileWriter struct {
	// Current file
	file        *os.File
	fd          int
	filePath    string
	fileOffset  atomic.Int64
	maxFileSize int64

	// Next file (for rotation)
	nextFile     *os.File
	nextFd       int
	nextFilePath string

	// Configuration
	baseDir             string
	baseFileName        string
	preallocateFileSize int64

	// Mutex for rotation operations
	rotationMu sync.Mutex

	// Last write duration (for metrics tracking)
	lastPwritevDuration atomic.Int64 // Nanoseconds

	// Channel for completed files (for GCS upload)
	completedFileChan chan<- string
}

// NewSizeFileWriter creates a new SizeFileWriter (non-Linux fallback)
func NewSizeFileWriter(config Config, completedFileChan chan<- string) (*SizeFileWriter, error) {
	// Extract base directory and filename
	baseDir, baseFileName, err := extractBasePathSize(config.LogFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract base path: %w", err)
	}

	// Generate timestamped filename for initial file
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	initialPath := filepath.Join(baseDir, fmt.Sprintf("%s_%s.log", baseFileName, timestamp))

	// Open initial file (always starts at offset 0 for new files)
	file, err := openDirectIOSize(initialPath, config.PreallocateFileSize)
	if err != nil {
		return nil, fmt.Errorf("failed to open initial file: %w", err)
	}

	fw := &SizeFileWriter{
		file:                file,
		fd:                  0, // Not used on non-Linux
		filePath:            initialPath,
		maxFileSize:         config.MaxFileSize,
		baseDir:             baseDir,
		baseFileName:        baseFileName,
		preallocateFileSize: config.PreallocateFileSize,
		completedFileChan:   completedFileChan,
	}

	// New files always start at offset 0
	fw.fileOffset.Store(0)

	return fw, nil
}

// WriteVectored writes multiple buffers to the file (non-Linux fallback)
func (fw *SizeFileWriter) WriteVectored(buffers [][]byte) (int, error) {
	if len(buffers) == 0 {
		return 0, nil
	}

	// Check and perform rotation if needed
	if err := fw.rotateIfNeeded(); err != nil {
		return 0, fmt.Errorf("rotation failed: %w", err)
	}

	// Get current offset
	offset := fw.fileOffset.Load()

	// Write sequentially (non-Linux fallback)
	writeStart := time.Now()
	totalWritten := 0
	for _, buf := range buffers {
		if len(buf) == 0 {
			continue
		}
		n, err := fw.file.WriteAt(buf, offset+int64(totalWritten))
		if err != nil {
			fw.lastPwritevDuration.Store(time.Since(writeStart).Nanoseconds())
			return totalWritten, err
		}
		totalWritten += n
	}
	writeDuration := time.Since(writeStart)

	fw.lastPwritevDuration.Store(writeDuration.Nanoseconds())
	fw.fileOffset.Add(int64(totalWritten))

	return totalWritten, nil
}

// GetLastPwritevDuration returns the duration of the last write
func (fw *SizeFileWriter) GetLastPwritevDuration() time.Duration {
	return time.Duration(fw.lastPwritevDuration.Load())
}

// Close syncs and closes the current file
func (fw *SizeFileWriter) Close() error {
	var firstErr error

	if fw.nextFile != nil {
		if err := fw.nextFile.Close(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if fw.file != nil {
		if err := fw.file.Sync(); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := fw.file.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// rotateIfNeeded checks if rotation is needed
func (fw *SizeFileWriter) rotateIfNeeded() error {
	if fw.maxFileSize <= 0 {
		return nil
	}

	fw.rotationMu.Lock()
	defer fw.rotationMu.Unlock()

	currentOffset := fw.fileOffset.Load()

	if currentOffset >= fw.maxFileSize {
		if fw.nextFile == nil {
			if err := fw.createNextFile(); err != nil {
				return fmt.Errorf("failed to create next file: %w", err)
			}
		}

		if err := fw.swapFiles(); err != nil {
			return fmt.Errorf("failed to swap files: %w", err)
		}
		return nil
	}

	if currentOffset >= int64(float64(fw.maxFileSize)*0.9) {
		if fw.nextFile == nil {
			if err := fw.createNextFile(); err != nil {
				return nil // Non-blocking
			}
		}
	}

	return nil
}

// createNextFile creates a new file for rotation
func (fw *SizeFileWriter) createNextFile() error {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	nextPath := filepath.Join(fw.baseDir, fmt.Sprintf("%s_%s.log", fw.baseFileName, timestamp))

	file, err := openDirectIOSize(nextPath, fw.preallocateFileSize)
	if err != nil {
		return fmt.Errorf("failed to open next file: %w", err)
	}

	fw.nextFile = file
	fw.nextFd = 0
	fw.nextFilePath = nextPath

	return nil
}

// swapFiles atomically swaps from current file to next file
func (fw *SizeFileWriter) swapFiles() error {
	if fw.nextFile == nil || fw.nextFilePath == "" {
		return fmt.Errorf("next file is not set")
	}

	if fw.file == nil {
		return fmt.Errorf("current file is nil")
	}

	// Sync current file
	if err := fw.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync current file: %w", err)
	}

	completedFilePath := fw.filePath

	// Close current file
	if err := fw.file.Close(); err != nil {
		return fmt.Errorf("failed to close current file: %w", err)
	}

	// Send completed file to upload channel (non-blocking)
	if fw.completedFileChan != nil {
		select {
		case fw.completedFileChan <- completedFilePath:
		default:
			fmt.Printf("[WARNING] Upload channel full, skipping upload for %s\n", completedFilePath)
		}
	}

	// Swap next file to current
	fw.file = fw.nextFile
	fw.fd = fw.nextFd
	fw.filePath = fw.nextFilePath
	fw.fileOffset.Store(0)

	// Clear next file fields
	fw.nextFile = nil
	fw.nextFd = 0
	fw.nextFilePath = ""

	return nil
}

// openDirectIOSize opens a file (non-Linux fallback)
// Returns the file and error. New files always start at offset 0.
func openDirectIOSize(path string, preallocateSize int64) (*os.File, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	// New files with O_TRUNC always start at offset 0
	return file, nil
}

// extractBasePathSize extracts directory and base filename
func extractBasePathSize(fullPath string) (dir, baseName string, err error) {
	dir = filepath.Dir(fullPath)
	if dir == "." || dir == "" {
		dir = "."
	}

	baseName = filepath.Base(fullPath)
	baseName = strings.TrimSuffix(baseName, ".log")

	if baseName == "" {
		return "", "", fmt.Errorf("invalid file path: base name is empty after extraction")
	}

	return dir, baseName, nil
}
