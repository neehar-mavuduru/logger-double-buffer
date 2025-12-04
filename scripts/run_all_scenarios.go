package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	logger "github.com/neeharmavuduru/logger-double-buffer/logger"
)

func main() {
	fmt.Println("================================")
	fmt.Println("Profiling All Scenarios")
	fmt.Println("================================")
	fmt.Println()

	// Get all scenarios
	scenarios := logger.GetProfilingScenarios()
	
	fmt.Printf("Total scenarios: %d\n", len(scenarios))
	fmt.Printf("Estimated duration: %s\n", logger.GetEstimatedDuration())
	fmt.Println()

	// Display system info
	fmt.Println("System Information:")
	fmt.Println("===================")
	fmt.Printf("OS: %s\n", runtime.GOOS)
	fmt.Printf("Arch: %s\n", runtime.GOARCH)
	fmt.Printf("CPU Cores: %d\n", runtime.NumCPU())
	fmt.Printf("Go Version: %s\n", runtime.Version())
	fmt.Println()

	// Create results directory
	resultsDir := "profiling_results"
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create results directory: %v\n", err)
		os.Exit(1)
	}

	// Create output files
	timestamp := time.Now().Format("20060102_150405")
	csvFile, err := os.Create(filepath.Join(resultsDir, fmt.Sprintf("results_%s.csv", timestamp)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create CSV file: %v\n", err)
		os.Exit(1)
	}
	defer csvFile.Close()

	// Write CSV header
	fmt.Fprintln(csvFile, "SetName,ScenarioName,Threads,Shards,BufferMB,Duration,TotalLogs,DroppedLogs,DropRate,Throughput,Flushes,BytesWritten,FlushErrors,MemAllocMB,MemTotalAllocMB,NumGC,CPUCores")

	// Run all scenarios
	startTime := time.Now()
	completed := 0

	for i, scenario := range scenarios {
		fmt.Printf("\n[%d/%d] Running: %s\n", i+1, len(scenarios), scenario.Name)
		fmt.Printf("  Config: Threads=%d, Shards=%d, Buffer=%dMB\n", 
			scenario.Threads, scenario.Shards, scenario.BufferSize/(1024*1024))

		// Run the test
		result := logger.RunProfilingTest(scenario)

		// Print result
		fmt.Printf("  Duration: %v\n", result.Duration)
		fmt.Printf("  Throughput: %.2f logs/sec\n", result.Throughput)
		fmt.Printf("  Total Logs: %d\n", result.TotalLogs)
		fmt.Printf("  Dropped: %d (%.4f%%)\n", result.DroppedLogs, result.DropRate)
		fmt.Printf("  Flushes: %d\n", result.TotalFlushes)
		fmt.Printf("  Memory: %.2f MB alloc, %d GCs\n", result.MemAllocMB, result.NumGC)

		// Determine set name
		setName := "Unknown"
		if len(scenario.Name) >= 4 {
			setName = scenario.Name[:4]
		}

		// Write to CSV
		fmt.Fprintf(csvFile, "%s,%s,%d,%d,%d,%.2f,%d,%d,%.4f,%.2f,%d,%d,%d,%.2f,%.2f,%d,%d\n",
			setName,
			scenario.Name,
			scenario.Threads,
			scenario.Shards,
			scenario.BufferSize/(1024*1024),
			result.Duration.Seconds(),
			result.TotalLogs,
			result.DroppedLogs,
			result.DropRate,
			result.Throughput,
			result.TotalFlushes,
			result.BytesWritten,
			result.FlushErrors,
			result.MemAllocMB,
			result.MemTotalAllocMB,
			result.NumGC,
			result.CPUCores,
		)

		completed++
		elapsed := time.Since(startTime)
		avgTimePerTest := elapsed / time.Duration(completed)
		remaining := time.Duration(len(scenarios)-completed) * avgTimePerTest

		fmt.Printf("  Progress: %d/%d complete\n", completed, len(scenarios))
		fmt.Printf("  Elapsed: %v, Remaining: ~%v\n", 
			elapsed.Round(time.Second), remaining.Round(time.Second))
	}

	totalDuration := time.Since(startTime)
	fmt.Println()
	fmt.Println("================================")
	fmt.Println("Profiling Complete!")
	fmt.Println("================================")
	fmt.Printf("Total scenarios: %d\n", completed)
	fmt.Printf("Total duration: %v\n", totalDuration.Round(time.Second))
	fmt.Printf("Average per test: %v\n", (totalDuration / time.Duration(completed)).Round(time.Second))
	fmt.Printf("Results saved to: %s\n", csvFile.Name())
	fmt.Println()
}

