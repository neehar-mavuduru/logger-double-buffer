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

type Result struct {
	SetName         string
	ScenarioName    string
	Threads         int
	Shards          int
	BufferMB        int
	Duration        float64
	TotalLogs       int64
	DroppedLogs     int64
	DropRate        float64
	Throughput      float64
	Flushes         int64
	BytesWritten    int64
	FlushErrors     int64
	MemAllocMB      float64
	MemTotalAllocMB float64
	NumGC           int
	CPUCores        int
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run process_results.go <csv_file>")
		os.Exit(1)
	}

	csvPath := os.Args[1]
	
	// Read CSV file
	results, err := readCSV(csvPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading CSV: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Loaded %d results\n", len(results))

	// Generate markdown report
	reportPath := "PROFILING_STUDY_RESULTS.md"
	if err := generateReport(results, reportPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Report generated: %s\n", reportPath)
}

func readCSV(path string) ([]Result, error) {
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
		return nil, fmt.Errorf("CSV file is empty or has no data rows")
	}

	var results []Result
	for i, record := range records[1:] { // Skip header
		if len(record) < 17 {
			fmt.Fprintf(os.Stderr, "Warning: Row %d has insufficient columns, skipping\n", i+2)
			continue
		}

		result := Result{
			SetName:      record[0],
			ScenarioName: record[1],
		}

		// Parse numeric fields
		result.Threads, _ = strconv.Atoi(record[2])
		result.Shards, _ = strconv.Atoi(record[3])
		result.BufferMB, _ = strconv.Atoi(record[4])
		result.Duration, _ = strconv.ParseFloat(record[5], 64)
		result.TotalLogs, _ = strconv.ParseInt(record[6], 10, 64)
		result.DroppedLogs, _ = strconv.ParseInt(record[7], 10, 64)
		result.DropRate, _ = strconv.ParseFloat(record[8], 64)
		result.Throughput, _ = strconv.ParseFloat(record[9], 64)
		result.Flushes, _ = strconv.ParseInt(record[10], 10, 64)
		result.BytesWritten, _ = strconv.ParseInt(record[11], 10, 64)
		result.FlushErrors, _ = strconv.ParseInt(record[12], 10, 64)
		result.MemAllocMB, _ = strconv.ParseFloat(record[13], 64)
		result.MemTotalAllocMB, _ = strconv.ParseFloat(record[14], 64)
		result.NumGC, _ = strconv.Atoi(record[15])
		result.CPUCores, _ = strconv.Atoi(record[16])

		results = append(results, result)
	}

	return results, nil
}

func generateReport(results []Result, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	w := func(format string, args ...interface{}) {
		fmt.Fprintf(file, format, args...)
	}

	// Title and metadata
	w("# Profiling Study Results: Sharded Double Buffer CAS\n\n")
	w("**Generated:** %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	w("**Total Scenarios:** %d\n\n", len(results))
	
	if len(results) > 0 {
		w("**System:** %d CPU cores, Docker constrained (4 cores, 4GB RAM)\n\n", results[0].CPUCores)
	}

	w("---\n\n")

	// Executive Summary
	w("## Executive Summary\n\n")
	
	// Find best configurations
	bestThroughput := findBest(results, func(r Result) float64 { return r.Throughput })
	bestDropRate := findBest(results, func(r Result) float64 { return -r.DropRate }) // Negative for lowest
	bestEfficiency := findBest(results, func(r Result) float64 { 
		return r.Throughput / float64(r.CPUCores) 
	})

	w("### Key Findings\n\n")
	w("**Highest Throughput:**\n")
	w("- Configuration: %d threads, %d shards, %dMB buffer\n", 
		bestThroughput.Threads, bestThroughput.Shards, bestThroughput.BufferMB)
	w("- Throughput: %.2f logs/sec\n", bestThroughput.Throughput)
	w("- Drop Rate: %.4f%%\n\n", bestThroughput.DropRate)

	w("**Lowest Drop Rate:**\n")
	w("- Configuration: %d threads, %d shards, %dMB buffer\n", 
		bestDropRate.Threads, bestDropRate.Shards, bestDropRate.BufferMB)
	w("- Throughput: %.2f logs/sec\n", bestDropRate.Throughput)
	w("- Drop Rate: %.4f%%\n\n", bestDropRate.DropRate)

	w("**Best Efficiency (Throughput per Core):**\n")
	w("- Configuration: %d threads, %d shards, %dMB buffer\n", 
		bestEfficiency.Threads, bestEfficiency.Shards, bestEfficiency.BufferMB)
	w("- Throughput: %.2f logs/sec\n", bestEfficiency.Throughput)
	w("- Per-Core: %.2f logs/sec/core\n\n", bestEfficiency.Throughput/float64(bestEfficiency.CPUCores))

	w("---\n\n")

	// Group by set
	sets := groupBySet(results)
	setOrder := []string{"SetA", "SetB", "SetC", "SetD"}

	for _, setName := range setOrder {
		setResults, ok := sets[setName]
		if !ok || len(setResults) == 0 {
			continue
		}

		w("## %s: ", setName)
		switch setName {
		case "SetA":
			w("Baseline (Thread Scaling)\n\n")
		case "SetB":
			w("Shard Scaling\n\n")
		case "SetC":
			w("Buffer Sizing\n\n")
		case "SetD":
			w("Edge Cases\n\n")
		}

		// Create table
		w("| Threads | Shards | Buffer | Throughput | Drop Rate | Flushes | Memory | GC |\n")
		w("|---------|--------|--------|------------|-----------|---------|--------|----|---|\n")

		for _, r := range setResults {
			w("| %d | %d | %dMB | %.2f K/s | %.4f%% | %d | %.1f MB | %d |\n",
				r.Threads, r.Shards, r.BufferMB,
				r.Throughput/1000,
				r.DropRate,
				r.Flushes,
				r.MemAllocMB,
				r.NumGC)
		}

		w("\n")

		// Analysis for this set
		generateSetAnalysis(w, setName, setResults)
		w("\n---\n\n")
	}

	// Heatmaps
	generateHeatmaps(w, results)

	// Recommendations
	w("## Recommendations\n\n")
	generateRecommendations(w, results)

	return nil
}

func findBest(results []Result, score func(Result) float64) Result {
	if len(results) == 0 {
		return Result{}
	}

	best := results[0]
	bestScore := score(best)

	for _, r := range results[1:] {
		s := score(r)
		if s > bestScore {
			best = r
			bestScore = s
		}
	}

	return best
}

func groupBySet(results []Result) map[string][]Result {
	sets := make(map[string][]Result)
	for _, r := range results {
		sets[r.SetName] = append(sets[r.SetName], r)
	}
	return sets
}

func generateSetAnalysis(w func(string, ...interface{}), setName string, results []Result) {
	w("### Analysis\n\n")

	// Calculate statistics
	var totalThroughput, totalDropRate float64
	var maxThroughput, minDropRate float64
	maxThroughput = 0
	minDropRate = 100.0

	for _, r := range results {
		totalThroughput += r.Throughput
		totalDropRate += r.DropRate
		if r.Throughput > maxThroughput {
			maxThroughput = r.Throughput
		}
		if r.DropRate < minDropRate {
			minDropRate = r.DropRate
		}
	}

	avgThroughput := totalThroughput / float64(len(results))
	avgDropRate := totalDropRate / float64(len(results))

	w("- **Average Throughput:** %.2f logs/sec\n", avgThroughput)
	w("- **Peak Throughput:** %.2f logs/sec\n", maxThroughput)
	w("- **Average Drop Rate:** %.4f%%\n", avgDropRate)
	w("- **Minimum Drop Rate:** %.4f%%\n", minDropRate)
	w("\n")
}

func generateHeatmaps(w func(string, ...interface{}), results []Result) {
	w("## Performance Heatmaps\n\n")
	
	// Thread vs Shard heatmap for throughput
	w("### Throughput (K logs/sec) by Threads × Shards\n\n")
	threadShardMap := make(map[string]float64)
	
	for _, r := range results {
		key := fmt.Sprintf("%d-%d", r.Threads, r.Shards)
		if existing, ok := threadShardMap[key]; !ok || r.Throughput > existing {
			threadShardMap[key] = r.Throughput
		}
	}

	// Get unique threads and shards
	threadsSet := make(map[int]bool)
	shardsSet := make(map[int]bool)
	for _, r := range results {
		threadsSet[r.Threads] = true
		shardsSet[r.Shards] = true
	}

	threads := sortedKeys(threadsSet)
	shards := sortedKeys(shardsSet)

	// Header
	w("| Threads \\ Shards |")
	for _, s := range shards {
		w(" %d |", s)
	}
	w("\n")

	w("|")
	for range shards {
		w("---|")
	}
	w("---|\n")

	// Data rows
	for _, t := range threads {
		w("| %d |", t)
		for _, s := range shards {
			key := fmt.Sprintf("%d-%d", t, s)
			if throughput, ok := threadShardMap[key]; ok {
				w(" %.1f |", throughput/1000)
			} else {
				w(" - |")
			}
		}
		w("\n")
	}
	w("\n")
}

func sortedKeys(m map[int]bool) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

func generateRecommendations(w func(string, ...interface{}), results []Result) {
	// Find optimal configurations for different scenarios
	
	w("### For High Throughput (>0.1%% drops acceptable)\n\n")
	highThroughput := findBest(results, func(r Result) float64 { return r.Throughput })
	w("- **Configuration:** %d threads, %d shards, %dMB buffer\n", 
		highThroughput.Threads, highThroughput.Shards, highThroughput.BufferMB)
	w("- **Expected Performance:** %.2f logs/sec, %.4f%% drops\n\n", 
		highThroughput.Throughput, highThroughput.DropRate)

	w("### For Low Drop Rate (<0.1%% target)\n\n")
	// Find configs with <0.1% drops and highest throughput
	var lowDropConfigs []Result
	for _, r := range results {
		if r.DropRate < 0.1 {
			lowDropConfigs = append(lowDropConfigs, r)
		}
	}
	
	if len(lowDropConfigs) > 0 {
		bestLowDrop := findBest(lowDropConfigs, func(r Result) float64 { return r.Throughput })
		w("- **Configuration:** %d threads, %d shards, %dMB buffer\n", 
			bestLowDrop.Threads, bestLowDrop.Shards, bestLowDrop.BufferMB)
		w("- **Expected Performance:** %.2f logs/sec, %.4f%% drops\n\n", 
			bestLowDrop.Throughput, bestLowDrop.DropRate)
	} else {
		w("- No configurations achieved <0.1%% drop rate in constrained environment\n\n")
	}

	w("### For Resource-Constrained Environments\n\n")
	// Find best efficiency (throughput per MB of memory)
	bestMemEfficiency := findBest(results, func(r Result) float64 { 
		if r.MemAllocMB > 0 {
			return r.Throughput / r.MemAllocMB
		}
		return 0
	})
	w("- **Configuration:** %d threads, %d shards, %dMB buffer\n", 
		bestMemEfficiency.Threads, bestMemEfficiency.Shards, bestMemEfficiency.BufferMB)
	w("- **Expected Performance:** %.2f logs/sec, %.1f MB memory\n", 
		bestMemEfficiency.Throughput, bestMemEfficiency.MemAllocMB)
	w("- **Efficiency:** %.2f logs/sec/MB\n\n", 
		bestMemEfficiency.Throughput/math.Max(1, bestMemEfficiency.MemAllocMB))
}

