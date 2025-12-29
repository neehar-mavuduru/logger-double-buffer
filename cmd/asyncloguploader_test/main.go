package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/neeharmavuduru/logger-double-buffer/asyncloguploader"
)

var (
	totalLogs    int64
	droppedLogs  int64
	bytesWritten int64
)

func main() {
	// Configuration
	var (
		numThreads            = flag.Int("threads", 100, "Number of concurrent threads")
		logSizeKB             = flag.Int("log-size-kb", 300, "Log size in KB")
		targetRPS             = flag.Int("rps", 1000, "Target requests per second (total across all threads)")
		duration              = flag.Duration("duration", 10*time.Minute, "Test duration")
		bufferMB              = flag.Int("buffer-mb", 64, "Buffer size in MB")
		numShards             = flag.Int("shards", 8, "Number of shards")
		flushInterval         = flag.Duration("flush-interval", 10*time.Second, "Flush interval")
		flushTimeout          = flag.Duration("flush-timeout", 10*time.Millisecond, "Flush timeout (write completion wait)")
		maxFileSizeGB         = flag.Int("max-file-size-gb", 0, "Maximum file size in GB before rotation (0 to disable)")
		preallocateFileSizeGB = flag.Int("preallocate-size-gb", 0, "Preallocate file size in GB (0 to use max-file-size-gb)")
		logDir                = flag.String("log-dir", "logs", "Log directory")
		useEvents             = flag.Bool("use-events", false, "Use LoggerManager with event-based logging")
		numEvents             = flag.Int("num-events", 3, "Number of events (for event-based logging)")
		gcsBucket             = flag.String("gcs-bucket", "", "GCS bucket name for uploads (empty to disable)")
		gcsPrefix             = flag.String("gcs-prefix", "", "GCS object prefix (e.g., 'logs/event1/')")
		gcsChunkSizeMB        = flag.Int("gcs-chunk-mb", 32, "GCS upload chunk size in MB")
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

	// Initialize GCS uploader if enabled
	var uploader *asyncloguploader.Uploader
	var uploadChan chan<- string
	if *gcsBucket != "" {
		uploaderConfig := asyncloguploader.DefaultGCSUploadConfig(*gcsBucket)
		uploaderConfig.ObjectPrefix = *gcsPrefix
		uploaderConfig.ChunkSize = *gcsChunkSizeMB * 1024 * 1024

		var err error
		uploader, err = asyncloguploader.NewUploader(uploaderConfig)
		if err != nil {
			log.Fatalf("Failed to create GCS uploader: %v", err)
		}
		uploadChan = uploader.GetUploadChannel()
		uploader.Start()
		defer uploader.Stop()
		log.Printf("GCS uploader enabled: bucket=%s, prefix=%s, chunk=%dMB", *gcsBucket, *gcsPrefix, *gcsChunkSizeMB)
	}

	// Initialize logger
	var loggerManager *asyncloguploader.LoggerManager
	var logger *asyncloguploader.Logger
	var err error

	if *useEvents {
		// Use LoggerManager for event-based logging
		config := asyncloguploader.DefaultConfig(fmt.Sprintf("%s/event1.log", *logDir))
		config.BufferSize = *bufferMB * 1024 * 1024
		config.NumShards = *numShards
		config.FlushInterval = *flushInterval
		config.FlushTimeout = *flushTimeout
		config.MaxFileSize = maxFileSize
		config.PreallocateFileSize = preallocateSize
		config.UploadChannel = uploadChan

		loggerManager, err = asyncloguploader.NewLoggerManager(config)
		if err != nil {
			log.Fatalf("Failed to create logger manager: %v", err)
		}
		log.Printf("LoggerManager initialized successfully")
		defer func() {
			log.Printf("Closing logger manager...")
			if err := loggerManager.Close(); err != nil {
				log.Printf("Error closing logger manager: %v", err)
			} else {
				log.Printf("Logger manager closed successfully")
			}
		}()
	} else {
		// Use single Logger
		config := asyncloguploader.DefaultConfig(fmt.Sprintf("%s/test.log", *logDir))
		config.BufferSize = *bufferMB * 1024 * 1024
		config.NumShards = *numShards
		config.FlushInterval = *flushInterval
		config.FlushTimeout = *flushTimeout
		config.MaxFileSize = maxFileSize
		config.PreallocateFileSize = preallocateSize
		config.UploadChannel = uploadChan

		logger, err = asyncloguploader.NewLogger(config)
		if err != nil {
			log.Fatalf("Failed to create logger: %v", err)
		}
		log.Printf("Logger initialized successfully")
		defer func() {
			log.Printf("Closing logger...")
			if err := logger.Close(); err != nil {
				log.Printf("Error closing logger: %v", err)
			} else {
				log.Printf("Logger closed successfully")
			}
		}()
	}

	// Calculate rate per thread
	ratePerThread := float64(*targetRPS) / float64(*numThreads)
	intervalPerThread := time.Duration(float64(time.Second) / ratePerThread)

	log.Printf("Starting asyncloguploader test:")
	log.Printf("  Threads: %d", *numThreads)
	log.Printf("  Log size: %d KB", *logSizeKB)
	log.Printf("  Target RPS: %d (%.2f per thread)", *targetRPS, ratePerThread)
	log.Printf("  Duration: %v", *duration)
	log.Printf("  Buffer: %d MB, Shards: %d", *bufferMB, *numShards)
	log.Printf("  Flush Interval: %v", *flushInterval)
	log.Printf("  Flush Timeout: %v", *flushTimeout)
	log.Printf("  Max File Size: %d GB", *maxFileSizeGB)
	log.Printf("  Preallocate Size: %d GB", func() int {
		if *preallocateFileSizeGB > 0 {
			return *preallocateFileSizeGB
		}
		return *maxFileSizeGB
	}())
	log.Printf("  Event-based: %v", *useEvents)
	if *useEvents {
		log.Printf("  Number of events: %d", *numEvents)
	}
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
				var totalLogs, droppedLogs, bytesWritten, flushes, flushErrors int64
				var flushMetrics asyncloguploader.FlushMetrics

				if *useEvents {
					totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, _ = loggerManager.GetAggregatedStats()
					flushMetrics = loggerManager.GetAggregatedFlushMetrics()
				} else {
					totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, _ = logger.GetStatsSnapshot()
					flushMetrics = logger.GetFlushMetrics()
				}

				var m runtime.MemStats
				runtime.ReadMemStats(&m)

				dropRate := 0.0
				if totalLogs > 0 {
					dropRate = float64(droppedLogs) / float64(totalLogs) * 100.0
				}

				writePct := 0.0
				if flushMetrics.AvgFlushDuration > 0 {
					writePct = flushMetrics.WritePercent
				}

				pwritevPct := 0.0
				if flushMetrics.AvgFlushDuration > 0 {
					pwritevPct = flushMetrics.PwritevPercent
				}

				log.Printf("METRICS: Logs: %d Dropped: %d (%.4f%%) | Bytes: %d | Flushes: %d Errors: %d | "+
					"AvgFlush: %.2fms MaxFlush: %.2fms | AvgWrite: %.2fms MaxWrite: %.2fms WritePct: %.1f%% | "+
					"AvgPwritev: %.2fms MaxPwritev: %.2fms PwritevPct: %.1f%% | GC: %d cycles %.2fms pause | Mem: %.2fMB",
					totalLogs, droppedLogs, dropRate, bytesWritten, flushes, flushErrors,
					float64(flushMetrics.AvgFlushDuration)/1e6, float64(flushMetrics.MaxFlushDuration)/1e6,
					float64(flushMetrics.AvgWriteDuration)/1e6, float64(flushMetrics.MaxWriteDuration)/1e6, writePct,
					float64(flushMetrics.AvgPwritevDuration)/1e6, float64(flushMetrics.MaxPwritevDuration)/1e6, pwritevPct,
					m.NumGC, float64(m.PauseTotalNs)/1e6, float64(m.Alloc)/1024/1024)

			case <-done:
				return
			}
		}
	}()

	// Start load generation
	startTime := time.Now()
	endTime := startTime.Add(*duration)
	log.Printf("Test start time: %v, end time: %v", startTime, endTime)

	eventNames := make([]string, *numEvents)
	for i := 0; i < *numEvents; i++ {
		eventNames[i] = fmt.Sprintf("event%d", i+1)
	}

	log.Printf("Starting %d worker threads...", *numThreads)
	for i := 0; i < *numThreads; i++ {
		wg.Add(1)
		go func(threadID int) {
			defer wg.Done()

			// Each thread maintains its own rate limiter
			nextWrite := time.Now()
			writeCount := int64(0)

			for {
				now := time.Now()
				if !now.Before(endTime) {
					break // Test duration expired
				}

				if now.Before(nextWrite) {
					sleepDuration := nextWrite.Sub(now)
					if sleepDuration > 0 {
						time.Sleep(sleepDuration)
					}
				}

				// Generate unique data for each write
				data := make([]byte, logSizeBytes)
				copy(data, logData)
				// Add thread ID and timestamp to make each log unique
				data[0] = byte(threadID)
				binary.LittleEndian.PutUint64(data[1:9], uint64(time.Now().UnixNano()))

				if *useEvents {
					// Round-robin through events
					eventName := eventNames[threadID%*numEvents]
					loggerManager.LogBytesWithEvent(eventName, data)
				} else {
					logger.LogBytes(data)
				}

				atomic.AddInt64(&totalLogs, 1)
				writeCount++

				// Debug: Log first few writes from thread 0
				if threadID == 0 && writeCount <= 3 {
					log.Printf("[DEBUG] Thread 0: Write #%d completed", writeCount)
				}

				// Calculate next write time
				nextWrite = nextWrite.Add(intervalPerThread)
			}

			if threadID == 0 {
				log.Printf("Thread %d completed: %d writes", threadID, writeCount)
			}
		}(i)
	}

	log.Printf("All worker threads started")

	// Wait for all threads to complete
	log.Printf("Waiting for all worker threads to complete...")
	wg.Wait()
	log.Printf("All worker threads completed")
	close(done)

	// Final statistics
	var finalTotalLogs, finalDroppedLogs, finalBytesWritten, finalFlushes, finalFlushErrors int64
	if *useEvents {
		finalTotalLogs, finalDroppedLogs, finalBytesWritten, finalFlushes, finalFlushErrors, _ = loggerManager.GetAggregatedStats()
	} else {
		finalTotalLogs, finalDroppedLogs, finalBytesWritten, finalFlushes, finalFlushErrors, _ = logger.GetStatsSnapshot()
	}

	log.Println()
	log.Printf("Final Statistics:")
	log.Printf("  Total Logs: %d", finalTotalLogs)
	log.Printf("  Dropped Logs: %d", finalDroppedLogs)
	log.Printf("  Bytes Written: %d", finalBytesWritten)
	log.Printf("  Flushes: %d", finalFlushes)
	log.Printf("  Flush Errors: %d", finalFlushErrors)

	if uploader != nil {
		// Give uploader a moment to process any pending files
		log.Printf("Waiting for uploader to finish processing...")
		time.Sleep(2 * time.Second)

		uploadStats := uploader.GetStats()
		log.Printf("  GCS Uploads: %d", uploadStats.Successful)
		log.Printf("  GCS Upload Errors: %d", uploadStats.Failed)
		log.Printf("  GCS Upload Bytes: %d", uploadStats.TotalBytes)
		if uploadStats.TotalFiles > 0 {
			log.Printf("  GCS Total Files Processed: %d", uploadStats.TotalFiles)
			log.Printf("  GCS Last Upload Time: %v", uploadStats.LastUploadTime)
		}
	}
}
