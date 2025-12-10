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
		duration         = flag.Duration("duration", 10*time.Minute, "Test duration")
		bufferMB         = flag.Int("buffer-mb", 64, "Buffer size in MB")
		numShards        = flag.Int("shards", 8, "Number of shards")
		flushInterval    = flag.Duration("flush-interval", 10*time.Second, "Flush interval")
		rotationInterval = flag.Duration("rotation-interval", 24*time.Hour, "File rotation interval (0 to disable)")
		logDir           = flag.String("log-dir", "logs", "Log directory")

		// Event configurations
		event1Name    = flag.String("event1-name", "event1", "Event1 name")
		event1RPS     = flag.Int("event1-rps", 350, "Event1 RPS")
		event1Threads = flag.Int("event1-threads", 35, "Event1 threads")

		event2Name    = flag.String("event2-name", "event2", "Event2 name")
		event2RPS     = flag.Int("event2-rps", 350, "Event2 RPS")
		event2Threads = flag.Int("event2-threads", 35, "Event2 threads")

		event3Name    = flag.String("event3-name", "event3", "Event3 name")
		event3RPS     = flag.Int("event3-rps", 300, "Event3 RPS")
		event3Threads = flag.Int("event3-threads", 30, "Event3 threads")

		logSizeKB = flag.Int("log-size-kb", 300, "Log size in KB")
	)
	flag.Parse()

	// Create log directory
	if err := os.MkdirAll(*logDir, 0755); err != nil {
		log.Fatalf("Failed to create log directory: %v", err)
	}

	// Create single LoggerManager for all events
	config := asynclogger.Config{
		BufferSize:       *bufferMB * 1024 * 1024,
		NumShards:        *numShards,
		FlushInterval:    *flushInterval,
		RotationInterval: *rotationInterval,
		LogFilePath:      fmt.Sprintf("%s/%s.log", *logDir, *event1Name), // Base path, actual files will be event-specific
	}

	loggerManager, err := asynclogger.NewLoggerManager(config)
	if err != nil {
		log.Fatalf("Failed to create logger manager: %v", err)
	}
	defer loggerManager.Close()

	// Pre-initialize loggers for all events (prevents lazy init overhead)
	if *event1RPS > 0 {
		if err := loggerManager.InitializeEventLogger(*event1Name); err != nil {
			log.Fatalf("Failed to initialize event1 logger: %v", err)
		}
	}
	if *event2RPS > 0 {
		if err := loggerManager.InitializeEventLogger(*event2Name); err != nil {
			log.Fatalf("Failed to initialize event2 logger: %v", err)
		}
	}
	if *event3RPS > 0 {
		if err := loggerManager.InitializeEventLogger(*event3Name); err != nil {
			log.Fatalf("Failed to initialize event3 logger: %v", err)
		}
	}

	log.Printf("Starting multi-event logger test (single process):")
	log.Printf("  Duration: %v", *duration)
	log.Printf("  Buffer: %d MB, Shards: %d", *bufferMB, *numShards)
	log.Printf("  Rotation Interval: %v", *rotationInterval)
	log.Printf("  Events:")
	log.Printf("    %s: %d RPS, %d threads", *event1Name, *event1RPS, *event1Threads)
	log.Printf("    %s: %d RPS, %d threads", *event2Name, *event2RPS, *event2Threads)
	log.Printf("    %s: %d RPS, %d threads", *event3Name, *event3RPS, *event3Threads)
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
				printStats(loggerManager)
			case <-done:
				return
			}
		}
	}()

	// Start time
	startTime := time.Now()
	endTime := startTime.Add(*duration)

	// Start worker threads for each event
	var workerWg sync.WaitGroup

	// Event1 workers
	if *event1RPS > 0 && *event1Threads > 0 {
		ratePerThread := float64(*event1RPS) / float64(*event1Threads)
		intervalPerThread := time.Duration(float64(time.Second) / ratePerThread)
		for i := 0; i < *event1Threads; i++ {
			workerWg.Add(1)
			go func(threadID int) {
				defer workerWg.Done()
				worker(loggerManager, *event1Name, logData, intervalPerThread, endTime, threadID)
			}(i)
		}
	}

	// Event2 workers
	if *event2RPS > 0 && *event2Threads > 0 {
		ratePerThread := float64(*event2RPS) / float64(*event2Threads)
		intervalPerThread := time.Duration(float64(time.Second) / ratePerThread)
		for i := 0; i < *event2Threads; i++ {
			workerWg.Add(1)
			go func(threadID int) {
				defer workerWg.Done()
				worker(loggerManager, *event2Name, logData, intervalPerThread, endTime, threadID)
			}(i)
		}
	}

	// Event3 workers
	if *event3RPS > 0 && *event3Threads > 0 {
		ratePerThread := float64(*event3RPS) / float64(*event3Threads)
		intervalPerThread := time.Duration(float64(time.Second) / ratePerThread)
		for i := 0; i < *event3Threads; i++ {
			workerWg.Add(1)
			go func(threadID int) {
				defer workerWg.Done()
				worker(loggerManager, *event3Name, logData, intervalPerThread, endTime, threadID)
			}(i)
		}
	}

	// Wait for all workers
	workerWg.Wait()

	// Stop statistics reporting
	close(done)
	wg.Wait()

	// Final statistics
	log.Println()
	log.Println("=== Final Statistics ===")
	printStats(loggerManager)

	elapsed := time.Since(startTime)
	log.Printf("Test completed in %v", elapsed)
}

func worker(
	loggerManager *asynclogger.LoggerManager,
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
			loggerManager.LogBytesWithEvent(eventName, logData)
			atomic.AddInt64(&bytesWritten, int64(len(logData)))

		case <-time.After(time.Until(endTime)):
			return
		}
	}
}

func printStats(loggerManager *asynclogger.LoggerManager) {
	// Get aggregated stats
	totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, setSwaps := loggerManager.GetStatsSnapshot()
	flushMetrics := loggerManager.GetAggregatedFlushMetrics()

	avgFlushMs := float64(flushMetrics.AvgFlushDuration.Nanoseconds()) / 1e6
	maxFlushMs := float64(flushMetrics.MaxFlushDuration.Nanoseconds()) / 1e6
	avgWriteMs := float64(flushMetrics.AvgWriteDuration.Nanoseconds()) / 1e6
	maxWriteMs := float64(flushMetrics.MaxWriteDuration.Nanoseconds()) / 1e6
	writePercent := flushMetrics.WritePercent
	avgPwritevMs := float64(flushMetrics.AvgPwritevDuration.Nanoseconds()) / 1e6
	maxPwritevMs := float64(flushMetrics.MaxPwritevDuration.Nanoseconds()) / 1e6
	pwritevPercent := flushMetrics.PwritevPercent

	dropRate := 0.0
	if totalLogs > 0 {
		dropRate = float64(droppedLogs) / float64(totalLogs) * 100.0
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	log.Printf("METRICS: Logs: %d Dropped: %d (%.4f%%) | Bytes: %d | Flushes: %d Errors: %d Swaps: %d | AvgFlush: %.2fms MaxFlush: %.2fms | AvgWrite: %.2fms MaxWrite: %.2fms WritePct: %.1f%% | AvgPwritev: %.2fms MaxPwritev: %.2fms PwritevPct: %.1f%% | GC: %d cycles %.2fms pause | Mem: %.2fMB",
		totalLogs, droppedLogs, dropRate, bytesWritten, flushes, flushErrors, setSwaps,
		avgFlushMs, maxFlushMs,
		avgWriteMs, maxWriteMs, writePercent,
		avgPwritevMs, maxPwritevMs, pwritevPercent,
		memStats.NumGC, float64(memStats.PauseTotalNs)/1e6,
		float64(memStats.Alloc)/1024/1024)
}
