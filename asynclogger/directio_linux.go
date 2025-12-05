//go:build linux

package asynclogger

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// alignmentSize is the required alignment for O_DIRECT on Linux
// For ext4 filesystem, this must be 4096 bytes (4KB), not 512 bytes!
// O_DIRECT requires alignment to filesystem block size, not just sector size
const alignmentSize = 4096

// flushBufferPool provides pre-allocated aligned buffers for flushing
// Each buffer is 256MB to comfortably handle 128MB buffer + alignment padding
var flushBufferPool = sync.Pool{
	New: func() interface{} {
		size := 256 * 1024 * 1024 // 256MB
		buf := allocAlignedBuffer(size)
		return &buf
	},
}

// openDirectIO opens a file with O_DIRECT and O_DSYNC flags
// O_DIRECT: Bypasses OS page cache, writes directly to disk
// O_DSYNC: Each write automatically syncs data to disk (eliminates need for explicit sync)
func openDirectIO(path string) (*os.File, error) {
	// Ensure parent directory exists
	// Open with O_DIRECT, O_DSYNC, O_WRONLY, O_APPEND, O_CREAT
	fd, err := syscall.Open(path,
		syscall.O_WRONLY|syscall.O_APPEND|syscall.O_CREAT|syscall.O_DIRECT|syscall.O_DSYNC,
		0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file with O_DIRECT: %w", err)
	}

	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to create file descriptor")
	}

	return file, nil
}

// allocAlignedBuffer allocates a byte slice aligned to filesystem block size (4096 bytes for ext4) for O_DIRECT
func allocAlignedBuffer(size int) []byte {
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

// writeAligned writes data to file ensuring filesystem block size alignment (4096 bytes for ext4)
// Pads data if necessary to meet alignment requirements
// Uses pooled buffers to eliminate allocations (was 290 MB/sec!)
func writeAligned(file *os.File, data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	// Calculate aligned size (round up to alignment boundary)
	alignedSize := ((len(data) + alignmentSize - 1) / alignmentSize) * alignmentSize

	// If data is already aligned, write directly
	if len(data) == alignedSize {
		n, err := file.Write(data)
		if err != nil {
			return n, fmt.Errorf("direct I/O write failed: %w", err)
		}
		return n, nil
	}

	// Get buffer from pool (eliminates 290 MB/sec allocations!)
	bufPtr := flushBufferPool.Get().(*[]byte)
	alignedBuf := *bufPtr
	defer flushBufferPool.Put(bufPtr)

	// Ensure buffer is large enough
	if cap(alignedBuf) < alignedSize {
		// Pool buffer too small, allocate new one (should be rare)
		alignedBuf = allocAlignedBuffer(alignedSize)
	} else {
		// Reuse pooled buffer
		alignedBuf = alignedBuf[:alignedSize]
	}

	// Copy data and zero out padding
	copy(alignedBuf, data)
	for i := len(data); i < alignedSize; i++ {
		alignedBuf[i] = 0
	}

	n, err := file.Write(alignedBuf[:alignedSize])
	if err != nil {
		return n, fmt.Errorf("direct I/O write failed: %w", err)
	}

	// Return actual data written (not including padding)
	if n > len(data) {
		return len(data), nil
	}
	return n, nil
}

// isAddressAligned checks if a buffer's memory address is aligned to alignmentSize
func isAddressAligned(buf []byte) bool {
	if len(buf) == 0 {
		return true
	}
	addr := uintptr(unsafe.Pointer(&buf[0]))
	return addr%alignmentSize == 0
}

// writevAligned writes multiple buffers to file in a single vectored I/O operation
// Uses true syscall.Writev() - NO memory copy, just pointers!
// This is the OPTIMAL implementation - reduces 8 syscalls to 1 with zero copy overhead
// IMPORTANT: With O_DIRECT, buffers must be aligned to filesystem block size (4096 bytes for ext4)
// File offset alignment is ensured by always writing aligned sizes (file starts at 0, stays aligned)
func writevAligned(file *os.File, buffers [][]byte) (int, error) {
	if len(buffers) == 0 {
		return 0, nil
	}

	// Prepare aligned buffers - each buffer aligned independently
	var alignedBuffers [][]byte
	var totalActualSize int
	var pooledBuffers []*[]byte // Track pooled buffers to return after write

	for _, buf := range buffers {
		if len(buf) == 0 {
			continue
		}

		totalActualSize += len(buf)
		alignedSize := ((len(buf) + alignmentSize - 1) / alignmentSize) * alignmentSize

		// Check both size AND address alignment for O_DIRECT
		// O_DIRECT requires: (1) buffer size must be multiple of filesystem block size (4096 bytes for ext4)
		//                    (2) buffer memory address must be aligned to filesystem block size
		if len(buf) == alignedSize && isAddressAligned(buf) {
			// Already aligned in both size and address, use directly (zero copy!)
			alignedBuffers = append(alignedBuffers, buf)
		} else {
			// Buffer is NOT properly aligned (either size or address misaligned)
			// Fix by copying to a properly aligned buffer from pool or allocating new one
			bufPtr := flushBufferPool.Get().(*[]byte)
			paddedBuf := *bufPtr

			if cap(paddedBuf) < alignedSize {
				// Pool buffer too small, allocate new aligned buffer
				flushBufferPool.Put(bufPtr)
				paddedBuf = allocAlignedBuffer(alignedSize)
			} else {
				// Use pooled buffer (already address-aligned from allocAlignedBuffer())
				// Slicing preserves address alignment since base address doesn't change
				paddedBuf = paddedBuf[:alignedSize]
				pooledBuffers = append(pooledBuffers, bufPtr)
			}

			// Copy data into aligned buffer and zero-pad to aligned size
			copy(paddedBuf, buf)
			// Zero out padding bytes (compiler optimizes this loop)
			for i := len(buf); i < alignedSize; i++ {
				paddedBuf[i] = 0
			}

			alignedBuffers = append(alignedBuffers, paddedBuf)
		}
	}

	// Return pooled buffers after write completes
	defer func() {
		for _, bufPtr := range pooledBuffers {
			flushBufferPool.Put(bufPtr)
		}
	}()

	if len(alignedBuffers) == 0 {
		return 0, nil
	}

	// Single vectored write syscall - kernel reads from multiple buffers!
	// unix.Writev takes [][]byte directly - NO iovec creation needed!
	n, err := unix.Writev(int(file.Fd()), alignedBuffers)
	if err != nil {
		return n, fmt.Errorf("vectored I/O write failed: %w", err)
	}

	// Return actual data written (not including padding)
	if n > totalActualSize {
		return totalActualSize, nil
	}
	return n, nil
}

// isAligned checks if a size is aligned to the required boundary
func isAligned(size int) bool {
	return size%alignmentSize == 0
}

// alignSize rounds up size to the nearest alignment boundary
func alignSize(size int) int {
	return ((size + alignmentSize - 1) / alignmentSize) * alignmentSize
}
