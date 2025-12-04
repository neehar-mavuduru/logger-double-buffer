package logger

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

// CASShard represents a single shard using CAS for lock-free writes
type CASShard struct {
	data          []byte
	offset        int32 // Atomic CAS
	capacity      int32
	id            uint32
	readyForFlush atomic.Bool
}

// ShardedCASBuffer manages multiple CAS shards
type ShardedCASBuffer struct {
	shards        []*CASShard
	numShards     int
	counter       atomic.Uint64
	totalCapacity int32
}

// NewCASShard creates a new CAS-based shard
func NewCASShard(capacity int, id uint32) *CASShard {
	return &CASShard{
		data:     make([]byte, capacity),
		offset:   0,
		capacity: int32(capacity),
		id:       id,
	}
}

// NewShardedCASBuffer creates a new sharded CAS buffer
func NewShardedCASBuffer(totalCapacity int, numShards int) *ShardedCASBuffer {
	if numShards <= 0 {
		numShards = 16 // Default
	}

	shardCapacity := totalCapacity / numShards
	if shardCapacity < 64*1024 {
		// Ensure minimum 64KB per shard
		shardCapacity = 64 * 1024
		numShards = totalCapacity / shardCapacity
		if numShards == 0 {
			numShards = 1
		}
	}

	shards := make([]*CASShard, numShards)
	for i := 0; i < numShards; i++ {
		shards[i] = NewCASShard(shardCapacity, uint32(i))
	}

	return &ShardedCASBuffer{
		shards:        shards,
		numShards:     numShards,
		totalCapacity: int32(totalCapacity),
	}
}

// Write writes data to a shard using round-robin and CAS
func (scb *ShardedCASBuffer) Write(p []byte) (n int, needsFlush bool, shardID int) {
	if len(p) == 0 {
		return 0, false, -1
	}

	// Round-robin shard selection
	shardIdx := int(scb.counter.Add(1) % uint64(scb.numShards))
	shard := scb.shards[shardIdx]

	n, needsFlush = shard.Write(p)
	return n, needsFlush, shardIdx
}

// GetShard returns a specific shard
func (scb *ShardedCASBuffer) GetShard(idx int) *CASShard {
	if idx < 0 || idx >= scb.numShards {
		return nil
	}
	return scb.shards[idx]
}

// NumShards returns the number of shards
func (scb *ShardedCASBuffer) NumShards() int {
	return scb.numShards
}

// Shards returns all shards
func (scb *ShardedCASBuffer) Shards() []*CASShard {
	return scb.shards
}

// Write writes data to the shard using CAS (lock-free)
func (cs *CASShard) Write(p []byte) (n int, needsFlush bool) {
	if len(p) == 0 {
		return 0, false
	}

	// Check if already marked for flush
	if cs.readyForFlush.Load() {
		return 0, true
	}

	// Try to reserve space using CAS
	currentOffset := atomic.LoadInt32(&cs.offset)
	newOffset := currentOffset + int32(len(p))

	// Check if we have enough space
	if newOffset > cs.capacity {
		cs.readyForFlush.Store(true)
		return 0, true
	}

	// Try to atomically update the offset
	if !atomic.CompareAndSwapInt32(&cs.offset, currentOffset, newOffset) {
		// Another goroutine updated the offset, retry
		return cs.Write(p)
	}

	// Copy data to the reserved space
	copy(cs.data[currentOffset:newOffset], p)

	// Check if buffer is now full or nearly full
	if newOffset >= cs.capacity {
		cs.readyForFlush.Store(true)
		return len(p), true
	}

	return len(p), false
}

// Bytes returns the current data in the shard
func (cs *CASShard) Bytes() []byte {
	currentOffset := atomic.LoadInt32(&cs.offset)
	if currentOffset == 0 {
		return nil
	}
	return cs.data[:currentOffset]
}

// Reset clears the shard for reuse
func (cs *CASShard) Reset() {
	atomic.StoreInt32(&cs.offset, 0)
	cs.readyForFlush.Store(false)
}

// Offset returns the current offset
func (cs *CASShard) Offset() int32 {
	return atomic.LoadInt32(&cs.offset)
}

// IsFull returns whether the shard is ready for flushing
func (cs *CASShard) IsFull() bool {
	return cs.readyForFlush.Load()
}

// ID returns the shard identifier
func (cs *CASShard) ID() uint32 {
	return cs.id
}

// SetID updates the shard identifier
func (cs *CASShard) SetID(id uint32) {
	cs.id = id
}

// Capacity returns the shard capacity
func (cs *CASShard) Capacity() int32 {
	return cs.capacity
}

// ShardedCASLogger implements Logger using sharded buffers with CAS operations
type ShardedCASLogger struct {
	// Sharded CAS buffers
	shardedBuffer *ShardedCASBuffer

	// File for writing logs
	file *os.File

	// Channel for flush requests (shard index)
	flushChan chan int

	// Ticker for periodic flushing
	ticker *time.Ticker

	// Channel for shutdown signal
	done chan struct{}

	// Semaphore to prevent concurrent flushes
	semaphore chan struct{}

	// Configuration
	config Config

	// Statistics
	stats struct {
		totalLogs    atomic.Int64
		totalFlushes atomic.Int64
		droppedLogs  atomic.Int64
		bytesWritten atomic.Int64
		flushErrors  atomic.Int64
	}

	// Next buffer ID for tracking
	nextID atomic.Uint32
}

// newShardedCASLogger creates a new sharded CAS logger
func newShardedCASLogger(config Config, file *os.File) (*ShardedCASLogger, error) {
	numShards := config.NumShards
	if numShards <= 0 {
		numShards = 16 // Default
	}

	scl := &ShardedCASLogger{
		shardedBuffer: NewShardedCASBuffer(config.BufferSize, numShards),
		file:          file,
		flushChan:     make(chan int, numShards*2),
		ticker:        time.NewTicker(config.FlushInterval),
		done:          make(chan struct{}),
		semaphore:     make(chan struct{}, 1),
		config:        config,
	}

	scl.nextID.Store(uint32(numShards))

	// Start flush worker
	go scl.flushWorker()

	// Start periodic flush checker
	go scl.periodicFlushChecker()

	return scl, nil
}

// Log writes a log message to a shard using CAS
func (scl *ShardedCASLogger) Log(msg string) {
	// Add newline if not present
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		msg = msg + "\n"
	}

	data := []byte(msg)
	scl.stats.totalLogs.Add(1)

	// Write to sharded CAS buffer
	n, needsFlush, shardID := scl.shardedBuffer.Write(data)

	if n == 0 && needsFlush {
		// Shard is full, request flush
		select {
		case scl.flushChan <- shardID:
			// Flush request queued

			// Retry write to same shard
			shard := scl.shardedBuffer.GetShard(shardID)
			if shard != nil {
				n2, _ := shard.Write(data)
				if n2 == 0 {
					scl.stats.droppedLogs.Add(1)
				}
			}
		default:
			// Flush channel full
			scl.stats.droppedLogs.Add(1)
			// fmt.Fprintf(os.Stderr, "[LOGGER ERROR] Flush channel full, dropped log\n")
		}
	}
}

// Logf writes a formatted log message
func (scl *ShardedCASLogger) Logf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	scl.Log(msg)
}

// periodicFlushChecker checks all shards periodically
func (scl *ShardedCASLogger) periodicFlushChecker() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] periodicFlushChecker: %v\n", r)
		}
	}()

	for {
		select {
		case <-scl.done:
			return
		case <-scl.ticker.C:
			// Check all shards for pending data
			for i := 0; i < scl.shardedBuffer.NumShards(); i++ {
				shard := scl.shardedBuffer.GetShard(i)
				if shard != nil && shard.Offset() > 0 {
					select {
					case scl.flushChan <- i:
						// Flush request queued
					default:
						// Channel full, skip this shard
					}
				}
			}
		}
	}
}

// flushWorker handles background flushing
func (scl *ShardedCASLogger) flushWorker() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] flushWorker: %v\n", r)
		}
	}()

	for {
		select {
		case <-scl.done:
			// Flush any remaining shards
			scl.flushFinal()
			return
		case shardIdx := <-scl.flushChan:
			shard := scl.shardedBuffer.GetShard(shardIdx)
			if shard != nil {
				scl.flushShard(shard)
			}
		}
	}
}

// flushShard writes a shard to disk
func (scl *ShardedCASLogger) flushShard(shard *CASShard) {
	// Use semaphore to serialize disk writes
	select {
	case scl.semaphore <- struct{}{}:
		defer func() { <-scl.semaphore }()
	default:
		// Another flush in progress, skip
		return
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] flushShard: %v\n", r)
			scl.stats.flushErrors.Add(1)
		}
	}()

	data := shard.Bytes()
	if len(data) == 0 {
		return
	}

	// Write to file
	n, err := scl.file.Write(data)
	if err != nil {
		// fmt.Fprintf(os.Stderr, "[LOGGER ERROR] Failed to write to file: %v\n", err)
		scl.stats.flushErrors.Add(1)
		return
	}

	scl.stats.bytesWritten.Add(int64(n))
	scl.stats.totalFlushes.Add(1)

	// Update shard ID and reset
	shard.SetID(scl.nextID.Add(1))
	shard.Reset()
}

// flushFinal flushes all remaining data during shutdown
func (scl *ShardedCASLogger) flushFinal() {
	// Flush any pending requests in the channel
	for {
		select {
		case shardIdx := <-scl.flushChan:
			shard := scl.shardedBuffer.GetShard(shardIdx)
			if shard != nil {
				scl.flushShard(shard)
			}
		default:
			goto flushAll
		}
	}

flushAll:
	// Flush all shards with data
	for _, shard := range scl.shardedBuffer.Shards() {
		if shard.Offset() > 0 {
			scl.flushShard(shard)
		}
	}
}

// Close gracefully shuts down the logger
func (scl *ShardedCASLogger) Close() error {
	// Check if already closed
	select {
	case <-scl.done:
		// Already closed
		return nil
	default:
		// Signal shutdown
		close(scl.done)
	}

	// Stop ticker
	scl.ticker.Stop()

	// Wait a bit for flush worker to finish
	time.Sleep(100 * time.Millisecond)

	// Sync and close file
	if scl.file != nil {
		scl.file.Sync()
		return scl.file.Close()
	}

	return nil
}

// Stats returns current logging statistics
func (scl *ShardedCASLogger) Stats() Statistics {
	return Statistics{
		TotalLogs:       scl.stats.totalLogs.Load(),
		TotalFlushes:    scl.stats.totalFlushes.Load(),
		DroppedLogs:     scl.stats.droppedLogs.Load(),
		BytesWritten:    scl.stats.bytesWritten.Load(),
		FlushErrors:     scl.stats.flushErrors.Load(),
		CurrentStrategy: ShardedCAS,
	}
}
