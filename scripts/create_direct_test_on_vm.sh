#!/bin/bash

# Script to create direct logger test on VM
# Run this on your VM to create the test program

set -e

echo "Creating direct logger test on VM..."

# Create directory
mkdir -p cmd/direct_logger_test

# Create main.go
cat > cmd/direct_logger_test/main.go << 'MAINGO'
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/neeharmavuduru/logger-double-buffer/asynclogger"
)

var (
	totalLogs    int64
	droppedLogs  int64
	bytesWritten int64
)

func main() {
	// Configuration
	var (
		numThreads     = flag.Int("threads", 100, "Number of concurrent threads")
		logSizeKB      = flag.Int("log-size-kb", 300, "Log size in KB")
		targetRPS      = flag.Int("rps", 1000, "Target requests per second (total across all threads)")
		duration       = flag.Duration("duration", 10*time.Minute, "Test duration")
		bufferMB       = flag.Int("buffer-mb", 64, "Buffer size in MB")
		numShards      = flag.Int("shards", 8, "Number of shards")
		flushInterval  = flag.Duration("flush-interval", 10*time.Second, "Flush interval")
		logDir         = flag.String("log-dir", "logs", "Log directory")
		eventName      = flag.String("event", "test", "Event name for event-based logging")
		useEventLogger = flag.Bool("use-events", false, "Use LoggerManager with event-based logging")
	)
	flag.Parse()

	// Create log directory
	if err := os.MkdirAll(*logDir, 0755); err != nil {
		log.Fatalf("Failed to create log directory: %v", err)
	}

	// Initialize logger
	var loggerManager *asynclogger.LoggerManager
	var logger *asynclogger.Logger
	var err error

	if *useEventLogger {
		// Use LoggerManager for event-based logging
		config := asynclogger.Config{
			BufferSize:    *bufferMB * 1024 * 1024,
			NumShards:     *numShards,
			FlushInterval: *flushInterval,
			LogFilePath:   fmt.Sprintf("%s/%s.log", *logDir, *eventName),
		}
		loggerManager, err = asynclogger.NewLoggerManager(config)
		if err != nil {
			log.Fatalf("Failed to create logger manager: %v", err)
		}
		defer loggerManager.Close()
	} else {
		// Use single Logger
		config := asynclogger.Config{
			BufferSize:    *bufferMB * 1024 * 1024,
			NumShards:     *numShards,
			FlushInterval: *flushInterval,
			LogFilePath:   fmt.Sprintf("%s/direct_test.log", *logDir),
		}
		logger, err = asynclogger.New(config)
		if err != nil {
			log.Fatalf("Failed to create logger: %v", err)
		}
		defer logger.Close()
	}

	// Calculate rate per thread
	ratePerThread := float64(*targetRPS) / float64(*numThreads)
	intervalPerThread := time.Duration(float64(time.Second) / ratePerThread)

	log.Printf("Starting direct logger test:")
	log.Printf("  Threads: %d", *numThreads)
	log.Printf("  Log size: %d KB", *logSizeKB)
	log.Printf("  Target RPS: %d (%.2f per thread)", *targetRPS, ratePerThread)
	log.Printf("  Duration: %v", *duration)
	log.Printf("  Buffer: %d MB, Shards: %d", *bufferMB, *numShards)
	log.Printf("  Event-based: %v", *useEventLogger)
	log.Println()

	// Prepare log data template
	logSizeBytes := *logSizeKB * 1024
	logData := make([]byte, logSizeBytes)
	for i := range logData {
		logData[i] = byte(rand.Intn(256))
	}

	// Start statistics reporting
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				printStats(loggerManager, logger, *useEventLogger)
			case <-done:
				return
			}
		}
	}()

	// Start time
	startTime := time.Now()
	endTime := startTime.Add(*duration)

	// Start worker threads
	var workerWg sync.WaitGroup
	for i := 0; i < *numThreads; i++ {
		workerWg.Add(1)
		go func(threadID int) {
			defer workerWg.Done()
			worker(loggerManager, logger, *useEventLogger, *eventName, logData, intervalPerThread, endTime, threadID)
		}(i)
	}

	// Wait for all workers
	workerWg.Wait()

	// Stop statistics reporting
	close(done)
	wg.Wait()

	// Final statistics
	log.Println()
	log.Println("=== Final Statistics ===")
	printStats(loggerManager, logger, *useEventLogger)

	elapsed := time.Since(startTime)
	log.Printf("Test completed in %v", elapsed)
}

func worker(
	loggerManager *asynclogger.LoggerManager,
	logger *asynclogger.Logger,
	useEventLogger bool,
	eventName string,
	logData []byte,
	interval time.Duration,
	endTime time.Time,
	threadID int,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if time.Now().After(endTime) {
				return
			}

			// Write log
			atomic.AddInt64(&totalLogs, 1)

			if useEventLogger && loggerManager != nil {
				loggerManager.LogBytesWithEvent(eventName, logData)
			} else if logger != nil {
				logger.LogBytes(logData)
			}

			atomic.AddInt64(&bytesWritten, int64(len(logData)))

		case <-time.After(time.Until(endTime)):
			return
		}
	}
}

func printStats(loggerManager *asynclogger.LoggerManager, logger *asynclogger.Logger, useEventLogger bool) {
	var totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, setSwaps int64
	var avgFlushMs, maxFlushMs float64

	if useEventLogger && loggerManager != nil {
		totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, setSwaps = loggerManager.GetStatsSnapshot()
		flushMetrics := loggerManager.GetAggregatedFlushMetrics()
		avgFlushMs = float64(flushMetrics.AvgFlushDuration.Nanoseconds()) / 1e6
		maxFlushMs = float64(flushMetrics.MaxFlushDuration.Nanoseconds()) / 1e6
	} else if logger != nil {
		totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, setSwaps = logger.GetStatsSnapshot()
		flushMetrics := logger.GetFlushMetrics()
		avgFlushMs = float64(flushMetrics.AvgFlushDuration.Nanoseconds()) / 1e6
		maxFlushMs = float64(flushMetrics.MaxFlushDuration.Nanoseconds()) / 1e6
	}

	dropRate := 0.0
	if totalLogs > 0 {
		dropRate = float64(droppedLogs) / float64(totalLogs) * 100.0
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	log.Printf("METRICS: Logs: %d Dropped: %d (%.4f%%) | Bytes: %d | Flushes: %d Errors: %d Swaps: %d | AvgFlush: %.2fms MaxFlush: %.2fms | GC: %d cycles %.2fms pause | Mem: %.2fMB",
		totalLogs, droppedLogs, dropRate, bytesWritten, flushes, flushErrors, setSwaps,
		avgFlushMs, maxFlushMs,
		memStats.NumGC, float64(memStats.PauseTotalNs)/1e6,
		float64(memStats.Alloc)/1024/1024)
}
MAINGO

echo "✓ Created cmd/direct_logger_test/main.go"
echo ""
echo "Now build it:"
echo "  go build -o bin/direct_logger_test ./cmd/direct_logger_test"









