package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ScenarioResult struct {
	Name        string
	Threads     int
	BufferMB    int
	Shards      int
	
	// Metrics at 270s (4.5 minutes)
	TotalRequests   int64
	TotalLogs       int64
	DroppedLogs     int64
	DropRatePct     float64
	
	LatencyP50      float64
	LatencyP95      float64
	LatencyP99      float64
	LatencyP999     float64
	
	GCCycles        int64
	GCPauseMs       float64
	MemoryMB        float64
	
	// CPU stats (average over first 270s)
	CPUMean         float64
	CPUMedian       float64
	CPUMax          float64
}

func main() {
	fmt.Println("Processing Sustained Test Results...")
	fmt.Println("Analysis Window: First 4.5 minutes (270 seconds)")
	fmt.Println()
	
	resultsDir := "results/sustained_tests"
	
	// Find all server log files
	pattern := filepath.Join(resultsDir, "*_server_*.log")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		fmt.Printf("No results found in %s\n", resultsDir)
		return
	}
	
	var results []ScenarioResult
	
	for _, logFile := range matches {
		result := processScenario(logFile, resultsDir)
		if result != nil {
			results = append(results, *result)
		}
	}
	
	if len(results) == 0 {
		fmt.Println("No valid results to process")
		return
	}
	
	// Sort by drop rate
	sort.Slice(results, func(i, j int) bool {
		return results[i].DropRatePct < results[j].DropRatePct
	})
	
	// Generate report
	generateReport(results, resultsDir)
	
	fmt.Println()
	fmt.Println("✓ Analysis complete!")
	fmt.Printf("  - %s/SUSTAINED_TEST_RESULTS.md\n", resultsDir)
}

func processScenario(logFile, resultsDir string) *ScenarioResult {
	// Extract scenario name from filename
	base := filepath.Base(logFile)
	scenarioName := strings.Split(base, "_server_")[0]
	
	fmt.Printf("Processing %s...\n", scenarioName)
	
	result := &ScenarioResult{
		Name: scenarioName,
	}
	
	// Parse scenario name for config
	if strings.Contains(scenarioName, "baseline") {
		result.Threads = 100
		result.BufferMB = 128
		result.Shards = 8
	} else if strings.Contains(scenarioName, "winner_50t") {
		result.Threads = 50
		result.BufferMB = 64
		result.Shards = 4
	}
	
	// Read server logs and find metrics at ~270 seconds
	file, err := os.Open(logFile)
	if err != nil {
		fmt.Printf("  Error reading %s: %v\n", logFile, err)
		return nil
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	metricsLines := []string{}
	
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "METRICS:") {
			metricsLines = append(metricsLines, line)
		}
	}
	
	if len(metricsLines) == 0 {
		fmt.Printf("  Warning: No metrics found for %s\n", scenarioName)
		return nil
	}
	
	// Use metric at ~270s (27th if available, or closest before that)
	targetMetric := 27
	if len(metricsLines) < targetMetric {
		targetMetric = len(metricsLines) - 3 // Use one that's ~30s before end to avoid shutdown
	}
	if targetMetric < 1 {
		targetMetric = len(metricsLines) - 1
	}
	
	metrics270 := metricsLines[targetMetric-1]
	fmt.Printf("  Using metric %d of %d (target was 27 for 270s)\n", targetMetric, len(metricsLines))
	
	// Parse metrics line
	parseMetricsLine(metrics270, result)
	
	// Process resource timeline (first 270 seconds)
	resourceFile := strings.Replace(logFile, "_server_", "_resource_", 1)
	resourceFile = strings.Replace(resourceFile, ".log", ".csv", 1)
	processResourceData(resourceFile, result)
	
	return result
}

func parseMetricsLine(line string, result *ScenarioResult) {
	// Extract: Total Requests: 268228 | Latency P50: 8.00us ... | Logs: 268230 Dropped: 1829 (0.6817%) | GC: 793 cycles 275.89ms pause | Mem: 385.76MB
	
	reRequests := regexp.MustCompile(`Total Requests: (\d+)`)
	reLogs := regexp.MustCompile(`Logs: (\d+) Dropped: (\d+) \(([0-9.]+)%\)`)
	reLatency := regexp.MustCompile(`P50: ([0-9.]+)us.*P95: ([0-9.]+)us.*P99: ([0-9.]+)us.*P999: ([0-9.]+)us`)
	reGC := regexp.MustCompile(`GC: (\d+) cycles ([0-9.]+)ms`)
	reMem := regexp.MustCompile(`Mem: ([0-9.]+)MB`)
	
	if match := reRequests.FindStringSubmatch(line); len(match) > 1 {
		result.TotalRequests, _ = strconv.ParseInt(match[1], 10, 64)
	}
	
	if match := reLogs.FindStringSubmatch(line); len(match) > 3 {
		result.TotalLogs, _ = strconv.ParseInt(match[1], 10, 64)
		result.DroppedLogs, _ = strconv.ParseInt(match[2], 10, 64)
		result.DropRatePct, _ = strconv.ParseFloat(match[3], 64)
	}
	
	if match := reLatency.FindStringSubmatch(line); len(match) > 4 {
		result.LatencyP50, _ = strconv.ParseFloat(match[1], 64)
		result.LatencyP95, _ = strconv.ParseFloat(match[2], 64)
		result.LatencyP99, _ = strconv.ParseFloat(match[3], 64)
		result.LatencyP999, _ = strconv.ParseFloat(match[4], 64)
	}
	
	if match := reGC.FindStringSubmatch(line); len(match) > 2 {
		result.GCCycles, _ = strconv.ParseInt(match[1], 10, 64)
		result.GCPauseMs, _ = strconv.ParseFloat(match[2], 64)
	}
	
	if match := reMem.FindStringSubmatch(line); len(match) > 1 {
		result.MemoryMB, _ = strconv.ParseFloat(match[1], 64)
	}
}

func processResourceData(resourceFile string, result *ScenarioResult) {
	file, err := os.Open(resourceFile)
	if err != nil {
		return
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	var cpuValues []float64
	startTime := int64(0)
	
	// Skip header
	scanner.Scan()
	
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue
		}
		
		timestamp, _ := strconv.ParseInt(fields[0], 10, 64)
		if startTime == 0 {
			startTime = timestamp
		}
		
		// Only process first 270 seconds
		elapsed := timestamp - startTime
		if elapsed > 270 {
			break
		}
		
		cpu, _ := strconv.ParseFloat(fields[1], 64)
		cpuValues = append(cpuValues, cpu)
	}
	
	if len(cpuValues) > 0 {
		// Calculate CPU stats
		sort.Float64s(cpuValues)
		
		sum := 0.0
		for _, v := range cpuValues {
			sum += v
		}
		result.CPUMean = sum / float64(len(cpuValues))
		result.CPUMedian = cpuValues[len(cpuValues)/2]
		result.CPUMax = cpuValues[len(cpuValues)-1]
	}
}

func generateReport(results []ScenarioResult, resultsDir string) {
	outputFile := filepath.Join(resultsDir, "SUSTAINED_TEST_RESULTS.md")
	f, err := os.Create(outputFile)
	if err != nil {
		fmt.Printf("Error creating report: %v\n", err)
		return
	}
	defer f.Close()
	
	w := bufio.NewWriter(f)
	defer w.Flush()
	
	// Header
	fmt.Fprintln(w, "# Sustained Performance Test Results")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "**Date**: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintln(w, "**Test Configuration**: 1000 RPS, 5 minutes duration")
	fmt.Fprintln(w, "**Analysis Window**: First 4.5 minutes (270 seconds)")
	fmt.Fprintln(w, "**Rationale**: Exclude test shutdown artifacts")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)
	
	// Summary table
	fmt.Fprintln(w, "## Results Summary")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Scenario | Threads | Buffer | Shards | Requests | Drop% | CPU% | P99 (µs) | P999 (µs) | GC Cycles | Memory (MB) |")
	fmt.Fprintln(w, "|----------|---------|--------|--------|----------|-------|------|----------|-----------|-----------|-------------|")
	
	for _, r := range results {
		fmt.Fprintf(w, "| %s | %d | %dMB | %d | %d | **%.4f%%** | %.1f%% | %.2f | %.2f | %d | %.1f |\n",
			r.Name, r.Threads, r.BufferMB, r.Shards, r.TotalRequests,
			r.DropRatePct, r.CPUMean, r.LatencyP99, r.LatencyP999,
			r.GCCycles, r.MemoryMB)
	}
	
	fmt.Fprintln(w)
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)
	
	// Detailed analysis
	fmt.Fprintln(w, "## Detailed Analysis")
	fmt.Fprintln(w)
	
	for _, r := range results {
		fmt.Fprintf(w, "### %s\n", r.Name)
		fmt.Fprintln(w)
		fmt.Fprintf(w, "**Configuration:**\n")
		fmt.Fprintf(w, "- Threads: %d\n", r.Threads)
		fmt.Fprintf(w, "- Buffer: %d MB\n", r.BufferMB)
		fmt.Fprintf(w, "- Shards: %d\n", r.Shards)
		fmt.Fprintln(w)
		
		fmt.Fprintf(w, "**Performance (270 seconds):**\n")
		fmt.Fprintf(w, "- Total Requests: %d\n", r.TotalRequests)
		fmt.Fprintf(w, "- Total Logs: %d\n", r.TotalLogs)
		fmt.Fprintf(w, "- Dropped Logs: %d\n", r.DroppedLogs)
		fmt.Fprintf(w, "- Drop Rate: **%.4f%%** %s\n", r.DropRatePct, getDropRateEmoji(r.DropRatePct))
		fmt.Fprintln(w)
		
		fmt.Fprintf(w, "**Latency:**\n")
		fmt.Fprintf(w, "- P50: %.2f µs\n", r.LatencyP50)
		fmt.Fprintf(w, "- P95: %.2f µs\n", r.LatencyP95)
		fmt.Fprintf(w, "- P99: %.2f µs\n", r.LatencyP99)
		fmt.Fprintf(w, "- P999: %.2f µs\n", r.LatencyP999)
		fmt.Fprintln(w)
		
		fmt.Fprintf(w, "**Resource Usage:**\n")
		fmt.Fprintf(w, "- CPU Mean: %.1f%%\n", r.CPUMean)
		fmt.Fprintf(w, "- CPU Median: %.1f%%\n", r.CPUMedian)
		fmt.Fprintf(w, "- CPU Max: %.1f%%\n", r.CPUMax)
		fmt.Fprintf(w, "- Memory: %.1f MB\n", r.MemoryMB)
		fmt.Fprintf(w, "- GC Cycles: %d (%.2f cycles/sec)\n", r.GCCycles, float64(r.GCCycles)/270.0)
		fmt.Fprintf(w, "- GC Pause: %.2f ms total (%.3f ms/cycle)\n", r.GCPauseMs, r.GCPauseMs/float64(r.GCCycles))
		fmt.Fprintln(w)
		
		fmt.Fprintf(w, "**Verdict:** %s\n", getVerdict(r.DropRatePct))
		fmt.Fprintln(w)
	}
	
	// Comparison
	if len(results) == 2 {
		fmt.Fprintln(w, "---")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "## Head-to-Head Comparison")
		fmt.Fprintln(w)
		
		r1, r2 := results[0], results[1]
		
		fmt.Fprintf(w, "| Metric | %s | %s | Winner |\n", r1.Name, r2.Name)
		fmt.Fprintln(w, "|--------|---------|---------|--------|")
		
		// Drop Rate
		dropWinner := r1.Name
		if r2.DropRatePct < r1.DropRatePct {
			dropWinner = r2.Name
		}
		fmt.Fprintf(w, "| Drop Rate | %.4f%% | %.4f%% | **%s** |\n", r1.DropRatePct, r2.DropRatePct, dropWinner)
		
		// Latency P99
		latWinner := r1.Name
		if r2.LatencyP99 < r1.LatencyP99 {
			latWinner = r2.Name
		}
		fmt.Fprintf(w, "| P99 Latency | %.2fµs | %.2fµs | **%s** |\n", r1.LatencyP99, r2.LatencyP99, latWinner)
		
		// CPU
		cpuWinner := r1.Name
		if r2.CPUMean < r1.CPUMean {
			cpuWinner = r2.Name
		}
		fmt.Fprintf(w, "| CPU Usage | %.1f%% | %.1f%% | **%s** |\n", r1.CPUMean, r2.CPUMean, cpuWinner)
		
		// Throughput
		fmt.Fprintf(w, "| Throughput | %d req | %d req | - |\n", r1.TotalRequests, r2.TotalRequests)
		
		fmt.Fprintln(w)
	}
	
	// Recommendations
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Recommendations")
	fmt.Fprintln(w)
	
	best := results[0]
	fmt.Fprintf(w, "**Best Configuration for 1000 RPS sustained load:**\n")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "```yaml\n")
	fmt.Fprintf(w, "Threads:      %d\n", best.Threads)
	fmt.Fprintf(w, "Buffer Size:  %d MB\n", best.BufferMB)
	fmt.Fprintf(w, "Shards:       %d\n", best.Shards)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Expected Performance:\n")
	fmt.Fprintf(w, "  Drop Rate:  %.4f%%\n", best.DropRatePct)
	fmt.Fprintf(w, "  CPU Usage:  %.1f%%\n", best.CPUMean)
	fmt.Fprintf(w, "  P99 Latency: %.2fµs\n", best.LatencyP99)
	fmt.Fprintf(w, "  Memory:     %.1fMB\n", best.MemoryMB)
	fmt.Fprintf(w, "```\n")
	fmt.Fprintln(w)
	
	if best.DropRatePct < 1.0 {
		fmt.Fprintln(w, "✅ **Production Ready** - Drop rate < 1%")
	} else if best.DropRatePct < 5.0 {
		fmt.Fprintln(w, "⚠️ **Acceptable** - Drop rate < 5%, monitor in production")
	} else {
		fmt.Fprintln(w, "❌ **Needs Tuning** - Drop rate > 5%, consider increasing buffer")
	}
}

func getDropRateEmoji(dropRate float64) string {
	if dropRate < 1.0 {
		return "✅"
	} else if dropRate < 5.0 {
		return "⚠️"
	}
	return "❌"
}

func getVerdict(dropRate float64) string {
	if dropRate < 1.0 {
		return "✅ **Production Ready** - Excellent sustained performance"
	} else if dropRate < 5.0 {
		return "⚠️ **Acceptable** - Good performance, monitor in production"
	}
	return "❌ **Needs Tuning** - Consider increasing buffer size"
}

