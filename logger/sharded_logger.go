package logger

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

// ShardedLogger implements Logger using sharded buffers with mutex-based locking
type ShardedLogger struct {
	// Sharded buffers
	shardedBuffer *ShardedBuffer

	// File for writing logs
	file *os.File

	// Channel for flush requests (one per shard)
	flushChan chan int // Shard index to flush

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

// newShardedLogger creates a new sharded logger
func newShardedLogger(config Config, file *os.File) (*ShardedLogger, error) {
	numShards := config.NumShards
	if numShards <= 0 {
		numShards = 16 // Default
	}

	sl := &ShardedLogger{
		shardedBuffer: NewShardedBuffer(config.BufferSize, numShards),
		file:          file,
		flushChan:     make(chan int, numShards*2), // Buffer for pending flush requests
		ticker:        time.NewTicker(config.FlushInterval),
		done:          make(chan struct{}),
		semaphore:     make(chan struct{}, 1),
		config:        config,
	}

	sl.nextID.Store(uint32(numShards))

	// Start flush worker
	go sl.flushWorker()

	// Start periodic flush checker
	go sl.periodicFlushChecker()

	return sl, nil
}

// Log writes a log message to a shard
func (sl *ShardedLogger) Log(msg string) {
	// Add newline if not present
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		msg = msg + "\n"
	}

	data := []byte(msg)
	sl.stats.totalLogs.Add(1)

	// Write to sharded buffer
	n, needsFlush, shardID := sl.shardedBuffer.Write(data)

	if n == 0 && needsFlush {
		// Shard is full, request flush
		select {
		case sl.flushChan <- shardID:
			// Flush request queued

			// Retry write to same shard after marking for flush
			shard := sl.shardedBuffer.GetShard(shardID)
			if shard != nil {
				// Try writing again (shard might have space for this message)
				n2, _ := shard.Write(data)
				if n2 == 0 {
					sl.stats.droppedLogs.Add(1)
				}
			}
		default:
			// Flush channel full
			sl.stats.droppedLogs.Add(1)
			// fmt.Fprintf(os.Stderr, "[LOGGER ERROR] Flush channel full, dropped log\n")
		}
	}
}

// Logf writes a formatted log message
func (sl *ShardedLogger) Logf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	sl.Log(msg)
}

// periodicFlushChecker checks all shards periodically
func (sl *ShardedLogger) periodicFlushChecker() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] periodicFlushChecker: %v\n", r)
		}
	}()

	for {
		select {
		case <-sl.done:
			return
		case <-sl.ticker.C:
			// Check all shards for pending data
			for i := 0; i < sl.shardedBuffer.NumShards(); i++ {
				shard := sl.shardedBuffer.GetShard(i)
				if shard != nil && shard.Offset() > 0 {
					select {
					case sl.flushChan <- i:
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
func (sl *ShardedLogger) flushWorker() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] flushWorker: %v\n", r)
		}
	}()

	for {
		select {
		case <-sl.done:
			// Flush any remaining shards
			sl.flushFinal()
			return
		case shardIdx := <-sl.flushChan:
			shard := sl.shardedBuffer.GetShard(shardIdx)
			if shard != nil {
				sl.flushShard(shard)
			}
		}
	}
}

// flushShard writes a shard to disk
func (sl *ShardedLogger) flushShard(shard *Shard) {
	// Use semaphore to serialize disk writes
	select {
	case sl.semaphore <- struct{}{}:
		defer func() { <-sl.semaphore }()
	default:
		// Another flush in progress, skip
		return
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] flushShard: %v\n", r)
			sl.stats.flushErrors.Add(1)
		}
	}()

	data := shard.Bytes()
	if len(data) == 0 {
		return
	}

	// Write to file
	n, err := sl.file.Write(data)
	if err != nil {
		// fmt.Fprintf(os.Stderr, "[LOGGER ERROR] Failed to write to file: %v\n", err)
		sl.stats.flushErrors.Add(1)
		return
	}

	sl.stats.bytesWritten.Add(int64(n))
	sl.stats.totalFlushes.Add(1)

	// Update shard ID and reset
	shard.SetID(sl.nextID.Add(1))
	shard.Reset()
}

// flushFinal flushes all remaining data during shutdown
func (sl *ShardedLogger) flushFinal() {
	// Flush any pending requests in the channel
	for {
		select {
		case shardIdx := <-sl.flushChan:
			shard := sl.shardedBuffer.GetShard(shardIdx)
			if shard != nil {
				sl.flushShard(shard)
			}
		default:
			goto flushAll
		}
	}

flushAll:
	// Flush all shards with data
	for _, shard := range sl.shardedBuffer.Shards() {
		if shard.Offset() > 0 {
			sl.flushShard(shard)
		}
	}
}

// Close gracefully shuts down the logger
func (sl *ShardedLogger) Close() error {
	// Check if already closed
	select {
	case <-sl.done:
		// Already closed
		return nil
	default:
		// Signal shutdown
		close(sl.done)
	}

	// Stop ticker
	sl.ticker.Stop()

	// Wait a bit for flush worker to finish
	time.Sleep(100 * time.Millisecond)

	// Sync and close file
	if sl.file != nil {
		sl.file.Sync()
		return sl.file.Close()
	}

	return nil
}

// Stats returns current logging statistics
func (sl *ShardedLogger) Stats() Statistics {
	return Statistics{
		TotalLogs:       sl.stats.totalLogs.Load(),
		TotalFlushes:    sl.stats.totalFlushes.Load(),
		DroppedLogs:     sl.stats.droppedLogs.Load(),
		BytesWritten:    sl.stats.bytesWritten.Load(),
		FlushErrors:     sl.stats.flushErrors.Load(),
		CurrentStrategy: Sharded,
	}
}
