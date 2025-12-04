package logger

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// MutexLogger implements Logger using mutex for buffer swapping
type MutexLogger struct {
	// Two buffers for double buffering
	buffer1 *LogBuffer
	buffer2 *LogBuffer

	// Active buffer pointer (protected by mutex)
	activeBuffer *LogBuffer

	// Mutex to protect buffer pointer
	mu sync.RWMutex

	// File for writing logs
	file *os.File

	// Channel for flush requests
	flushChan chan *LogBuffer

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

// newMutexLogger creates a new mutex-based logger
func newMutexLogger(config Config, file *os.File) (*MutexLogger, error) {
	ml := &MutexLogger{
		buffer1:      NewLogBuffer(config.BufferSize, 0),
		buffer2:      NewLogBuffer(config.BufferSize, 1),
		activeBuffer: nil,
		file:         file,
		flushChan:    make(chan *LogBuffer, 2),
		ticker:       time.NewTicker(config.FlushInterval),
		done:         make(chan struct{}),
		semaphore:    make(chan struct{}, 1),
		config:       config,
	}

	// Set initial active buffer
	ml.activeBuffer = ml.buffer1
	ml.nextID.Store(2)

	// Start flush worker
	go ml.flushWorker()

	// Start periodic flush checker
	go ml.periodicFlushChecker()

	return ml, nil
}

// Log writes a log message to the active buffer
func (ml *MutexLogger) Log(msg string) {
	// Add newline if not present
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		msg = msg + "\n"
	}

	data := []byte(msg)
	ml.stats.totalLogs.Add(1)

	// Get current active buffer with read lock
	ml.mu.RLock()
	buffer := ml.activeBuffer
	ml.mu.RUnlock()

	// Try to write to buffer
	n, needsFlush := buffer.Write(data)

	if n == 0 && needsFlush {
		// Buffer is full, need to swap
		ml.swapBuffers()

		// Try writing to new buffer
		ml.mu.RLock()
		buffer = ml.activeBuffer
		ml.mu.RUnlock()

		n, _ = buffer.Write(data)

		if n == 0 {
			// Still couldn't write, drop the log
			ml.stats.droppedLogs.Add(1)
			// fmt.Fprintf(os.Stderr, "[LOGGER ERROR] Dropped log: buffer full\n")
		}
	}
}

// Logf writes a formatted log message
func (ml *MutexLogger) Logf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	ml.Log(msg)
}

// swapBuffers swaps the active buffer using mutex
func (ml *MutexLogger) swapBuffers() {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	// Get current active buffer
	current := ml.activeBuffer

	// Determine the other buffer
	var next *LogBuffer
	if current == ml.buffer1 {
		next = ml.buffer2
	} else {
		next = ml.buffer1
	}

	// Swap the active buffer
	ml.activeBuffer = next

	// Send old buffer for flushing
	select {
	case ml.flushChan <- current:
		// Flush request queued successfully
	default:
		// Flush channel is full, log error
		// fmt.Fprintf(os.Stderr, "[LOGGER ERROR] Flush channel full, potential data loss\n")
	}
}

// periodicFlushChecker checks for periodic flush triggers
func (ml *MutexLogger) periodicFlushChecker() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] periodicFlushChecker: %v\n", r)
		}
	}()

	for {
		select {
		case <-ml.done:
			return
		case <-ml.ticker.C:
			// Periodic flush: swap if current buffer has data
			ml.mu.RLock()
			buffer := ml.activeBuffer
			ml.mu.RUnlock()

			if buffer.Offset() > 0 {
				ml.swapBuffers()
			}
		}
	}
}

// flushWorker handles background flushing
func (ml *MutexLogger) flushWorker() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] flushWorker: %v\n", r)
		}
	}()

	for {
		select {
		case <-ml.done:
			// Flush any remaining buffers
			ml.flushFinal()
			return
		case buffer := <-ml.flushChan:
			ml.flushBuffer(buffer)
		}
	}
}

// flushBuffer writes a buffer to disk
func (ml *MutexLogger) flushBuffer(buffer *LogBuffer) {
	// Use semaphore to prevent concurrent flushes of the same buffer
	select {
	case ml.semaphore <- struct{}{}:
		defer func() { <-ml.semaphore }()
	default:
		// Another flush is in progress, skip
		return
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] flushBuffer: %v\n", r)
			ml.stats.flushErrors.Add(1)
		}
	}()

	data := buffer.Bytes()
	if len(data) == 0 {
		return
	}

	// Write to file
	n, err := ml.file.Write(data)
	if err != nil {
		// fmt.Fprintf(os.Stderr, "[LOGGER ERROR] Failed to write to file: %v\n", err)
		ml.stats.flushErrors.Add(1)
		return
	}

	ml.stats.bytesWritten.Add(int64(n))
	ml.stats.totalFlushes.Add(1)

	// Update buffer ID and reset
	buffer.SetID(ml.nextID.Add(1))
	buffer.Reset()
}

// flushFinal flushes all remaining data during shutdown
func (ml *MutexLogger) flushFinal() {
	// Flush any pending buffers in the channel
	for {
		select {
		case buffer := <-ml.flushChan:
			ml.flushBuffer(buffer)
		default:
			goto flushActive
		}
	}

flushActive:
	// Flush the currently active buffer
	ml.mu.RLock()
	buffer := ml.activeBuffer
	ml.mu.RUnlock()

	if buffer.Offset() > 0 {
		ml.flushBuffer(buffer)
	}

	// Flush the other buffer as well
	if buffer == ml.buffer1 && ml.buffer2.Offset() > 0 {
		ml.flushBuffer(ml.buffer2)
	} else if buffer == ml.buffer2 && ml.buffer1.Offset() > 0 {
		ml.flushBuffer(ml.buffer1)
	}
}

// Close gracefully shuts down the logger
func (ml *MutexLogger) Close() error {
	// Check if already closed
	select {
	case <-ml.done:
		// Already closed
		return nil
	default:
		// Signal shutdown
		close(ml.done)
	}

	// Stop ticker
	ml.ticker.Stop()

	// Wait a bit for flush worker to finish
	time.Sleep(100 * time.Millisecond)

	// Sync and close file
	if ml.file != nil {
		ml.file.Sync()
		return ml.file.Close()
	}

	return nil
}

// Stats returns current logging statistics
func (ml *MutexLogger) Stats() Statistics {
	return Statistics{
		TotalLogs:       ml.stats.totalLogs.Load(),
		TotalFlushes:    ml.stats.totalFlushes.Load(),
		DroppedLogs:     ml.stats.droppedLogs.Load(),
		BytesWritten:    ml.stats.bytesWritten.Load(),
		FlushErrors:     ml.stats.flushErrors.Load(),
		CurrentStrategy: Mutex,
	}
}
