package logger

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

// AtomicLogger implements Logger using atomic pointer for lock-free buffer swapping
type AtomicLogger struct {
	// Two buffers for double buffering
	buffer1 *LogBuffer
	buffer2 *LogBuffer

	// Active buffer pointer (atomically swapped)
	activeBuffer atomic.Pointer[LogBuffer]

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

// newAtomicLogger creates a new atomic-based logger
func newAtomicLogger(config Config, file *os.File) (*AtomicLogger, error) {
	al := &AtomicLogger{
		buffer1:   NewLogBuffer(config.BufferSize, 0),
		buffer2:   NewLogBuffer(config.BufferSize, 1),
		file:      file,
		flushChan: make(chan *LogBuffer, 2),
		ticker:    time.NewTicker(config.FlushInterval),
		done:      make(chan struct{}),
		semaphore: make(chan struct{}, 1),
		config:    config,
	}

	// Set initial active buffer
	al.activeBuffer.Store(al.buffer1)
	al.nextID.Store(2)

	// Start flush worker
	go al.flushWorker()

	// Start periodic flush checker
	go al.periodicFlushChecker()

	return al, nil
}

// Log writes a log message to the active buffer
func (al *AtomicLogger) Log(msg string) {
	// Add newline if not present
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		msg = msg + "\n"
	}

	data := []byte(msg)
	al.stats.totalLogs.Add(1)

	// Get current active buffer
	buffer := al.activeBuffer.Load()

	// Try to write to buffer
	n, needsFlush := buffer.Write(data)

	if n == 0 && needsFlush {
		// Buffer is full, need to swap
		al.swapBuffers()

		// Try writing to new buffer
		buffer = al.activeBuffer.Load()
		n, _ = buffer.Write(data)

		if n == 0 {
			// Still couldn't write, drop the log
			al.stats.droppedLogs.Add(1)
			// fmt.Fprintf(os.Stderr, "[LOGGER ERROR] Dropped log: buffer full\n")
		}
	}
}

// Logf writes a formatted log message
func (al *AtomicLogger) Logf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	al.Log(msg)
}

// swapBuffers atomically swaps the active buffer
func (al *AtomicLogger) swapBuffers() {
	// Get current active buffer
	current := al.activeBuffer.Load()

	// Determine the other buffer
	var next *LogBuffer
	if current == al.buffer1 {
		next = al.buffer2
	} else {
		next = al.buffer1
	}

	// Try to swap using compare-and-swap
	if al.activeBuffer.CompareAndSwap(current, next) {
		// Swap successful, send old buffer for flushing
		select {
		case al.flushChan <- current:
			// Flush request queued successfully
		default:
			// Flush channel is full, log error
			// fmt.Fprintf(os.Stderr, "[LOGGER ERROR] Flush channel full, potential data loss\n")
		}
	}
	// If CAS failed, another goroutine already swapped, which is fine
}

// periodicFlushChecker checks for periodic flush triggers
func (al *AtomicLogger) periodicFlushChecker() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] periodicFlushChecker: %v\n", r)
		}
	}()

	for {
		select {
		case <-al.done:
			return
		case <-al.ticker.C:
			// Periodic flush: swap if current buffer has data
			buffer := al.activeBuffer.Load()
			if buffer.Offset() > 0 {
				al.swapBuffers()
			}
		}
	}
}

// flushWorker handles background flushing
func (al *AtomicLogger) flushWorker() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] flushWorker: %v\n", r)
		}
	}()

	for {
		select {
		case <-al.done:
			// Flush any remaining buffers
			al.flushFinal()
			return
		case buffer := <-al.flushChan:
			al.flushBuffer(buffer)
		}
	}
}

// flushBuffer writes a buffer to disk
func (al *AtomicLogger) flushBuffer(buffer *LogBuffer) {
	// Use semaphore to prevent concurrent flushes of the same buffer
	select {
	case al.semaphore <- struct{}{}:
		defer func() { <-al.semaphore }()
	default:
		// Another flush is in progress, skip
		return
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER PANIC] flushBuffer: %v\n", r)
			al.stats.flushErrors.Add(1)
		}
	}()

	data := buffer.Bytes()
	if len(data) == 0 {
		return
	}

	// Write to file
	n, err := al.file.Write(data)
	if err != nil {
		// fmt.Fprintf(os.Stderr, "[LOGGER ERROR] Failed to write to file: %v\n", err)
		al.stats.flushErrors.Add(1)
		return
	}

	al.stats.bytesWritten.Add(int64(n))
	al.stats.totalFlushes.Add(1)

	// Update buffer ID and reset
	buffer.SetID(al.nextID.Add(1))
	buffer.Reset()
}

// flushFinal flushes all remaining data during shutdown
func (al *AtomicLogger) flushFinal() {
	// Flush any pending buffers in the channel
	for {
		select {
		case buffer := <-al.flushChan:
			al.flushBuffer(buffer)
		default:
			goto flushActive
		}
	}

flushActive:
	// Flush the currently active buffer
	buffer := al.activeBuffer.Load()
	if buffer.Offset() > 0 {
		al.flushBuffer(buffer)
	}

	// Flush the other buffer as well
	if buffer == al.buffer1 && al.buffer2.Offset() > 0 {
		al.flushBuffer(al.buffer2)
	} else if buffer == al.buffer2 && al.buffer1.Offset() > 0 {
		al.flushBuffer(al.buffer1)
	}
}

// Close gracefully shuts down the logger
func (al *AtomicLogger) Close() error {
	// Check if already closed
	select {
	case <-al.done:
		// Already closed
		return nil
	default:
		// Signal shutdown
		close(al.done)
	}

	// Stop ticker
	al.ticker.Stop()

	// Wait a bit for flush worker to finish
	time.Sleep(100 * time.Millisecond)

	// Sync and close file
	if al.file != nil {
		al.file.Sync()
		return al.file.Close()
	}

	return nil
}

// Stats returns current logging statistics
func (al *AtomicLogger) Stats() Statistics {
	return Statistics{
		TotalLogs:       al.stats.totalLogs.Load(),
		TotalFlushes:    al.stats.totalFlushes.Load(),
		DroppedLogs:     al.stats.droppedLogs.Load(),
		BytesWritten:    al.stats.bytesWritten.Load(),
		FlushErrors:     al.stats.flushErrors.Load(),
		CurrentStrategy: Atomic,
	}
}
