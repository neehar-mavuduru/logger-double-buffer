package logger

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// BenchmarkOptimizedDoubleBufferStrategies benchmarks the two double buffer strategies
// with optimized configuration: 8MB buffer, 32 shards
func BenchmarkOptimizedDoubleBufferStrategies(b *testing.B) {
	strategies := []Strategy{ShardedDoubleBuffer, ShardedDoubleBufferCAS}
	concurrencyLevels := []int{1, 10, 100, 1000}

	// Optimized configuration
	bufferSize := 8 * 1024 * 1024   // 8MB
	numShards := 32                  // 32 shards
	flushInterval := 10 * time.Second // Long interval - buffer fills first

	for _, strategy := range strategies {
		for _, concurrency := range concurrencyLevels {
			b.Run(fmt.Sprintf("%s/Workers-%d", strategy.String(), concurrency), func(b *testing.B) {
				tmpDir := b.TempDir()
				logFile := filepath.Join(tmpDir, "bench.log")

				config := Config{
					BufferSize:    bufferSize,
					FlushInterval: flushInterval,
					LogFilePath:   logFile,
					Strategy:      strategy,
					NumShards:     numShards,
				}

				logger, err := New(config)
				if err != nil {
					b.Fatalf("Failed to create logger: %v", err)
				}
				defer logger.Close()

				msg := "Optimized benchmark test message with moderate length for realistic testing"

				// Track memory stats before
				var memStatsBefore runtime.MemStats
				runtime.ReadMemStats(&memStatsBefore)

				b.ResetTimer()
				b.SetParallelism(concurrency)
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						logger.Log(msg)
					}
				})
				b.StopTimer()

				// Track memory stats after
				var memStatsAfter runtime.MemStats
				runtime.ReadMemStats(&memStatsAfter)

				logger.Close()

				stats := logger.Stats()
				dropRate := float64(stats.DroppedLogs) / float64(stats.TotalLogs) * 100

				// Report custom metrics
				b.ReportMetric(float64(stats.BytesWritten), "bytes")
				b.ReportMetric(float64(stats.DroppedLogs), "dropped")
				b.ReportMetric(dropRate, "drop_rate_%")
				b.ReportMetric(float64(stats.TotalFlushes), "flushes")
				b.ReportMetric(float64(stats.TotalLogs), "total_logs")
				b.ReportMetric(float64(memStatsAfter.Alloc-memStatsBefore.Alloc)/1024/1024, "alloc_MB")
				b.ReportMetric(float64(memStatsAfter.TotalAlloc-memStatsBefore.TotalAlloc)/1024/1024, "total_alloc_MB")
				b.ReportMetric(float64(memStatsAfter.NumGC-memStatsBefore.NumGC), "num_gc")
			})
		}
	}
}

// BenchmarkOptimizedThroughput measures raw throughput with optimized config
func BenchmarkOptimizedThroughput(b *testing.B) {
	strategies := []Strategy{ShardedDoubleBuffer, ShardedDoubleBufferCAS}

	bufferSize := 8 * 1024 * 1024
	numShards := 32
	flushInterval := 10 * time.Second

	for _, strategy := range strategies {
		b.Run(strategy.String(), func(b *testing.B) {
			tmpDir := b.TempDir()
			logFile := filepath.Join(tmpDir, "bench.log")

			config := Config{
				BufferSize:    bufferSize,
				FlushInterval: flushInterval,
				LogFilePath:   logFile,
				Strategy:      strategy,
				NumShards:     numShards,
			}

			logger, err := New(config)
			if err != nil {
				b.Fatalf("Failed to create logger: %v", err)
			}
			defer logger.Close()

			msg := "Throughput test message"

			var memStatsBefore runtime.MemStats
			runtime.ReadMemStats(&memStatsBefore)

			b.ResetTimer()
			start := time.Now()

			for i := 0; i < b.N; i++ {
				logger.Log(msg)
			}

			elapsed := time.Since(start)
			b.StopTimer()

			var memStatsAfter runtime.MemStats
			runtime.ReadMemStats(&memStatsAfter)

			logger.Close()

			stats := logger.Stats()
			logsPerSec := float64(stats.TotalLogs) / elapsed.Seconds()
			dropRate := float64(stats.DroppedLogs) / float64(stats.TotalLogs) * 100

			b.ReportMetric(float64(stats.DroppedLogs), "dropped")
			b.ReportMetric(dropRate, "drop_rate_%")
			b.ReportMetric(logsPerSec, "logs/sec")
			b.ReportMetric(float64(stats.TotalLogs), "total_logs")
			b.ReportMetric(float64(memStatsAfter.Alloc-memStatsBefore.Alloc)/1024/1024, "alloc_MB")
			b.ReportMetric(float64(memStatsAfter.NumGC-memStatsBefore.NumGC), "num_gc")
		})
	}
}

// BenchmarkOptimizedScalability tests scalability across many concurrency levels
func BenchmarkOptimizedScalability(b *testing.B) {
	strategies := []Strategy{ShardedDoubleBuffer, ShardedDoubleBufferCAS}
	concurrencyLevels := []int{10, 50, 100, 500, 1000, 5000, 10000}

	bufferSize := 8 * 1024 * 1024
	numShards := 32
	flushInterval := 10 * time.Second

	for _, strategy := range strategies {
		for _, concurrency := range concurrencyLevels {
			b.Run(fmt.Sprintf("%s/Workers-%d", strategy.String(), concurrency), func(b *testing.B) {
				tmpDir := b.TempDir()
				logFile := filepath.Join(tmpDir, "bench.log")

				config := Config{
					BufferSize:    bufferSize,
					FlushInterval: flushInterval,
					LogFilePath:   logFile,
					Strategy:      strategy,
					NumShards:     numShards,
				}

				logger, err := New(config)
				if err != nil {
					b.Fatalf("Failed to create logger: %v", err)
				}
				defer logger.Close()

				msg := "Scalability test"

				b.ResetTimer()
				b.SetParallelism(concurrency)
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						logger.Log(msg)
					}
				})
				b.StopTimer()

				logger.Close()

				stats := logger.Stats()
				dropRate := float64(stats.DroppedLogs) / float64(stats.TotalLogs) * 100

				b.ReportMetric(float64(stats.TotalLogs), "total_logs")
				b.ReportMetric(float64(stats.DroppedLogs), "dropped")
				b.ReportMetric(dropRate, "drop_rate_%")
			})
		}
	}
}

