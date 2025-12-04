package logger

import (
	"sync/atomic"
)

// LogBuffer represents a single buffer for log entries
type LogBuffer struct {
	// data is the pre-allocated byte slice
	data []byte

	// offset tracks the current write position (must use atomic operations)
	offset int32

	// capacity is the maximum buffer size
	capacity int32

	// id is the buffer identifier for debugging
	id uint32

	// readyForFlush indicates the buffer is full and needs flushing
	readyForFlush atomic.Bool
}

// NewLogBuffer creates a new log buffer with the given capacity and ID
func NewLogBuffer(capacity int, id uint32) *LogBuffer {
	return &LogBuffer{
		data:     make([]byte, capacity),
		offset:   0,
		capacity: int32(capacity),
		id:       id,
	}
}

// Write appends data to the buffer
// Returns the number of bytes written and whether the buffer needs flushing
func (b *LogBuffer) Write(p []byte) (n int, needsFlush bool) {
	if len(p) == 0 {
		return 0, false
	}

	// Check if buffer is already full
	if b.readyForFlush.Load() {
		return 0, true
	}

	// Try to reserve space in the buffer
	currentOffset := atomic.LoadInt32(&b.offset)
	newOffset := currentOffset + int32(len(p))

	// Check if we have enough space
	if newOffset > b.capacity {
		b.readyForFlush.Store(true)
		return 0, true
	}

	// Try to atomically update the offset
	if !atomic.CompareAndSwapInt32(&b.offset, currentOffset, newOffset) {
		// Another goroutine updated the offset, retry
		return b.Write(p)
	}

	// Copy data to the reserved space
	copy(b.data[currentOffset:newOffset], p)

	// Check if buffer is now full or nearly full
	if newOffset >= b.capacity {
		b.readyForFlush.Store(true)
		return len(p), true
	}

	return len(p), false
}

// Reset clears the buffer for reuse
func (b *LogBuffer) Reset() {
	atomic.StoreInt32(&b.offset, 0)
	b.readyForFlush.Store(false)
}

// Bytes returns the currently written portion of the buffer
func (b *LogBuffer) Bytes() []byte {
	currentOffset := atomic.LoadInt32(&b.offset)
	if currentOffset == 0 {
		return nil
	}
	return b.data[:currentOffset]
}

// IsFull returns true if the buffer is ready for flushing
func (b *LogBuffer) IsFull() bool {
	return b.readyForFlush.Load()
}

// ID returns the buffer identifier
func (b *LogBuffer) ID() uint32 {
	return b.id
}

// SetID updates the buffer identifier (used during buffer swap)
func (b *LogBuffer) SetID(id uint32) {
	b.id = id
}

// Offset returns the current write offset
func (b *LogBuffer) Offset() int32 {
	return atomic.LoadInt32(&b.offset)
}

// Capacity returns the buffer capacity
func (b *LogBuffer) Capacity() int32 {
	return b.capacity
}

