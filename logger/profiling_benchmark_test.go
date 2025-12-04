package logger

import (
	"fmt"
	"testing"
)

// BenchmarkAllProfilingScenarios runs all profiling scenarios
func BenchmarkAllProfilingScenarios(b *testing.B) {
	scenarios := GetProfilingScenarios()
	
	for _, config := range scenarios {
		b.Run(config.Name, func(b *testing.B) {
			result := RunProfilingTest(config)
			
			// Report metrics
			b.ReportMetric(result.Throughput, "logs/sec")
			b.ReportMetric(result.DropRate, "drop_rate_%")
			b.ReportMetric(float64(result.TotalLogs), "total_logs")
			b.ReportMetric(float64(result.DroppedLogs), "dropped_logs")
			b.ReportMetric(float64(result.TotalFlushes), "flushes")
			b.ReportMetric(float64(result.BytesWritten), "bytes_written")
			b.ReportMetric(result.MemAllocMB, "mem_alloc_MB")
			b.ReportMetric(result.MemTotalAllocMB, "mem_total_alloc_MB")
			b.ReportMetric(float64(result.NumGC), "num_gc")
			
			// Print detailed result
			fmt.Printf("\n=== PROFILING RESULT: %s ===\n", config.Name)
			fmt.Printf("Config: Threads=%d, Shards=%d, BufferSize=%dMB\n", 
				config.Threads, config.Shards, config.BufferSize/(1024*1024))
			fmt.Printf("Duration: %v\n", result.Duration)
			fmt.Printf("Throughput: %.2f logs/sec\n", result.Throughput)
			fmt.Printf("Total Logs: %d\n", result.TotalLogs)
			fmt.Printf("Dropped Logs: %d (%.4f%%)\n", result.DroppedLogs, result.DropRate)
			fmt.Printf("Flushes: %d\n", result.TotalFlushes)
			fmt.Printf("Bytes Written: %d\n", result.BytesWritten)
			fmt.Printf("Memory Alloc: %.2f MB\n", result.MemAllocMB)
			fmt.Printf("GC Count: %d\n", result.NumGC)
			fmt.Printf("CPU Cores: %d\n", result.CPUCores)
			fmt.Printf("================================\n\n")
		})
	}
}

// TestMessage200Bytes verifies the message generator creates exactly 200 bytes
func TestMessage200Bytes(t *testing.T) {
	for i := 0; i < 100; i++ {
		msg := Generate200ByteMessage(i)
		if len(msg) != 200 {
			t.Errorf("Expected 200 bytes, got %d for message %d: %s", len(msg), i, msg)
		}
	}
}

