package logger

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkAllStrategies benchmarks all 6 logger strategies with various concurrency levels
func BenchmarkAllStrategies(b *testing.B) {
	strategies := []Strategy{Atomic, Mutex, Sharded, ShardedCAS, ShardedDoubleBuffer, ShardedDoubleBufferCAS}
	concurrencyLevels := []int{1, 10, 100, 1000}

	for _, strategy := range strategies {
		for _, concurrency := range concurrencyLevels {
			b.Run(fmt.Sprintf("%s/Concurrency-%d", strategy.String(), concurrency), func(b *testing.B) {
				tmpDir := b.TempDir()
				logFile := filepath.Join(tmpDir, "bench.log")

				config := Config{
					BufferSize:    2 * 1024 * 1024, // 2MB
					FlushInterval: 10 * time.Second,
					LogFilePath:   logFile,
					Strategy:      strategy,
					NumShards:     16,
				}

				logger, err := New(config)
				if err != nil {
					b.Fatalf("Failed to create logger: %v", err)
				}
				defer logger.Close()

				msg := "Benchmark log message with some content for testing"

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
				b.ReportMetric(float64(stats.TotalLogs), "logs")
				b.ReportMetric(float64(stats.DroppedLogs), "dropped")
			})
		}
	}
}

// BenchmarkScalability tests how each strategy scales with increasing concurrency
func BenchmarkScalability(b *testing.B) {
	strategies := []Strategy{Atomic, Mutex, Sharded, ShardedCAS, ShardedDoubleBuffer, ShardedDoubleBufferCAS}
	concurrencyLevels := []int{10, 50, 100, 500, 1000, 5000, 10000}

	for _, strategy := range strategies {
		for _, concurrency := range concurrencyLevels {
			b.Run(fmt.Sprintf("%s/Workers-%d", strategy.String(), concurrency), func(b *testing.B) {
				tmpDir := b.TempDir()
				logFile := filepath.Join(tmpDir, "bench.log")

				config := Config{
					BufferSize:    4 * 1024 * 1024, // 4MB for high concurrency
					FlushInterval: 30 * time.Second,
					LogFilePath:   logFile,
					Strategy:      strategy,
					NumShards:     32, // More shards for high concurrency
				}

				logger, err := New(config)
				if err != nil {
					b.Fatalf("Failed to create logger: %v", err)
				}
				defer logger.Close()

				msg := "Scalability test message"
				var counter atomic.Int64

				b.ResetTimer()

				var wg sync.WaitGroup
				wg.Add(concurrency)

				for i := 0; i < concurrency; i++ {
					go func() {
						defer wg.Done()
						for j := 0; j < b.N/concurrency; j++ {
							logger.Log(msg)
							counter.Add(1)
						}
					}()
				}

				wg.Wait()
				b.StopTimer()

				logger.Close()

				b.ReportMetric(float64(counter.Load()), "total_logs")
			})
		}
	}
}

// BenchmarkStrategyMessageSizes tests different message sizes across all strategies
func BenchmarkStrategyMessageSizes(b *testing.B) {
	strategies := []Strategy{Atomic, Mutex, Sharded, ShardedCAS, ShardedDoubleBuffer, ShardedDoubleBufferCAS}
	messageSizes := []int{16, 64, 256, 1024, 4096}

	for _, strategy := range strategies {
		for _, msgSize := range messageSizes {
			b.Run(fmt.Sprintf("%s/MsgSize-%dB", strategy.String(), msgSize), func(b *testing.B) {
				tmpDir := b.TempDir()
				logFile := filepath.Join(tmpDir, "bench.log")

				config := Config{
					BufferSize:    2 * 1024 * 1024,
					FlushInterval: 10 * time.Second,
					LogFilePath:   logFile,
					Strategy:      strategy,
					NumShards:     16,
				}

				logger, err := New(config)
				if err != nil {
					b.Fatalf("Failed to create logger: %v", err)
				}
				defer logger.Close()

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
}

// BenchmarkStrategyBufferSizes tests different buffer sizes across all strategies
func BenchmarkStrategyBufferSizes(b *testing.B) {
	strategies := []Strategy{Atomic, Mutex, Sharded, ShardedCAS, ShardedDoubleBuffer, ShardedDoubleBufferCAS}
	bufferSizes := []int{
		256 * 1024,      // 256KB
		1024 * 1024,     // 1MB
		4 * 1024 * 1024, // 4MB
		16 * 1024 * 1024, // 16MB
	}

	for _, strategy := range strategies {
		for _, bufSize := range bufferSizes {
			b.Run(fmt.Sprintf("%s/BufSize-%dMB", strategy.String(), bufSize/(1024*1024)), func(b *testing.B) {
				tmpDir := b.TempDir()
				logFile := filepath.Join(tmpDir, "bench.log")

				config := Config{
					BufferSize:    bufSize,
					FlushInterval: 10 * time.Second,
					LogFilePath:   logFile,
					Strategy:      strategy,
					NumShards:     16,
				}

				logger, err := New(config)
				if err != nil {
					b.Fatalf("Failed to create logger: %v", err)
				}
				defer logger.Close()

				msg := "Buffer size test message"

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
}

// BenchmarkShardCounts tests different shard counts for sharded strategies
func BenchmarkShardCounts(b *testing.B) {
	strategies := []Strategy{Sharded, ShardedCAS, ShardedDoubleBuffer, ShardedDoubleBufferCAS}
	shardCounts := []int{4, 8, 16, 32, 64, 128}

	for _, strategy := range strategies {
		for _, numShards := range shardCounts {
			b.Run(fmt.Sprintf("%s/Shards-%d", strategy.String(), numShards), func(b *testing.B) {
				tmpDir := b.TempDir()
				logFile := filepath.Join(tmpDir, "bench.log")

				config := Config{
					BufferSize:    2 * 1024 * 1024,
					FlushInterval: 10 * time.Second,
					LogFilePath:   logFile,
					Strategy:      strategy,
					NumShards:     numShards,
				}

				logger, err := New(config)
				if err != nil {
					b.Fatalf("Failed to create logger: %v", err)
				}
				defer logger.Close()

				msg := "Shard count test message"

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
				b.ReportMetric(float64(stats.DroppedLogs), "dropped")
			})
		}
	}
}

// BenchmarkTailLatency measures tail latency for all strategies
func BenchmarkTailLatency(b *testing.B) {
	strategies := []Strategy{Atomic, Mutex, Sharded, ShardedCAS, ShardedDoubleBuffer, ShardedDoubleBufferCAS}

	for _, strategy := range strategies {
		b.Run(strategy.String(), func(b *testing.B) {
			tmpDir := b.TempDir()
			logFile := filepath.Join(tmpDir, "bench.log")

			config := Config{
				BufferSize:    2 * 1024 * 1024,
				FlushInterval: 10 * time.Second,
				LogFilePath:   logFile,
				Strategy:      strategy,
				NumShards:     16,
			}

			logger, err := New(config)
			if err != nil {
				b.Fatalf("Failed to create logger: %v", err)
			}
			defer logger.Close()

			msg := "Latency test message"
			latencies := make([]time.Duration, 0, b.N)
			var mu sync.Mutex

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				localLatencies := make([]time.Duration, 0, 1000)
				for pb.Next() {
					start := time.Now()
					logger.Log(msg)
					latency := time.Since(start)
					localLatencies = append(localLatencies, latency)
				}
				mu.Lock()
				latencies = append(latencies, localLatencies...)
				mu.Unlock()
			})
			b.StopTimer()

			logger.Close()

			// Calculate percentiles
			if len(latencies) > 0 {
				// Sort latencies
				for i := 0; i < len(latencies)-1; i++ {
					for j := i + 1; j < len(latencies); j++ {
						if latencies[i] > latencies[j] {
							latencies[i], latencies[j] = latencies[j], latencies[i]
						}
					}
				}

				p50 := latencies[len(latencies)*50/100]
				p95 := latencies[len(latencies)*95/100]
				p99 := latencies[len(latencies)*99/100]
				p999 := latencies[len(latencies)*999/1000]

				b.ReportMetric(float64(p50.Nanoseconds()), "p50_ns")
				b.ReportMetric(float64(p95.Nanoseconds()), "p95_ns")
				b.ReportMetric(float64(p99.Nanoseconds()), "p99_ns")
				b.ReportMetric(float64(p999.Nanoseconds()), "p999_ns")
			}
		})
	}
}

// BenchmarkThroughput measures raw throughput for all strategies
func BenchmarkThroughput(b *testing.B) {
	strategies := []Strategy{Atomic, Mutex, Sharded, ShardedCAS, ShardedDoubleBuffer, ShardedDoubleBufferCAS}

	for _, strategy := range strategies {
		b.Run(strategy.String(), func(b *testing.B) {
			tmpDir := b.TempDir()
			logFile := filepath.Join(tmpDir, "bench.log")

			config := Config{
				BufferSize:    4 * 1024 * 1024,
				FlushInterval: 30 * time.Second,
				LogFilePath:   logFile,
				Strategy:      strategy,
				NumShards:     16,
			}

			logger, err := New(config)
			if err != nil {
				b.Fatalf("Failed to create logger: %v", err)
			}
			defer logger.Close()

			msg := "Throughput test message"

			b.ResetTimer()
			start := time.Now()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					logger.Log(msg)
				}
			})

			elapsed := time.Since(start)
			b.StopTimer()

			logger.Close()

			stats := logger.Stats()
			throughput := float64(stats.TotalLogs) / elapsed.Seconds()
			b.ReportMetric(throughput, "logs/sec")
			b.ReportMetric(float64(stats.TotalLogs), "total_logs")
			b.ReportMetric(float64(stats.DroppedLogs), "dropped")
		})
	}
}

// BenchmarkDirectComparison directly compares all 6 strategies under identical conditions
func BenchmarkDirectComparison(b *testing.B) {
	concurrency := 100
	strategies := []Strategy{Atomic, Mutex, Sharded, ShardedCAS, ShardedDoubleBuffer, ShardedDoubleBufferCAS}

	for _, strategy := range strategies {
		b.Run(strategy.String(), func(b *testing.B) {
			tmpDir := b.TempDir()
			logFile := filepath.Join(tmpDir, "bench.log")

			config := Config{
				BufferSize:    2 * 1024 * 1024,
				FlushInterval: 10 * time.Second,
				LogFilePath:   logFile,
				Strategy:      strategy,
				NumShards:     16,
			}

			logger, err := New(config)
			if err != nil {
				b.Fatalf("Failed to create logger: %v", err)
			}
			defer logger.Close()

			msg := "Direct comparison test message with moderate length for realistic testing"

			b.ResetTimer()

			var wg sync.WaitGroup
			wg.Add(concurrency)

			for i := 0; i < concurrency; i++ {
				go func() {
					defer wg.Done()
					for j := 0; j < b.N/concurrency; j++ {
						logger.Log(msg)
					}
				}()
			}

			wg.Wait()
			b.StopTimer()

			logger.Close()

			stats := logger.Stats()
			b.ReportMetric(float64(stats.TotalLogs), "total_logs")
			b.ReportMetric(float64(stats.TotalFlushes), "flushes")
			b.ReportMetric(float64(stats.DroppedLogs), "dropped")
			b.ReportMetric(float64(stats.BytesWritten), "bytes")
		})
	}
}

