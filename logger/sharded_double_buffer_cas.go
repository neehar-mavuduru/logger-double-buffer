package logger

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

// ShardedDoubleBufferCASLogger implements Logger using double-buffered sharded sets with atomic CAS swapping
type ShardedDoubleBufferCASLogger struct {
	// Two sets of sharded CAS buffers
	setA *ShardedCASBufferSet
	setB *ShardedCASBufferSet

	// Active set pointer (atomically swapped)
	activeSet atomic.Pointer[ShardedCASBufferSet]

	// File for writing logs
	file *os.File

	// Channel for flush requests
	flushChan chan *ShardedCASBufferSet

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
		setSwaps     atomic.Int64
	}

	// Next set ID for tracking
	nextID atomic.Uint32

	// Swap in progress flag
	swapping atomic.Bool
}

// newShardedDoubleBufferCASLogger creates a new sharded double buffer logger with CAS
func newShardedDoubleBufferCASLogger(config Config, file *os.File) (*ShardedDoubleBufferCASLogger, error) {
	numShards := config.NumShards
	if numShards <= 0 {
		numShards = 16 // Default
	}

	// Create two buffer sets
	setA := NewShardedCASBufferSet(config.BufferSize, numShards, 0)
	setB := NewShardedCASBufferSet(config.BufferSize, numShards, 1)

	sdbcl := &ShardedDoubleBufferCASLogger{
		setA:      setA,
		setB:      setB,
		file:      file,
		flushChan: make(chan *ShardedCASBufferSet, 2), // Buffer for both sets
		ticker:    time.NewTicker(config.FlushInterval),
		done:      make(chan struct{}),
		semaphore: make(chan struct{}, 1),
		config:    config,
	}

	sdbcl.activeSet.Store(setA)
	sdbcl.nextID.Store(2) // Start from 2 since setA=0, setB=1

	// Start flush worker
	go sdbcl.flushWorker()

	// Start periodic checker
	go sdbcl.periodicChecker()

	return sdbcl, nil
}

// Log writes a log message to the active set
func (sdbcl *ShardedDoubleBufferCASLogger) Log(msg string) {
	// Add newline if not present
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		msg = msg + "\n"
	}

	data := []byte(msg)
	sdbcl.stats.totalLogs.Add(1)

	// Get current active set
	currentSet := sdbcl.activeSet.Load()
	n, needsFlush, _ := currentSet.Write(data)

	// If write was successful, we're done
	if n > 0 {
		// Check if we need to trigger a swap
		if needsFlush {
			sdbcl.triggerSwap()
		}
		return
	}

	// Write failed (buffer full), trigger swap and retry
	if needsFlush {
		sdbcl.triggerSwap()
	}

	// Retry write after swap
	currentSet = sdbcl.activeSet.Load()
	n, _, _ = currentSet.Write(data)

	if n == 0 {
		sdbcl.stats.droppedLogs.Add(1)
	}
}

// Logf writes a formatted log message
func (sdbcl *ShardedDoubleBufferCASLogger) Logf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	sdbcl.Log(msg)
}

// triggerSwap atomically swaps the active and inactive sets
func (sdbcl *ShardedDoubleBufferCASLogger) triggerSwap() {
	// Use CAS to ensure only one swap happens at a time
	if !sdbcl.swapping.CompareAndSwap(false, true) {
		// Another swap is in progress
		return
	}
	defer sdbcl.swapping.Store(false)

	// Get the current active set
	oldSet := sdbcl.activeSet.Load()

	// Determine the new set
	var newSet *ShardedCASBufferSet
	if oldSet == sdbcl.setA {
		newSet = sdbcl.setB
	} else {
		newSet = sdbcl.setA
	}

	// Atomically swap the active set
	if !sdbcl.activeSet.CompareAndSwap(oldSet, newSet) {
		// Swap failed, another goroutine did it
		return
	}

	sdbcl.stats.setSwaps.Add(1)

	// Queue old set for flushing if it has data
	if oldSet.HasData() {
		select {
		case sdbcl.flushChan <- oldSet:
			// Queued successfully
		default:
			// Channel full, skip this flush
		}
	}
}

// periodicChecker checks for periodic flushes
func (sdbcl *ShardedDoubleBufferCASLogger) periodicChecker() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] periodicChecker: %v\n", r)
		}
	}()

	for {
		select {
		case <-sdbcl.done:
			return
		case <-sdbcl.ticker.C:
			// Trigger swap for periodic flush
			currentSet := sdbcl.activeSet.Load()
			if currentSet.HasData() {
				sdbcl.triggerSwap()
			}
		}
	}
}

// flushWorker handles background flushing
func (sdbcl *ShardedDoubleBufferCASLogger) flushWorker() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] flushWorker: %v\n", r)
		}
	}()

	for {
		select {
		case <-sdbcl.done:
			// Flush any remaining sets
			sdbcl.flushFinal()
			return
		case set := <-sdbcl.flushChan:
			sdbcl.flushSet(set)
		}
	}
}

// flushSet writes all shards in a set to disk
func (sdbcl *ShardedDoubleBufferCASLogger) flushSet(set *ShardedCASBufferSet) {
	// Use semaphore to serialize disk writes
	select {
	case sdbcl.semaphore <- struct{}{}:
		defer func() { <-sdbcl.semaphore }()
	default:
		// Another flush in progress, skip
		return
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] flushSet: %v\n", r)
			sdbcl.stats.flushErrors.Add(1)
		}
	}()

	// Flush each shard in the set
	for _, shard := range set.Shards() {
		data := shard.Bytes()
		if len(data) == 0 {
			continue
		}

		// Write to file
		n, err := sdbcl.file.Write(data)
		if err != nil {
			sdbcl.stats.flushErrors.Add(1)
			continue
		}

		sdbcl.stats.bytesWritten.Add(int64(n))
	}

	sdbcl.stats.totalFlushes.Add(1)

	// Reset all shards in the set
	set.SetID(sdbcl.nextID.Add(1))
	set.ResetAll()
}

// flushFinal flushes all remaining data during shutdown
func (sdbcl *ShardedDoubleBufferCASLogger) flushFinal() {
	// Flush any pending sets in the channel
	for {
		select {
		case set := <-sdbcl.flushChan:
			sdbcl.flushSet(set)
		default:
			goto flushBoth
		}
	}

flushBoth:
	// Flush both sets if they have data
	if sdbcl.setA.HasData() {
		sdbcl.flushSet(sdbcl.setA)
	}
	if sdbcl.setB.HasData() {
		sdbcl.flushSet(sdbcl.setB)
	}
}

// Close gracefully shuts down the logger
func (sdbcl *ShardedDoubleBufferCASLogger) Close() error {
	// Check if already closed
	select {
	case <-sdbcl.done:
		// Already closed
		return nil
	default:
		// Signal shutdown
		close(sdbcl.done)
	}

	// Stop ticker
	sdbcl.ticker.Stop()

	// Wait a bit for flush worker to finish
	time.Sleep(100 * time.Millisecond)

	// Sync and close file
	if sdbcl.file != nil {
		sdbcl.file.Sync()
		return sdbcl.file.Close()
	}

	return nil
}

// Stats returns current logging statistics
func (sdbcl *ShardedDoubleBufferCASLogger) Stats() Statistics {
	return Statistics{
		TotalLogs:       sdbcl.stats.totalLogs.Load(),
		TotalFlushes:    sdbcl.stats.totalFlushes.Load(),
		DroppedLogs:     sdbcl.stats.droppedLogs.Load(),
		BytesWritten:    sdbcl.stats.bytesWritten.Load(),
		FlushErrors:     sdbcl.stats.flushErrors.Load(),
		CurrentStrategy: ShardedDoubleBufferCAS,
	}
}
