package asyncloguploader

import (
	"encoding/binary"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// headerOffset is the number of bytes reserved at the start of each buffer for the shard header
const headerOffset = 8

// Shard represents a single shard with double buffer
// Merges Buffer and Shard functionality into single struct
type Shard struct {
	// Double buffer: two buffers (A and B) allocated via anonymous mmap
	bufferA []byte // mmap'd buffer A
	bufferB []byte // mmap'd buffer B

	// Active buffer pointer (atomically swapped)
	activeBuffer atomic.Pointer[[]byte]

	// Write state (for active buffer)
	offsetA atomic.Int32 // Offset in bufferA
	offsetB atomic.Int32 // Offset in bufferB

	// Capacity (same for both buffers, includes headerOffset)
	capacity int32

	// Mutex for flush operations
	mu sync.Mutex

	// Shard identifier
	id uint32

	// Swap coordination
	swapping      atomic.Bool
	readyForFlush atomic.Bool
	swapSemaphore chan struct{} // Per-shard semaphore for swap coordination (buffer size 1)

	// Inflight write tracking (for both buffers)
	inflightA atomic.Int64 // Number of concurrent writes in progress for bufferA
	inflightB atomic.Int64 // Number of concurrent writes in progress for bufferB

	// Cleanup functions for mmap (called on Close)
	cleanupA func()
	cleanupB func()
}

// NewShard creates a new shard with double buffer using anonymous mmap
func NewShard(capacity int, id uint32) (*Shard, error) {

	alignedCap := alignSize(capacity)

	// Allocate bufferA via anonymous mmap
	bufferA, cleanupA, err := allocMmapBuffer(alignedCap)
	if err != nil {
		return nil, err
	}

	// Allocate bufferB via anonymous mmap
	bufferB, cleanupB, err := allocMmapBuffer(alignedCap)
	if err != nil {
		cleanupA()
		unix.Munmap(bufferA)
		return nil, err
	}

	s := &Shard{
		bufferA:       bufferA,
		bufferB:       bufferB,
		capacity:      int32(alignedCap),
		id:            id,
		cleanupA:      cleanupA,
		cleanupB:      cleanupB,
		swapSemaphore: make(chan struct{}, 1), // Per-shard semaphore (buffer size 1)
	}

	// Set bufferA as initial active buffer
	s.activeBuffer.Store(&s.bufferA)

	// Initialize offsets to skip header
	s.offsetA.Store(headerOffset)
	s.offsetB.Store(headerOffset)

	return s, nil
}

// allocMmapBuffer allocates a buffer using anonymous mmap
// Returns the buffer, cleanup function, and error
func allocMmapBuffer(size int) ([]byte, func(), error) {
	// Round up to page size alignment
	alignedSize := alignSize(size)

	// Create anonymous private mapping
	data, err := unix.Mmap(
		-1, 0,
		alignedSize,
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_PRIVATE|unix.MAP_ANONYMOUS,
	)
	if err != nil {
		return nil, nil, err
	}

	// Cleanup function keeps buffer alive during use
	cleanup := func() {
		runtime.KeepAlive(data)
		// Don't unmap during normal operation - reuse buffers
	}

	// Set finalizer for cleanup on GC
	runtime.SetFinalizer(&data, func(d *[]byte) {
		if d != nil && len(*d) > 0 {
			unix.Munmap(*d)
		}
	})

	return data, cleanup, nil
}

// alignSize rounds up size to the nearest alignment boundary (4096 bytes)
func alignSize(size int) int {
	const alignmentSize = 4096
	return ((size + alignmentSize - 1) / alignmentSize) * alignmentSize
}

// Write writes data to the active buffer (lock-free hot path)
// Prepends a 4-byte length prefix (little-endian) before the log data
// Returns the number of bytes written (including length prefix) and whether the buffer needs flushing
func (s *Shard) Write(p []byte) (n int, needsFlush bool) {
	if len(p) == 0 {
		return 0, false
	}

	// Get active buffer
	activeBufPtr := s.activeBuffer.Load()
	if activeBufPtr == nil {
		// Active buffer is nil - shard may be in invalid state
		return 0, true
	}

	// Determine which offset to use based on active buffer
	var offset *atomic.Int32
	if activeBufPtr == &s.bufferA {
		offset = &s.offsetA
	} else {
		offset = &s.offsetB
	}

	// Reserve space for: 4-byte length prefix + log data
	const lengthPrefixSize = 4
	totalSize := lengthPrefixSize + len(p)

	// Try to reserve space in the buffer (starting after the 8-byte header)
	currentOffset := offset.Load()
	newOffset := currentOffset + int32(totalSize)

	// Check if we have enough space in the active buffer
	// IMPORTANT: Check buffer space BEFORE checking readyForFlush
	// This allows writes to the new active buffer after swap, even if readyForFlush is still true
	if newOffset >= s.capacity {
		// Active buffer is full - mark for flush
		s.readyForFlush.Store(true)
		return 0, true
	}

	// If readyForFlush is true but active buffer has space, it means:
	// - A swap just happened and the inactive buffer is being flushed
	// - The new active buffer is empty and ready for writes
	// - We should allow writes to proceed
	// Note: readyForFlush only prevents writes when BOTH buffers are full

	// Try to atomically update the offset (CAS)
	if !offset.CompareAndSwap(currentOffset, newOffset) {
		// Another goroutine updated the offset, retry
		return s.Write(p)
	}

	// Get active buffer slice
	activeBuf := *activeBufPtr

	// Bounds check: ensure we have enough space in buffer
	if int(newOffset) > len(activeBuf) {
		// Buffer overflow detected - reset offset and mark for flush
		offset.Store(currentOffset)
		s.readyForFlush.Store(true)
		return 0, true
	}

	// Increment inflight counter for the active buffer
	var inflight *atomic.Int64
	if activeBufPtr == &s.bufferA {
		inflight = &s.inflightA
	} else {
		inflight = &s.inflightB
	}
	inflight.Add(1)

	// Write 4-byte length prefix (little-endian uint32)
	binary.LittleEndian.PutUint32(activeBuf[currentOffset:currentOffset+lengthPrefixSize], uint32(len(p)))

	// Use copy() for data copy - Go's copy() is already highly optimized and safe
	// The performance difference vs memmove is negligible (<10-20% for large buffers)
	// and not worth the complexity and risk of unsafe pointer manipulation
	copy(activeBuf[currentOffset+lengthPrefixSize:newOffset], p)

	// Decrement inflight counter: write completed
	inflight.Add(-1)

	// Check if buffer is now full or nearly full (within 10%)
	if newOffset >= s.capacity*9/10 {
		s.readyForFlush.Store(true)
		return totalSize, true
	}

	return totalSize, false
}

// trySwap attempts to swap the active buffer (CAS-protected)
func (s *Shard) trySwap() {
	// Check if already swapping
	if !s.swapping.CompareAndSwap(false, true) {
		return // Another goroutine is swapping
	}
	defer s.swapping.Store(false)

	// Get current active buffer
	currentBufPtr := s.activeBuffer.Load()
	if currentBufPtr == nil {
		return
	}

	// Determine next buffer
	var nextBufPtr *[]byte
	if currentBufPtr == &s.bufferA {
		nextBufPtr = &s.bufferB
	} else {
		nextBufPtr = &s.bufferA
	}

	// Atomically swap active buffer
	if !s.activeBuffer.CompareAndSwap(currentBufPtr, nextBufPtr) {
		// Swap failed, another goroutine beat us
		return
	}

	// Mark shard as ready for flush
	s.readyForFlush.Store(true)
}

// GetData returns the data from the inactive buffer (the one being flushed)
// Should only be called when shard is ready for flush
// Waits for inflight == 0 or timeout expires
// Returns the full capacity slice and whether all writes completed
func (s *Shard) GetData(timeout time.Duration) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get the buffer that was swapped out (inactive)
	activeBufPtr := s.activeBuffer.Load()
	var inactiveBuf []byte
	var inflight *atomic.Int64

	if activeBufPtr == nil || activeBufPtr == &s.bufferA {
		// Active is A or nil, so inactive is B
		inactiveBuf = s.bufferB
		inflight = &s.inflightB
	} else {
		// Active is B, so inactive is A
		inactiveBuf = s.bufferA
		inflight = &s.inflightA
	}

	if inactiveBuf == nil {
		return nil, false
	}

	// Wait for all inflight writes to complete
	deadline := time.Now().Add(timeout)
	const checkInterval = 50 * time.Microsecond

	for time.Now().Before(deadline) {
		if inflight.Load() == 0 {
			// All writes have completed
			return inactiveBuf[:s.capacity], true
		}

		// Writes still in progress, yield CPU
		runtime.Gosched()
		time.Sleep(checkInterval)
	}

	// Timeout expired: flush anyway (may contain incomplete last write)
	return inactiveBuf[:s.capacity], false
}

// GetInactiveOffset returns the offset of the inactive buffer (the one being flushed)
func (s *Shard) GetInactiveOffset() int32 {
	activeBufPtr := s.activeBuffer.Load()
	if activeBufPtr == nil || activeBufPtr == &s.bufferA {
		return s.offsetB.Load()
	}
	return s.offsetA.Load()
}

// Reset clears the inactive buffer after flush
func (s *Shard) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get inactive buffer and reset it
	activeBufPtr := s.activeBuffer.Load()
	var inactiveOffset *atomic.Int32
	var inflight *atomic.Int64

	if activeBufPtr == nil || activeBufPtr == &s.bufferA {
		// Active is A or nil, so inactive is B
		inactiveOffset = &s.offsetB
		inflight = &s.inflightB
	} else {
		// Active is B, so inactive is A
		inactiveOffset = &s.offsetA
		inflight = &s.inflightA
	}

	if inactiveOffset != nil {
		inactiveOffset.Store(headerOffset)
		inflight.Store(0)
	}
	s.readyForFlush.Store(false)
}

// ID returns the shard identifier
func (s *Shard) ID() uint32 {
	return s.id
}

// IsFull returns true if the shard is ready for flush
func (s *Shard) IsFull() bool {
	return s.readyForFlush.Load()
}

// HasData returns true if the inactive buffer has data
func (s *Shard) HasData() bool {
	activeBufPtr := s.activeBuffer.Load()
	var inactiveOffset *atomic.Int32
	if activeBufPtr == nil || activeBufPtr == &s.bufferA {
		inactiveOffset = &s.offsetB
	} else {
		inactiveOffset = &s.offsetA
	}
	return inactiveOffset.Load() > headerOffset
}

// Offset returns the offset of the active buffer
func (s *Shard) Offset() int32 {
	activeBufPtr := s.activeBuffer.Load()
	if activeBufPtr == nil || activeBufPtr == &s.bufferA {
		return s.offsetA.Load()
	}
	return s.offsetB.Load()
}

// Capacity returns the capacity of buffers
func (s *Shard) Capacity() int32 {
	return s.capacity
}

// Close releases resources associated with the shard
func (s *Shard) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cleanupA != nil {
		s.cleanupA()
	}
	if s.cleanupB != nil {
		s.cleanupB()
	}

	// Unmap buffers
	if len(s.bufferA) > 0 {
		unix.Munmap(s.bufferA)
	}
	if len(s.bufferB) > 0 {
		unix.Munmap(s.bufferB)
	}
}
