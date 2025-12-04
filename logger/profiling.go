package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ProfilingResult holds comprehensive metrics from a profiling run
type ProfilingResult struct {
	Config          ProfilingConfig
	Duration        time.Duration
	TotalLogs       int64
	DroppedLogs     int64
	TotalFlushes    int64
	BytesWritten    int64
	FlushErrors     int64
	Throughput      float64 // logs per second
	DropRate        float64 // percentage
	MemAllocMB      float64
	MemTotalAllocMB float64
	NumGC           uint32
	CPUCores        int
}

// Generate200ByteMessage creates a realistic 200-byte log message
func Generate200ByteMessage(id int) string {
	// Timestamp (26) + Level (6) + Message (160) + ID (8) = 200 bytes
	timestamp := time.Now().Format("2006-01-02 15:04:05.000000")
	msg := fmt.Sprintf("[%s] INFO: Processing request for user_id=%08d with transaction_type=PAYMENT and amount=1234.56 USD status=SUCCESS region=US-EAST-1 datacenter=DC01 %08d", 
		timestamp, id, id)
	
	// Pad to exactly 200 bytes
	if len(msg) < 200 {
		padding := 200 - len(msg)
		for i := 0; i < padding; i++ {
			msg += "X"
		}
	} else if len(msg) > 200 {
		msg = msg[:200]
	}
	
	return msg
}

// RunProfilingTest executes a single profiling test for 2 minutes
func RunProfilingTest(config ProfilingConfig) ProfilingResult {
	tmpDir, _ := os.MkdirTemp("", "profiling-test-*")
	defer os.RemoveAll(tmpDir)
	
	logFile := filepath.Join(tmpDir, "test.log")
	
	loggerConfig := Config{
		BufferSize:    config.BufferSize,
		FlushInterval: 10 * time.Second,
		LogFilePath:   logFile,
		Strategy:      ShardedDoubleBufferCAS,
		NumShards:     config.Shards,
	}
	
	logger, err := New(loggerConfig)
	if err != nil {
		panic(fmt.Sprintf("Failed to create logger: %v", err))
	}
	
	// Capture memory stats before
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	
	// Run test for exactly 2 minutes
	testDuration := 2 * time.Minute
	startTime := time.Now()
	endTime := startTime.Add(testDuration)
	
	var wg sync.WaitGroup
	var totalOps atomic.Int64
	
	// Start worker threads
	for i := 0; i < config.Threads; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			localOps := 0
			msgID := workerID * 1000000
			
			for time.Now().Before(endTime) {
				msg := Generate200ByteMessage(msgID)
				logger.Log(msg)
				localOps++
				msgID++
				
				// Small yield to prevent tight loop
				if localOps%1000 == 0 {
					runtime.Gosched()
				}
			}
			
			totalOps.Add(int64(localOps))
		}(i)
	}
	
	// Wait for all workers to complete
	wg.Wait()
	actualDuration := time.Since(startTime)
	
	// Capture memory stats after
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	
	// Close logger and get final stats
	logger.Close()
	stats := logger.Stats()
	
	// Calculate metrics
	throughput := float64(stats.TotalLogs) / actualDuration.Seconds()
	dropRate := 0.0
	if stats.TotalLogs > 0 {
		dropRate = (float64(stats.DroppedLogs) / float64(stats.TotalLogs)) * 100.0
	}
	
	return ProfilingResult{
		Config:          config,
		Duration:        actualDuration,
		TotalLogs:       stats.TotalLogs,
		DroppedLogs:     stats.DroppedLogs,
		TotalFlushes:    stats.TotalFlushes,
		BytesWritten:    stats.BytesWritten,
		FlushErrors:     stats.FlushErrors,
		Throughput:      throughput,
		DropRate:        dropRate,
		MemAllocMB:      float64(memAfter.Alloc-memBefore.Alloc) / 1024 / 1024,
		MemTotalAllocMB: float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / 1024 / 1024,
		NumGC:           memAfter.NumGC - memBefore.NumGC,
		CPUCores:        runtime.NumCPU(),
	}
}

