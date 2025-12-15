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
		numThreads          = flag.Int("threads", 100, "Number of concurrent threads")
		logSizeKB           = flag.Int("log-size-kb", 300, "Log size in KB")
		targetRPS           = flag.Int("rps", 1000, "Target requests per second (total across all threads)")
		duration            = flag.Duration("duration", 10*time.Minute, "Test duration")
		bufferMB            = flag.Int("buffer-mb", 64, "Buffer size in MB")
		numShards           = flag.Int("shards", 8, "Number of shards")
		flushInterval       = flag.Duration("flush-interval", 10*time.Second, "Flush interval")
		maxFileSizeGB       = flag.Int("max-file-size-gb", 1, "Maximum file size in GB before rotation (0 to disable)")
		preallocateFileSizeGB = flag.Int("preallocate-size-gb", 0, "Preallocate file size in GB (0 to use max-file-size-gb)")
		logDir              = flag.String("log-dir", "logs", "Log directory")
	)
	flag.Parse()

	// Create log directory
	if err := os.MkdirAll(*logDir, 0755); err != nil {
		log.Fatalf("Failed to create log directory: %v", err)
	}

	// Calculate file sizes
	maxFileSize := int64(*maxFileSizeGB) * 1024 * 1024 * 1024
	preallocateSize := maxFileSize
	if *preallocateFileSizeGB > 0 {
		preallocateSize = int64(*preallocateFileSizeGB) * 1024 * 1024 * 1024
	}

	// Initialize size-based logger
	config := asynclogger.SizeConfig{
		BufferSize:          *bufferMB * 1024 * 1024,
		NumShards:           *numShards,
		FlushInterval:       *flushInterval,
		MaxFileSize:         maxFileSize,
		PreallocateFileSize: preallocateSize,
		LogFilePath:         fmt.Sprintf("%s/size_test.log", *logDir),
	}

	logger, err := asynclogger.NewSizeLogger(config)
	if err != nil {
		log.Fatalf("Failed to create size logger: %v", err)
	}
	defer logger.Close()

	// Calculate rate per thread
	ratePerThread := float64(*targetRPS) / float64(*numThreads)
	intervalPerThread := time.Duration(float64(time.Second) / ratePerThread)

	log.Printf("Starting size-based logger test:")
	log.Printf("  Threads: %d", *numThreads)
	log.Printf("  Log size: %d KB", *logSizeKB)
	log.Printf("  Target RPS: %d (%.2f per thread)", *targetRPS, ratePerThread)
	log.Printf("  Duration: %v", *duration)
	log.Printf("  Buffer: %d MB, Shards: %d", *bufferMB, *numShards)
	log.Printf("  Max File Size: %d GB", *maxFileSizeGB)
	log.Printf("  Preallocate Size: %d GB", func() int {
		if *preallocateFileSizeGB > 0 {
			return *preallocateFileSizeGB
		}
		return *maxFileSizeGB
	}())
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
				printStats(logger)
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
			worker(logger, logData, intervalPerThread, endTime, threadID)
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
	printStats(logger)

	elapsed := time.Since(startTime)
	log.Printf("Test completed in %v", elapsed)
}

func worker(
	logger *asynclogger.SizeLogger,
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
			logger.LogBytes(logData)
			atomic.AddInt64(&bytesWritten, int64(len(logData)))

		case <-time.After(time.Until(endTime)):
			return
		}
	}
}

func printStats(logger *asynclogger.SizeLogger) {
	totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, setSwaps := logger.GetStatsSnapshot()
	flushMetrics := logger.GetFlushMetrics()

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

