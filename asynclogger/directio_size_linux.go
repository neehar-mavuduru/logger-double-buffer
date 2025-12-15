//go:build linux

package asynclogger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// alignUp rounds n up to the next multiple of align (power of 2).
// alignmentSize is already defined in directio_linux.go (shared constant)
func alignUp(n, align int64) int64 {
	return (n + align - 1) &^ (align - 1)
}

// openDirectIOSize opens a file with O_DIRECT and O_DSYNC flags, preallocating with fallocate
// O_DIRECT: Bypasses OS page cache, writes directly to disk
// O_DSYNC: Each write automatically syncs data to disk (eliminates need for explicit sync)
// O_TRUNC: Truncates file to ensure it starts at offset 0 (4096-byte aligned) for O_DIRECT compliance
// fallocate: Preallocates file extents for Direct I/O, improving write performance
func openDirectIOSize(path string, preallocateSize int64) (*os.File, int64, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, 0, fmt.Errorf("failed to create directory: %w", err)
	}

	// Align preallocate size to filesystem block size
	alignedSize := alignUp(preallocateSize, alignmentSize)

	// Open with O_DIRECT, O_DSYNC, O_WRONLY, O_CREAT, O_TRUNC
	// O_TRUNC ensures file starts at offset 0 (aligned) for O_DIRECT compliance
	fd, err := syscall.Open(path,
		syscall.O_WRONLY|syscall.O_CREAT|syscall.O_TRUNC|syscall.O_DIRECT|syscall.O_DSYNC,
		0644)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open file with O_DIRECT: %w", err)
	}

	// Preallocate file using fallocate to ensure extents are ready for Direct I/O
	// This improves write performance by avoiding extent allocation during writes
	if err := unix.Fallocate(fd, 0, 0, alignedSize); err != nil {
		syscall.Close(fd)
		return nil, 0, fmt.Errorf("failed to preallocate file with fallocate: %w", err)
	}

	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		syscall.Close(fd)
		return nil, 0, fmt.Errorf("failed to create file descriptor")
	}

	// File is always truncated and preallocated, so offset is always 0 (aligned)
	return file, 0, nil
}

// allocAlignedBuffer allocates a byte slice aligned to filesystem block size (4096 bytes for ext4) for O_DIRECT
func allocAlignedBufferSize(size int) []byte {
	// Round up to alignment
	alignedSize := ((size + alignmentSize - 1) / alignmentSize) * alignmentSize

	// Allocate extra space to ensure we can align
	buf := make([]byte, alignedSize+alignmentSize)

	// Get the address of the first byte
	addr := uintptr(unsafe.Pointer(&buf[0]))

	// Calculate offset needed for alignment
	offset := int(alignmentSize - (addr % alignmentSize))
	if offset == alignmentSize {
		offset = 0
	}

	// Return aligned slice
	return buf[offset : offset+alignedSize]
}

// writevAlignedWithOffsetSize writes multiple buffers to file at a specific offset using vectored I/O
// Uses unix.Pwritev() - NO memory copy, just pointers! Maintains vectored I/O efficiency with offset control
// OPTIMIZATION: Buffers are already address and size-aligned (4096 bytes) from NewBuffer(),
// so no padding or buffer pooling is needed - true zero-copy!
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

	// Buffers are already aligned (address and size), so we can write directly!
	// Single vectored write syscall at specific offset - kernel reads from multiple buffers!
	// unix.Pwritev takes [][]byte directly with offset - NO iovec creation needed, NO copying!
	n, err := unix.Pwritev(fd, nonEmptyBuffers, offset)
	if err != nil {
		return n, fmt.Errorf("vectored I/O write failed: %w", err)
	}

	// Return actual bytes written by Pwritev
	// Buffers are already 4096-byte aligned, so offset stays aligned after write
	return n, nil
}

// alignSize rounds up size to the nearest alignment boundary
func alignSizeSize(size int) int {
	return ((size + alignmentSize - 1) / alignmentSize) * alignmentSize
}

// extractBasePathSize extracts directory and base filename from a full file path
// Returns directory, base filename without extension, and error
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

// SizeFileWriter manages file handles, offset tracking, and size-based rotation for Direct I/O writes
// Encapsulates all file management logic, keeping logger.go unaware of rotation details
// Uses fallocate to preallocate files for optimal Direct I/O performance
type SizeFileWriter struct {
	// Current file
	file        *os.File
	fd          int
	filePath    string
	fileOffset  atomic.Int64
	maxFileSize int64 // Maximum file size before rotation

	// Next file (for rotation)
	nextFile     *os.File
	nextFd       int
	nextFilePath string

	// Configuration
	baseDir             string
	baseFileName        string
	preallocateFileSize int64 // Size to preallocate using fallocate

	// Mutex for rotation operations (only held during rotation)
	rotationMu sync.Mutex

	// Last Pwritev duration (for metrics tracking)
	lastPwritevDuration atomic.Int64 // Nanoseconds
}

// NewSizeFileWriter creates a new SizeFileWriter with the given configuration
func NewSizeFileWriter(config SizeConfig) (*SizeFileWriter, error) {
	// Extract base directory and filename
	baseDir, baseFileName, err := extractBasePathSize(config.LogFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract base path: %w", err)
	}

	// Open initial file with preallocation
	file, initialOffset, err := openDirectIOSize(config.LogFilePath, config.PreallocateFileSize)
	if err != nil {
		return nil, fmt.Errorf("failed to open initial file: %w", err)
	}

	fw := &SizeFileWriter{
		file:                file,
		fd:                  int(file.Fd()),
		filePath:            config.LogFilePath,
		maxFileSize:         config.MaxFileSize,
		baseDir:             baseDir,
		baseFileName:        baseFileName,
		preallocateFileSize: config.PreallocateFileSize,
	}

	// Set initial offset (0 for new files)
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
	// We check before writing, so if current offset + next write size would exceed, rotate
	// For simplicity, we rotate when we're at 90% capacity to allow proactive next file creation
	if currentOffset >= int64(float64(fw.maxFileSize)*0.9) {
		// Acquire rotation mutex to prevent concurrent rotations
		fw.rotationMu.Lock()
		defer fw.rotationMu.Unlock()

		// Double-check after acquiring lock (another goroutine might have rotated)
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

	// Check if we've actually exceeded the max file size (need to swap immediately)
	if currentOffset >= fw.maxFileSize {
		// Acquire rotation mutex
		fw.rotationMu.Lock()
		defer fw.rotationMu.Unlock()

		// Double-check after acquiring lock
		currentOffset = fw.fileOffset.Load()
		if currentOffset < fw.maxFileSize {
			return nil
		}

		// Ensure next file exists
		if fw.nextFile == nil {
			if err := fw.createNextFile(); err != nil {
				return fmt.Errorf("failed to create next file: %w", err)
			}
		}

		// Swap to next file
		if err := fw.swapFiles(); err != nil {
			return fmt.Errorf("failed to swap files: %w", err)
		}
	}

	return nil
}

// createNextFile creates a new file for rotation with preallocation
func (fw *SizeFileWriter) createNextFile() error {
	// Generate timestamped filename: {baseFileName}_{YYYY-MM-DD_HH-MM-SS}.log
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	nextPath := filepath.Join(fw.baseDir, fmt.Sprintf("%s_%s.log", fw.baseFileName, timestamp))

	// Open new file with preallocation
	file, initialOffset, err := openDirectIOSize(nextPath, fw.preallocateFileSize)
	if err != nil {
		return fmt.Errorf("failed to open next file: %w", err)
	}

	// Store next file details
	fw.nextFile = file
	fw.nextFd = int(file.Fd())
	fw.nextFilePath = nextPath

	// Next file should start at offset 0 (new file)
	if initialOffset != 0 {
		return fmt.Errorf("next file should be empty, but has size %d", initialOffset)
	}

	return nil
}

// swapFiles atomically swaps from current file to next file
func (fw *SizeFileWriter) swapFiles() error {
	if fw.nextFile == nil || fw.nextFd == 0 || fw.nextFilePath == "" {
		return fmt.Errorf("next file is not set")
	}

	// Defensive check: ensure current file is valid (fast pointer check, no performance impact)
	if fw.file == nil {
		return fmt.Errorf("current file is nil")
	}

	// Sync current file to ensure all data is written
	if err := unix.Fsync(fw.fd); err != nil {
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
	fw.fileOffset.Store(0) // Reset offset for new file

	// Clear next file fields
	fw.nextFile = nil
	fw.nextFd = 0
	fw.nextFilePath = ""

	return nil
}

// WriteVectored writes multiple buffers to the file using vectored I/O
// Handles rotation automatically before writing
func (fw *SizeFileWriter) WriteVectored(buffers [][]byte) (int, error) {
	// Fast path: skip if no data to write (defensive check, no performance impact)
	if len(buffers) == 0 {
		return 0, nil
	}

	// Calculate total size of buffers to write
	totalSize := 0
	for _, buf := range buffers {
		totalSize += len(buf)
	}

	// Check if this write would exceed max file size
	currentOffset := fw.fileOffset.Load()
	if fw.maxFileSize > 0 && currentOffset+int64(totalSize) > fw.maxFileSize {
		// Need to rotate before writing
		if err := fw.rotateIfNeeded(); err != nil {
			return 0, fmt.Errorf("rotation failed: %w", err)
		}
		// Re-read offset after potential rotation
		currentOffset = fw.fileOffset.Load()
	} else {
		// Check if we're approaching max file size (proactive rotation at 90%)
		if err := fw.rotateIfNeeded(); err != nil {
			return 0, fmt.Errorf("rotation failed: %w", err)
		}
	}

	// Get current offset
	offset := fw.fileOffset.Load()

	// Write using vectored I/O at specific offset (Linux uses fd for Pwritev)
	// Track ONLY the Pwritev syscall time (pure disk I/O)
	pwritevStart := time.Now()
	n, err := writevAlignedWithOffsetSize(fw.fd, buffers, offset)
	pwritevDuration := time.Since(pwritevStart)

	// Store Pwritev duration for metrics (even on error, to track syscall time)
	fw.lastPwritevDuration.Store(pwritevDuration.Nanoseconds())

	if err != nil {
		return n, err
	}

	// Update offset atomically after successful write
	fw.fileOffset.Add(int64(n))

	return n, nil
}

// Close syncs and closes the current file, and closes next file if it exists
func (fw *SizeFileWriter) Close() error {
	var firstErr error

	// Sync and close current file
	if fw.file != nil {
		if err := unix.Fsync(fw.fd); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to sync current file: %w", err)
		}
		if err := fw.file.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close current file: %w", err)
		}
	}

	// Close next file if it exists
	if fw.nextFile != nil {
		if err := fw.nextFile.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close next file: %w", err)
		}
	}

	return firstErr
}

// GetLastPwritevDuration returns the duration of the last Pwritev syscall
// This measures pure disk I/O time, excluding rotation checks and other overhead
func (fw *SizeFileWriter) GetLastPwritevDuration() time.Duration {
	return time.Duration(fw.lastPwritevDuration.Load())
}
