//go:build !linux

package asynclogger

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
// This is a fallback implementation that doesn't use Direct I/O
type SizeFileWriter struct {
	// Current file
	file          *os.File
	fd            int
	filePath      string
	fileOffset    atomic.Int64
	maxFileSize   int64 // Maximum file size before rotation

	// Next file (for rotation)
	nextFile     *os.File
	nextFd       int
	nextFilePath string

	// Configuration
	baseDir              string
	baseFileName         string
	preallocateFileSize  int64 // Size to preallocate (not used on non-Linux)

	// Mutex for rotation operations (only held during rotation)
	rotationMu sync.Mutex

	// Last write duration (for metrics tracking)
	lastPwritevDuration atomic.Int64 // Nanoseconds
}

// openDirectIOSize opens a file without Direct I/O (non-Linux fallback)
func openDirectIOSize(path string, preallocateSize int64) (*os.File, int64, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, 0, fmt.Errorf("failed to create directory: %w", err)
	}

	// Open file normally (no Direct I/O on non-Linux)
	file, err := os.Create(path)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create file: %w", err)
	}

	// Get file info to determine initial offset
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, fmt.Errorf("failed to stat file: %w", err)
	}

	return file, info.Size(), nil
}

// writevAlignedWithOffsetSize writes multiple buffers to file at a specific offset (non-Linux fallback)
// Uses WriteAt for each buffer sequentially (not as efficient as Pwritev, but works)
func writevAlignedWithOffsetSize(fd int, buffers [][]byte, offset int64) (int, error) {
	if len(buffers) == 0 {
		return 0, nil
	}

	// Filter out empty buffers
	nonEmptyBuffers := make([][]byte, 0, len(buffers))
	for _, buf := range buffers {
		if len(buf) > 0 {
			nonEmptyBuffers = append(nonEmptyBuffers, buf)
		}
	}

	if len(nonEmptyBuffers) == 0 {
		return 0, nil
	}

	// Get file from fd (non-Linux fallback)
	// Note: This is a simplified implementation - in practice, you'd need to track file separately
	// For now, we'll return an error indicating this needs to be implemented properly
	return 0, fmt.Errorf("writevAlignedWithOffsetSize not fully implemented for non-Linux systems")
}

// extractBasePathSize extracts directory and base filename from a full file path
func extractBasePathSize(fullPath string) (dir, baseName string, err error) {
	dir = filepath.Dir(fullPath)
	if dir == "." || dir == "" {
		dir = "."
	}

	baseName = filepath.Base(fullPath)
	// Remove .log extension if present
	if strings.HasSuffix(baseName, ".log") {
		baseName = strings.TrimSuffix(baseName, ".log")
	}

	if baseName == "" {
		return "", "", fmt.Errorf("invalid file path: base name is empty after extraction")
	}

	return dir, baseName, nil
}

// NewSizeFileWriter creates a new SizeFileWriter with the given configuration (non-Linux fallback)
func NewSizeFileWriter(config SizeConfig) (*SizeFileWriter, error) {
	// Extract base directory and filename
	baseDir, baseFileName, err := extractBasePathSize(config.LogFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract base path: %w", err)
	}

	// Generate timestamped filename for initial file (consistent naming)
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	initialPath := filepath.Join(baseDir, fmt.Sprintf("%s_%s.log", baseFileName, timestamp))

	// Open initial file
	file, initialOffset, err := openDirectIOSize(initialPath, config.PreallocateFileSize)
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
	}

	// Set initial offset
	fw.fileOffset.Store(initialOffset)

	return fw, nil
}

// rotateIfNeeded checks if rotation is needed based on file size and performs it if necessary
func (fw *SizeFileWriter) rotateIfNeeded() error {
	// If rotation is disabled (maxFileSize is 0), skip
	if fw.maxFileSize <= 0 {
		return nil
	}

	// Get current offset
	currentOffset := fw.fileOffset.Load()

	// Check if rotation is needed (current write would exceed max file size)
	if currentOffset >= int64(float64(fw.maxFileSize)*0.9) {
		// Acquire rotation mutex to prevent concurrent rotations
		fw.rotationMu.Lock()
		defer fw.rotationMu.Unlock()

		// Double-check after acquiring lock
		currentOffset = fw.fileOffset.Load()
		if currentOffset < int64(float64(fw.maxFileSize)*0.9) {
			return nil
		}

		// If next file doesn't exist, create it
		if fw.nextFile == nil {
			if err := fw.createNextFile(); err != nil {
				return fmt.Errorf("failed to create next file: %w", err)
			}
		}
	}

	// Check if we've actually exceeded the max file size
	if currentOffset >= fw.maxFileSize {
		fw.rotationMu.Lock()
		defer fw.rotationMu.Unlock()

		currentOffset = fw.fileOffset.Load()
		if currentOffset < fw.maxFileSize {
			return nil
		}

		if fw.nextFile == nil {
			if err := fw.createNextFile(); err != nil {
				return fmt.Errorf("failed to create next file: %w", err)
			}
		}

		if err := fw.swapFiles(); err != nil {
			return fmt.Errorf("failed to swap files: %w", err)
		}
	}

	return nil
}

// createNextFile creates a new file for rotation
func (fw *SizeFileWriter) createNextFile() error {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	nextPath := filepath.Join(fw.baseDir, fmt.Sprintf("%s_%s.log", fw.baseFileName, timestamp))

	file, initialOffset, err := openDirectIOSize(nextPath, fw.preallocateFileSize)
	if err != nil {
		return fmt.Errorf("failed to open next file: %w", err)
	}

	fw.nextFile = file
	fw.nextFd = 0
	fw.nextFilePath = nextPath

	if initialOffset != 0 {
		return fmt.Errorf("next file should be empty, but has size %d", initialOffset)
	}

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

	// Close current file
	if err := fw.file.Close(); err != nil {
		return fmt.Errorf("failed to close current file: %w", err)
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

// WriteVectored writes multiple buffers to the file (non-Linux fallback)
func (fw *SizeFileWriter) WriteVectored(buffers [][]byte) (int, error) {
	if len(buffers) == 0 {
		return 0, nil
	}

	// Calculate total size
	totalSize := 0
	for _, buf := range buffers {
		totalSize += len(buf)
	}

	currentOffset := fw.fileOffset.Load()
	if fw.maxFileSize > 0 && currentOffset+int64(totalSize) > fw.maxFileSize {
		if err := fw.rotateIfNeeded(); err != nil {
			return 0, fmt.Errorf("rotation failed: %w", err)
		}
		currentOffset = fw.fileOffset.Load()
	} else {
		if err := fw.rotateIfNeeded(); err != nil {
			return 0, fmt.Errorf("rotation failed: %w", err)
		}
	}

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

// Close syncs and closes the current file, and closes next file if it exists
func (fw *SizeFileWriter) Close() error {
	var firstErr error

	if fw.file != nil {
		if err := fw.file.Sync(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to sync current file: %w", err)
		}
		if err := fw.file.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close current file: %w", err)
		}
	}

	if fw.nextFile != nil {
		if err := fw.nextFile.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close next file: %w", err)
		}
	}

	return firstErr
}

// GetLastPwritevDuration returns the duration of the last write syscall
func (fw *SizeFileWriter) GetLastPwritevDuration() time.Duration {
	return time.Duration(fw.lastPwritevDuration.Load())
}

