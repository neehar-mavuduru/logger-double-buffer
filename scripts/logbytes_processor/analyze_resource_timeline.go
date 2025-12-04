package main

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"time"
)

type ResourceSample struct {
	Timestamp      string
	ElapsedSeconds int
	CPUPercent     float64
	MemUsageMB     float64
	MemPercent     float64
	MemLimitMB     float64
	NetRxMB        float64
	NetTxMB        float64
	BlockReadMB    float64
	BlockWriteMB   float64
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run analyze_resource_timeline.go <timeline_csv>")
		fmt.Println("Example: go run scripts/logbytes_processor/analyze_resource_timeline.go results/logbytes/resource_timeline_20250115_120000.csv")
		os.Exit(1)
	}

	csvPath := os.Args[1]

	// Read timeline data
	samples, err := readTimeline(csvPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading timeline: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Loaded %d resource samples\n", len(samples))

	if len(samples) == 0 {
		fmt.Println("No data to analyze")
		return
	}

	// Generate analysis report
	reportPath := "RESOURCE_USAGE_ANALYSIS.md"
	if err := generateAnalysis(samples, reportPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating analysis: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Resource usage analysis saved to: %s\n", reportPath)
}

func readTimeline(path string) ([]ResourceSample, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("timeline file is empty or has no data rows")
	}

	var samples []ResourceSample
	for i, record := range records[1:] { // Skip header
		if len(record) < 10 {
			fmt.Fprintf(os.Stderr, "Warning: Row %d has insufficient columns, skipping\n", i+2)
			continue
		}

		sample := ResourceSample{
			Timestamp: record[0],
		}

		sample.ElapsedSeconds, _ = strconv.Atoi(record[1])
		sample.CPUPercent, _ = strconv.ParseFloat(record[2], 64)
		sample.MemUsageMB, _ = strconv.ParseFloat(record[3], 64)
		sample.MemPercent, _ = strconv.ParseFloat(record[4], 64)
		sample.MemLimitMB, _ = strconv.ParseFloat(record[5], 64)
		sample.NetRxMB, _ = strconv.ParseFloat(record[6], 64)
		sample.NetTxMB, _ = strconv.ParseFloat(record[7], 64)
		sample.BlockReadMB, _ = strconv.ParseFloat(record[8], 64)
		sample.BlockWriteMB, _ = strconv.ParseFloat(record[9], 64)

		samples = append(samples, sample)
	}

	return samples, nil
}

func generateAnalysis(samples []ResourceSample, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	w := func(format string, args ...interface{}) {
		fmt.Fprintf(file, format, args...)
	}

	// Header
	w("# Resource Usage Analysis - LogBytes Profiling\n\n")
	w("**Generated:** %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	w("**Sample Count:** %d\n", len(samples))
	w("**Duration:** %d seconds (%.1f minutes)\n\n", samples[len(samples)-1].ElapsedSeconds, float64(samples[len(samples)-1].ElapsedSeconds)/60.0)

	// Calculate statistics
	cpuStats := calculateStats(samples, func(s ResourceSample) float64 { return s.CPUPercent })
	memStats := calculateStats(samples, func(s ResourceSample) float64 { return s.MemUsageMB })

	// Executive Summary
	w("## Executive Summary\n\n")
	w("### CPU Usage\n")
	w("- **Mean:** %.2f%%\n", cpuStats.Mean)
	w("- **Median (P50):** %.2f%%\n", cpuStats.P50)
	w("- **P95:** %.2f%%\n", cpuStats.P95)
	w("- **P99:** %.2f%%\n", cpuStats.P99)
	w("- **Peak:** %.2f%%\n", cpuStats.Max)
	w("- **Minimum:** %.2f%%\n\n", cpuStats.Min)

	w("### Memory Usage\n")
	w("- **Mean:** %.2f MB\n", memStats.Mean)
	w("- **Median (P50):** %.2f MB\n", memStats.P50)
	w("- **P95:** %.2f MB\n", memStats.P95)
	w("- **P99:** %.2f MB\n", memStats.P99)
	w("- **Peak:** %.2f MB\n", memStats.Max)
	w("- **Minimum:** %.2f MB\n\n", memStats.Min)

	// Time series analysis - divide into 7 scenarios (2 minutes each = 120 seconds)
	w("## Resource Usage by Scenario\n\n")
	w("Assuming 2-minute test windows for each of 7 scenarios:\n\n")
	w("| Scenario | Time Window | Avg CPU%% | Peak CPU%% | Avg Mem MB | Peak Mem MB |\n")
	w("|----------|-------------|----------|-----------|------------|-------------|\n")

	scenarioDuration := 120 // 2 minutes per scenario
	totalScenarios := 7

	for i := 0; i < totalScenarios; i++ {
		startSec := i * scenarioDuration
		endSec := (i + 1) * scenarioDuration

		scenarioSamples := filterSamples(samples, startSec, endSec)
		if len(scenarioSamples) == 0 {
			continue
		}

		scenarioCPU := calculateStats(scenarioSamples, func(s ResourceSample) float64 { return s.CPUPercent })
		scenarioMem := calculateStats(scenarioSamples, func(s ResourceSample) float64 { return s.MemUsageMB })

		w("| Scenario %d | %d-%ds | %.2f | %.2f | %.2f | %.2f |\n",
			i+1, startSec, endSec,
			scenarioCPU.Mean, scenarioCPU.Max,
			scenarioMem.Mean, scenarioMem.Max)
	}
	w("\n")

	// Detailed time-series visualization (ASCII chart)
	w("## CPU Usage Over Time\n\n")
	w("```\n")
	generateASCIIChart(samples, "CPU", func(s ResourceSample) float64 { return s.CPUPercent }, 20, w)
	w("```\n\n")

	w("## Memory Usage Over Time\n\n")
	w("```\n")
	generateASCIIChart(samples, "Memory", func(s ResourceSample) float64 { return s.MemUsageMB }, 20, w)
	w("```\n\n")

	// Network and Block I/O
	totalNetRx := samples[len(samples)-1].NetRxMB
	totalNetTx := samples[len(samples)-1].NetTxMB
	totalBlockRead := samples[len(samples)-1].BlockReadMB
	totalBlockWrite := samples[len(samples)-1].BlockWriteMB

	w("## Network and I/O Statistics\n\n")
	w("### Network I/O\n")
	w("- **Total RX:** %.2f MB\n", totalNetRx)
	w("- **Total TX:** %.2f MB\n", totalNetTx)
	w("- **Average RX Rate:** %.2f MB/min\n", totalNetRx/(float64(samples[len(samples)-1].ElapsedSeconds)/60.0))
	w("- **Average TX Rate:** %.2f MB/min\n\n", totalNetTx/(float64(samples[len(samples)-1].ElapsedSeconds)/60.0))

	w("### Block I/O\n")
	w("- **Total Read:** %.2f MB\n", totalBlockRead)
	w("- **Total Write:** %.2f MB\n", totalBlockWrite)
	w("- **Average Read Rate:** %.2f MB/min\n", totalBlockRead/(float64(samples[len(samples)-1].ElapsedSeconds)/60.0))
	w("- **Average Write Rate:** %.2f MB/min\n\n", totalBlockWrite/(float64(samples[len(samples)-1].ElapsedSeconds)/60.0))

	// Observations and Recommendations
	w("## Observations\n\n")

	if cpuStats.Max > 80 {
		w("⚠️ **High CPU Usage:** Peak CPU usage reached %.2f%%, which may impact application performance\n", cpuStats.Max)
	} else if cpuStats.Max < 20 {
		w("✅ **Low CPU Usage:** Peak CPU usage was only %.2f%%, indicating efficient logging operations\n", cpuStats.Max)
	} else {
		w("✅ **Moderate CPU Usage:** CPU usage peaked at %.2f%%, which is acceptable for logging operations\n", cpuStats.Max)
	}

	if memStats.Max > 3000 {
		w("⚠️ **High Memory Usage:** Peak memory usage reached %.2f MB (>3GB), close to the 4GB limit\n", memStats.Max)
	} else if memStats.Max < 500 {
		w("✅ **Low Memory Usage:** Peak memory usage was only %.2f MB, very efficient\n", memStats.Max)
	} else {
		w("✅ **Moderate Memory Usage:** Memory usage peaked at %.2f MB, well within limits\n", memStats.Max)
	}

	memVariance := memStats.Max - memStats.Min
	if memVariance > 1000 {
		w("\n⚠️ **High Memory Variance:** Memory usage varied by %.2f MB, indicating potential memory churn\n", memVariance)
	} else {
		w("\n✅ **Stable Memory Usage:** Memory usage varied by only %.2f MB, indicating good stability\n", memVariance)
	}

	w("\n## Recommendations\n\n")
	w("Based on the resource usage patterns:\n\n")

	if cpuStats.Mean < 10 {
		w("1. **CPU:** Very low average CPU usage (%.2f%%) suggests room for higher throughput\n", cpuStats.Mean)
	} else if cpuStats.Mean > 50 {
		w("1. **CPU:** High average CPU usage (%.2f%%) may require optimization or scaling\n", cpuStats.Mean)
	} else {
		w("1. **CPU:** Average CPU usage (%.2f%%) is well-balanced for logging operations\n", cpuStats.Mean)
	}

	if memStats.Mean < 1000 {
		w("2. **Memory:** Low average memory usage (%.2f MB) allows for larger buffer sizes if needed\n", memStats.Mean)
	} else if memStats.Mean > 2500 {
		w("2. **Memory:** High average memory usage (%.2f MB) suggests buffer sizes may be optimal or slightly large\n", memStats.Mean)
	} else {
		w("2. **Memory:** Average memory usage (%.2f MB) is appropriate for current buffer configuration\n", memStats.Mean)
	}

	w("3. **I/O:** Block write rate of %.2f MB/min indicates log flush frequency and volume\n", totalBlockWrite/(float64(samples[len(samples)-1].ElapsedSeconds)/60.0))

	return nil
}

type Stats struct {
	Mean float64
	Min  float64
	Max  float64
	P50  float64
	P95  float64
	P99  float64
}

func calculateStats(samples []ResourceSample, extractor func(ResourceSample) float64) Stats {
	if len(samples) == 0 {
		return Stats{}
	}

	values := make([]float64, len(samples))
	var sum float64
	min := math.MaxFloat64
	max := -math.MaxFloat64

	for i, s := range samples {
		val := extractor(s)
		values[i] = val
		sum += val
		if val < min {
			min = val
		}
		if val > max {
			max = val
		}
	}

	sort.Float64s(values)

	return Stats{
		Mean: sum / float64(len(values)),
		Min:  min,
		Max:  max,
		P50:  values[len(values)*50/100],
		P95:  values[len(values)*95/100],
		P99:  values[len(values)*99/100],
	}
}

func filterSamples(samples []ResourceSample, startSec, endSec int) []ResourceSample {
	var filtered []ResourceSample
	for _, s := range samples {
		if s.ElapsedSeconds >= startSec && s.ElapsedSeconds < endSec {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func generateASCIIChart(samples []ResourceSample, label string, extractor func(ResourceSample) float64, rows int, w func(string, ...interface{})) {
	if len(samples) == 0 {
		return
	}

	// Find min and max for scaling
	min := math.MaxFloat64
	max := -math.MaxFloat64
	for _, s := range samples {
		val := extractor(s)
		if val < min {
			min = val
		}
		if val > max {
			max = val
		}
	}

	// Create buckets (group samples into time buckets)
	bucketCount := 60 // 60 columns
	bucketSize := len(samples) / bucketCount
	if bucketSize == 0 {
		bucketSize = 1
	}

	bucketValues := make([]float64, bucketCount)
	for i := 0; i < bucketCount; i++ {
		startIdx := i * bucketSize
		endIdx := startIdx + bucketSize
		if endIdx > len(samples) {
			endIdx = len(samples)
		}
		if startIdx >= len(samples) {
			break
		}

		// Average values in this bucket
		var sum float64
		count := 0
		for j := startIdx; j < endIdx; j++ {
			sum += extractor(samples[j])
			count++
		}
		if count > 0 {
			bucketValues[i] = sum / float64(count)
		}
	}

	// Print chart
	w("%s (%.2f - %.2f)\n", label, min, max)
	w("\n")

	for row := rows - 1; row >= 0; row-- {
		threshold := min + (max-min)*float64(row+1)/float64(rows)
		w("  ")
		for _, val := range bucketValues {
			if val >= threshold {
				w("█")
			} else {
				w(" ")
			}
		}
		if row == rows-1 {
			w(" %.2f\n", max)
		} else if row == 0 {
			w(" %.2f\n", min)
		} else {
			w("\n")
		}
	}

	// Time axis
	w("  ")
	for i := 0; i < bucketCount; i++ {
		if i%10 == 0 {
			w("|")
		} else {
			w("-")
		}
	}
	w("\n")
	w("  0")
	for i := 0; i < bucketCount-10; i++ {
		w(" ")
	}
	w(" Time (minutes) %.1f\n", float64(samples[len(samples)-1].ElapsedSeconds)/60.0)
}

