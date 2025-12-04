package logger

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ShardedDoubleBufferMutexLogger implements Logger using double-buffered sharded sets with mutex-based swapping
type ShardedDoubleBufferMutexLogger struct {
	// Two sets of sharded buffers
	setA *ShardedBufferSet
	setB *ShardedBufferSet

	// Mutex for set swapping
	mu sync.RWMutex

	// Active set (protected by mu)
	activeSet *ShardedBufferSet

	// File for writing logs
	file *os.File

	// Channel for flush requests
	flushChan chan *ShardedBufferSet

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
}

// newShardedDoubleBufferMutexLogger creates a new sharded double buffer logger with mutex
func newShardedDoubleBufferMutexLogger(config Config, file *os.File) (*ShardedDoubleBufferMutexLogger, error) {
	numShards := config.NumShards
	if numShards <= 0 {
		numShards = 16 // Default
	}

	// Create two buffer sets
	setA := NewShardedBufferSet(config.BufferSize, numShards, 0)
	setB := NewShardedBufferSet(config.BufferSize, numShards, 1)

	sdbml := &ShardedDoubleBufferMutexLogger{
		setA:      setA,
		setB:      setB,
		activeSet: setA,
		file:      file,
		flushChan: make(chan *ShardedBufferSet, 2), // Buffer for both sets
		ticker:    time.NewTicker(config.FlushInterval),
		done:      make(chan struct{}),
		semaphore: make(chan struct{}, 1),
		config:    config,
	}

	sdbml.nextID.Store(2) // Start from 2 since setA=0, setB=1

	// Start flush worker
	go sdbml.flushWorker()

	// Start periodic checker
	go sdbml.periodicChecker()

	return sdbml, nil
}

// Log writes a log message to the active set
func (sdbml *ShardedDoubleBufferMutexLogger) Log(msg string) {
	// Add newline if not present
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		msg = msg + "\n"
	}

	data := []byte(msg)
	sdbml.stats.totalLogs.Add(1)

	// Acquire read lock to access active set
	sdbml.mu.RLock()
	currentSet := sdbml.activeSet
	n, needsFlush, _ := currentSet.Write(data)
	sdbml.mu.RUnlock()

	// If write was successful, we're done
	if n > 0 {
		// Check if we need to trigger a swap
		if needsFlush {
			sdbml.triggerSwap()
		}
		return
	}

	// Write failed (buffer full), trigger swap and retry
	if needsFlush {
		sdbml.triggerSwap()
	}

	// Retry write after swap
	sdbml.mu.RLock()
	currentSet = sdbml.activeSet
	n, _, _ = currentSet.Write(data)
	sdbml.mu.RUnlock()

	if n == 0 {
		sdbml.stats.droppedLogs.Add(1)
	}
}

// Logf writes a formatted log message
func (sdbml *ShardedDoubleBufferMutexLogger) Logf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	sdbml.Log(msg)
}

// triggerSwap swaps the active and inactive sets
func (sdbml *ShardedDoubleBufferMutexLogger) triggerSwap() {
	sdbml.mu.Lock()
	defer sdbml.mu.Unlock()

	// Get the current active set
	oldSet := sdbml.activeSet

	// Swap to the other set
	if sdbml.activeSet == sdbml.setA {
		sdbml.activeSet = sdbml.setB
	} else {
		sdbml.activeSet = sdbml.setA
	}

	sdbml.stats.setSwaps.Add(1)

	// Queue old set for flushing if it has data
	if oldSet.HasData() {
		select {
		case sdbml.flushChan <- oldSet:
			// Queued successfully
		default:
			// Channel full, skip this flush
		}
	}
}

// periodicChecker checks for periodic flushes
func (sdbml *ShardedDoubleBufferMutexLogger) periodicChecker() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] periodicChecker: %v\n", r)
		}
	}()

	for {
		select {
		case <-sdbml.done:
			return
		case <-sdbml.ticker.C:
			// Trigger swap for periodic flush
			sdbml.mu.RLock()
			hasData := sdbml.activeSet.HasData()
			sdbml.mu.RUnlock()

			if hasData {
				sdbml.triggerSwap()
			}
		}
	}
}

// flushWorker handles background flushing
func (sdbml *ShardedDoubleBufferMutexLogger) flushWorker() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] flushWorker: %v\n", r)
		}
	}()

	for {
		select {
		case <-sdbml.done:
			// Flush any remaining sets
			sdbml.flushFinal()
			return
		case set := <-sdbml.flushChan:
			sdbml.flushSet(set)
		}
	}
}

// flushSet writes all shards in a set to disk
func (sdbml *ShardedDoubleBufferMutexLogger) flushSet(set *ShardedBufferSet) {
	// Use semaphore to serialize disk writes
	select {
	case sdbml.semaphore <- struct{}{}:
		defer func() { <-sdbml.semaphore }()
	default:
		// Another flush in progress, skip
		return
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] flushSet: %v\n", r)
			sdbml.stats.flushErrors.Add(1)
		}
	}()

	// Flush each shard in the set
	for _, shard := range set.Shards() {
		data := shard.Bytes()
		if len(data) == 0 {
			continue
		}

		// Write to file
		n, err := sdbml.file.Write(data)
		if err != nil {
			sdbml.stats.flushErrors.Add(1)
			continue
		}

		sdbml.stats.bytesWritten.Add(int64(n))
	}

	sdbml.stats.totalFlushes.Add(1)

	// Reset all shards in the set
	set.SetID(sdbml.nextID.Add(1))
	set.ResetAll()
}

// flushFinal flushes all remaining data during shutdown
func (sdbml *ShardedDoubleBufferMutexLogger) flushFinal() {
	// Flush any pending sets in the channel
	for {
		select {
		case set := <-sdbml.flushChan:
			sdbml.flushSet(set)
		default:
			goto flushBoth
		}
	}

flushBoth:
	// Flush both sets if they have data
	if sdbml.setA.HasData() {
		sdbml.flushSet(sdbml.setA)
	}
	if sdbml.setB.HasData() {
		sdbml.flushSet(sdbml.setB)
	}
}

// Close gracefully shuts down the logger
func (sdbml *ShardedDoubleBufferMutexLogger) Close() error {
	// Check if already closed
	select {
	case <-sdbml.done:
		// Already closed
		return nil
	default:
		// Signal shutdown
		close(sdbml.done)
	}

	// Stop ticker
	sdbml.ticker.Stop()

	// Wait a bit for flush worker to finish
	time.Sleep(100 * time.Millisecond)

	// Sync and close file
	if sdbml.file != nil {
		sdbml.file.Sync()
		return sdbml.file.Close()
	}

	return nil
}

// Stats returns current logging statistics
func (sdbml *ShardedDoubleBufferMutexLogger) Stats() Statistics {
	return Statistics{
		TotalLogs:       sdbml.stats.totalLogs.Load(),
		TotalFlushes:    sdbml.stats.totalFlushes.Load(),
		DroppedLogs:     sdbml.stats.droppedLogs.Load(),
		BytesWritten:    sdbml.stats.bytesWritten.Load(),
		FlushErrors:     sdbml.stats.flushErrors.Load(),
		CurrentStrategy: ShardedDoubleBuffer,
	}
}
