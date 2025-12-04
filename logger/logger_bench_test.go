package logger

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// BenchmarkLogBuffer benchmarks buffer write operations
func BenchmarkLogBuffer(b *testing.B) {
	buffer := NewLogBuffer(1024*1024, 1)
	data := []byte("test log message\n")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buffer.Write(data)
		if buffer.IsFull() {
			buffer.Reset()
		}
	}
}

// BenchmarkAtomicLogger benchmarks atomic logger with various concurrency levels
func BenchmarkAtomicLogger(b *testing.B) {
	concurrencyLevels := []int{1, 10, 100, 1000}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Concurrency-%d", concurrency), func(b *testing.B) {
			tmpDir := b.TempDir()
			logFile := filepath.Join(tmpDir, "bench.log")

			config := Config{
				BufferSize:    1024 * 1024, // 1MB
				FlushInterval: 10 * time.Second,
				LogFilePath:   logFile,
				Strategy:      Atomic,
			}

			logger, err := New(config)
			if err != nil {
				b.Fatalf("Failed to create logger: %v", err)
			}
			defer logger.Close()

			msg := "This is a benchmark log message with some content"

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					logger.Log(msg)
				}
			})
			b.StopTimer()

			logger.Close()

			stats := logger.Stats()
			b.ReportMetric(float64(stats.TotalLogs), "logs")
			b.ReportMetric(float64(stats.TotalFlushes), "flushes")
			b.ReportMetric(float64(stats.DroppedLogs), "dropped")
		})
	}
}

// BenchmarkMutexLogger benchmarks mutex logger with various concurrency levels
func BenchmarkMutexLogger(b *testing.B) {
	concurrencyLevels := []int{1, 10, 100, 1000}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Concurrency-%d", concurrency), func(b *testing.B) {
			tmpDir := b.TempDir()
			logFile := filepath.Join(tmpDir, "bench.log")

			config := Config{
				BufferSize:    1024 * 1024, // 1MB
				FlushInterval: 10 * time.Second,
				LogFilePath:   logFile,
				Strategy:      Mutex,
			}

			logger, err := New(config)
			if err != nil {
				b.Fatalf("Failed to create logger: %v", err)
			}
			defer logger.Close()

			msg := "This is a benchmark log message with some content"

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					logger.Log(msg)
				}
			})
			b.StopTimer()

			logger.Close()

			stats := logger.Stats()
			b.ReportMetric(float64(stats.TotalLogs), "logs")
			b.ReportMetric(float64(stats.TotalFlushes), "flushes")
			b.ReportMetric(float64(stats.DroppedLogs), "dropped")
		})
	}
}

// BenchmarkAtomicVsMutex directly compares both strategies
func BenchmarkAtomicVsMutex(b *testing.B) {
	strategies := []Strategy{Atomic, Mutex}

	for _, strategy := range strategies {
		b.Run(strategy.String(), func(b *testing.B) {
			tmpDir := b.TempDir()
			logFile := filepath.Join(tmpDir, "bench.log")

			config := Config{
				BufferSize:    1024 * 1024,
				FlushInterval: 10 * time.Second,
				LogFilePath:   logFile,
				Strategy:      strategy,
			}

			logger, err := New(config)
			if err != nil {
				b.Fatalf("Failed to create logger: %v", err)
			}
			defer logger.Close()

			msg := "Benchmark log message"

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				logger.Log(msg)
			}
			b.StopTimer()

			logger.Close()
		})
	}
}

// BenchmarkLoggerWithFormatting benchmarks formatted logging
func BenchmarkLoggerWithFormatting(b *testing.B) {
	tmpDir := b.TempDir()
	logFile := filepath.Join(tmpDir, "bench.log")

	config := Config{
		BufferSize:    1024 * 1024,
		FlushInterval: 10 * time.Second,
		LogFilePath:   logFile,
		Strategy:      Atomic,
	}

	logger, err := New(config)
	if err != nil {
		b.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Logf("Log message number %d with value %s", i, "test")
	}
	b.StopTimer()

	logger.Close()
}

// BenchmarkHighConcurrencyAtomic benchmarks atomic logger under extreme concurrency
func BenchmarkHighConcurrencyAtomic(b *testing.B) {
	tmpDir := b.TempDir()
	logFile := filepath.Join(tmpDir, "bench.log")

	config := Config{
		BufferSize:    2 * 1024 * 1024, // 2MB
		FlushInterval: 10 * time.Second,
		LogFilePath:   logFile,
		Strategy:      Atomic,
	}

	logger, err := New(config)
	if err != nil {
		b.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	numGoroutines := 1000
	msg := "High concurrency test log message"

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	b.ResetTimer()
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < b.N/numGoroutines; j++ {
				logger.Log(msg)
			}
		}()
	}
	wg.Wait()
	b.StopTimer()

	logger.Close()

	stats := logger.Stats()
	b.ReportMetric(float64(stats.TotalLogs), "logs")
	b.ReportMetric(float64(stats.DroppedLogs), "dropped")
}

// BenchmarkHighConcurrencyMutex benchmarks mutex logger under extreme concurrency
func BenchmarkHighConcurrencyMutex(b *testing.B) {
	tmpDir := b.TempDir()
	logFile := filepath.Join(tmpDir, "bench.log")

	config := Config{
		BufferSize:    2 * 1024 * 1024, // 2MB
		FlushInterval: 10 * time.Second,
		LogFilePath:   logFile,
		Strategy:      Mutex,
	}

	logger, err := New(config)
	if err != nil {
		b.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()

	numGoroutines := 1000
	msg := "High concurrency test log message"

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	b.ResetTimer()
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < b.N/numGoroutines; j++ {
				logger.Log(msg)
			}
		}()
	}
	wg.Wait()
	b.StopTimer()

	logger.Close()

	stats := logger.Stats()
	b.ReportMetric(float64(stats.TotalLogs), "logs")
	b.ReportMetric(float64(stats.DroppedLogs), "dropped")
}

// BenchmarkBufferSizes benchmarks different buffer sizes
func BenchmarkBufferSizes(b *testing.B) {
	sizes := []int{
		64 * 1024,       // 64KB
		256 * 1024,      // 256KB
		1024 * 1024,     // 1MB
		4 * 1024 * 1024, // 4MB
	}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size-%dKB", size/1024), func(b *testing.B) {
			tmpDir := b.TempDir()
			logFile := filepath.Join(tmpDir, "bench.log")

			config := Config{
				BufferSize:    size,
				FlushInterval: 10 * time.Second,
				LogFilePath:   logFile,
				Strategy:      Atomic,
			}

			logger, err := New(config)
			if err != nil {
				b.Fatalf("Failed to create logger: %v", err)
			}
			defer logger.Close()

			msg := "Buffer size benchmark message"

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					logger.Log(msg)
				}
			})
			b.StopTimer()

			logger.Close()
		})
	}
}

// BenchmarkMessageSizes benchmarks different message sizes
func BenchmarkMessageSizes(b *testing.B) {
	messageSizes := []int{16, 64, 256, 1024}

	for _, msgSize := range messageSizes {
		b.Run(fmt.Sprintf("MsgSize-%d", msgSize), func(b *testing.B) {
			tmpDir := b.TempDir()
			logFile := filepath.Join(tmpDir, "bench.log")

			config := Config{
				BufferSize:    1024 * 1024,
				FlushInterval: 10 * time.Second,
				LogFilePath:   logFile,
				Strategy:      Atomic,
			}

			logger, err := New(config)
			if err != nil {
				b.Fatalf("Failed to create logger: %v", err)
			}
			defer logger.Close()

			// Create message of specified size
			msg := string(make([]byte, msgSize))

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				logger.Log(msg)
			}
			b.StopTimer()

			logger.Close()
		})
	}
}

