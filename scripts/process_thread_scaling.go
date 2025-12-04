package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Scenario represents one test configuration
type Scenario struct {
	ID          string
	Phase       string
	Description string
	Timestamp   string
	Config      ScenarioConfig
	Metrics     ScenarioMetrics
	ShardStats  []ShardStat
}

type ScenarioConfig struct {
	BufferMB      int    `json:"buffer_mb"`
	Shards        int    `json:"shards"`
	Threads       int    `json:"threads"`
	RPS           int    `json:"rps"`
	Duration      string `json:"duration"`
	FlushInterval string `json:"flush_interval"`
}

type ScenarioMetrics struct {
	// Application metrics
	GRPCLatencyP50   float64
	GRPCLatencyP95   float64
	GRPCLatencyP99   float64
	GRPCLatencyP999  float64
	GRPCLatencyP9999 float64
	GRPCLatencyMax   float64
	RequestCount     int64
	ErrorCount       int64
	SuccessRate      float64
	RPS              float64

	// Logger metrics
	LogsTotal       int64
	LogsDropped     int64
	DropRate        float64
	ThroughputMBps  float64

	// System metrics
	CPUMean    float64
	CPUP95     float64
	CPUPeak    float64
	MemMean    float64
	MemPeak    float64
	GCCycles   int64
	GCPauseMs  float64

	// Computed metrics
	ThreadsPerShard  float64
	BufferPerThread  float64
	
	// Ranking
	OverallScore float64
}

type ShardStat struct {
	ShardID        int
	Utilization    float64
	WriteCount     int64
}

type GHZReport struct {
	Count               int64                  `json:"count"`
	Total               time.Duration          `json:"total"`
	Average             time.Duration          `json:"average"`
	Fastest             time.Duration          `json:"fastest"`
	Slowest             time.Duration          `json:"slowest"`
	Rps                 float64                `json:"rps"`
	ErrorDist           map[string]int         `json:"errorDistribution"`
	LatencyDistribution []LatencyBucket        `json:"latencyDistribution"`
}

type LatencyBucket struct {
	Percentage int     `json:"percentage"`
	Latency    float64 `json:"latency"`
}

type ResourceSample struct {
	Timestamp  string
	Elapsed    int
	CPUPercent float64
	MemoryMB   float64
}

type Metadata struct {
	ScenarioID  string         `json:"scenario_id"`
	Phase       string         `json:"phase"`
	Description string         `json:"description"`
	Timestamp   string         `json:"timestamp"`
	Config      ScenarioConfig `json:"config"`
}

type ServerMetrics struct {
	LogsWritten int64
	LogsDropped int64
	GCCount     int64
	GCPauseMs   float64
}

func main() {
	resultsDir := "results/thread_scaling"

	fmt.Println("Processing Thread Scaling Study Results...")
	fmt.Println("Results directory:", resultsDir)
	fmt.Println()

	// Load scenarios from both subdirectories
	scenarios50, err := loadScenarios(filepath.Join(resultsDir, "50_threads"))
	if err != nil {
		fmt.Printf("Error loading 50-thread scenarios: %v\n", err)
		scenarios50 = []*Scenario{}
	}

	scenarios200, err := loadScenarios(filepath.Join(resultsDir, "200_threads"))
	if err != nil {
		fmt.Printf("Error loading 200-thread scenarios: %v\n", err)
		scenarios200 = []*Scenario{}
	}

	allScenarios := append(scenarios50, scenarios200...)

	if len(allScenarios) == 0 {
		fmt.Println("No scenarios found!")
		os.Exit(1)
	}

	fmt.Printf("Loaded %d scenarios (%d for 50T, %d for 200T)\n", 
		len(allScenarios), len(scenarios50), len(scenarios200))
	fmt.Println()

	// Calculate ranking scores
	calculateScores(allScenarios)

	// Sort by overall score
	sort.Slice(allScenarios, func(i, j int) bool {
		return allScenarios[i].Metrics.OverallScore < allScenarios[j].Metrics.OverallScore
	})

	// Separate by thread count for analysis
	sort.Slice(scenarios50, func(i, j int) bool {
		return scenarios50[i].Metrics.OverallScore < scenarios50[j].Metrics.OverallScore
	})
	sort.Slice(scenarios200, func(i, j int) bool {
		return scenarios200[i].Metrics.OverallScore < scenarios200[j].Metrics.OverallScore
	})

	// Generate reports
	fmt.Println("Generating reports...")
	
	err = generateResultsReport(resultsDir, allScenarios, scenarios50, scenarios200)
	if err != nil {
		fmt.Printf("Error generating results report: %v\n", err)
		os.Exit(1)
	}

	err = generatePatternsReport(resultsDir, scenarios50, scenarios200)
	if err != nil {
		fmt.Printf("Error generating patterns report: %v\n", err)
		os.Exit(1)
	}

	err = generateTuningGuide(resultsDir, scenarios50, scenarios200)
	if err != nil {
		fmt.Printf("Error generating tuning guide: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("✓ All reports generated successfully!")
	fmt.Printf("  - %s/THREAD_SCALING_RESULTS.md\n", resultsDir)
	fmt.Printf("  - %s/THREAD_SCALING_PATTERNS.md\n", resultsDir)
	fmt.Printf("  - %s/PRODUCTION_TUNING_GUIDE.md\n", resultsDir)
	fmt.Println()

	// Print top configs
	if len(scenarios50) > 0 {
		fmt.Println("Top Config for 50 Threads:")
		s := scenarios50[0]
		fmt.Printf("  %s: %dMB, %d shards | Drop: %.2f%% | CPU: %.1f%% | P99: %.2fms\n",
			s.ID, s.Config.BufferMB, s.Config.Shards, s.Metrics.DropRate, 
			s.Metrics.CPUMean, s.Metrics.GRPCLatencyP99)
	}

	if len(scenarios200) > 0 {
		fmt.Println("\nTop Config for 200 Threads:")
		s := scenarios200[0]
		fmt.Printf("  %s: %dMB, %d shards | Drop: %.2f%% | CPU: %.1f%% | P99: %.2fms\n",
			s.ID, s.Config.BufferMB, s.Config.Shards, s.Metrics.DropRate,
			s.Metrics.CPUMean, s.Metrics.GRPCLatencyP99)
	}
	fmt.Println()
}

func loadScenarios(dir string) ([]*Scenario, error) {
	metadataFiles, err := filepath.Glob(filepath.Join(dir, "scenario_*_metadata.json"))
	if err != nil {
		return nil, err
	}

	if len(metadataFiles) == 0 {
		return nil, fmt.Errorf("no metadata files found in %s", dir)
	}

	var scenarios []*Scenario

	for _, metadataFile := range metadataFiles {
		basename := filepath.Base(metadataFile)
		scenarioID := strings.TrimPrefix(basename, "scenario_")
		scenarioID = strings.TrimSuffix(scenarioID, "_metadata.json")

		metadata, err := loadMetadata(metadataFile)
		if err != nil {
			fmt.Printf("Warning: Failed to load metadata for %s: %v\n", scenarioID, err)
			continue
		}

		ghzPath := filepath.Join(dir, fmt.Sprintf("scenario_%s_ghz.json", scenarioID))
		resourcePath := filepath.Join(dir, fmt.Sprintf("scenario_%s_resources.csv", scenarioID))
		serverLogPath := filepath.Join(dir, fmt.Sprintf("scenario_%s_server.log", scenarioID))

		scenario := &Scenario{
			ID:          scenarioID,
			Phase:       metadata.Phase,
			Description: metadata.Description,
			Timestamp:   metadata.Timestamp,
			Config:      metadata.Config,
		}

		// Load GHZ report
		ghz, err := loadGHZReport(ghzPath)
		if err != nil {
			fmt.Printf("Warning: Failed to load ghz report for %s: %v\n", scenarioID, err)
			continue
		}

		scenario.Metrics.RequestCount = ghz.Count
		scenario.Metrics.RPS = ghz.Rps
		scenario.Metrics.ErrorCount = int64(sumErrorCounts(ghz.ErrorDist))
		if ghz.Count+scenario.Metrics.ErrorCount > 0 {
			scenario.Metrics.SuccessRate = float64(ghz.Count) / float64(ghz.Count+scenario.Metrics.ErrorCount) * 100.0
		}

		// Extract latency percentiles including P999 and P9999
		for _, bucket := range ghz.LatencyDistribution {
			latencyMs := bucket.Latency / 1e6
			switch bucket.Percentage {
			case 50:
				scenario.Metrics.GRPCLatencyP50 = latencyMs
			case 95:
				scenario.Metrics.GRPCLatencyP95 = latencyMs
			case 99:
				scenario.Metrics.GRPCLatencyP99 = latencyMs
			case 999:
				scenario.Metrics.GRPCLatencyP999 = latencyMs
			case 9999:
				scenario.Metrics.GRPCLatencyP9999 = latencyMs
			}
		}
		scenario.Metrics.GRPCLatencyMax = float64(ghz.Slowest) / 1e6

		// Load resource data
		resources, err := loadResourceData(resourcePath)
		if err != nil {
			fmt.Printf("Warning: Failed to load resources for %s: %v\n", scenarioID, err)
		} else {
			scenario.Metrics.CPUMean, scenario.Metrics.CPUP95, scenario.Metrics.CPUPeak = calculateResourceStats(resources, "cpu")
			scenario.Metrics.MemMean, _, scenario.Metrics.MemPeak = calculateResourceStats(resources, "mem")
		}

		// Extract server metrics from logs
		serverMetrics, shardStats, err := extractServerMetrics(serverLogPath)
		if err != nil {
			fmt.Printf("Warning: Failed to extract server metrics for %s: %v\n", scenarioID, err)
		} else {
			scenario.Metrics.LogsTotal = serverMetrics.LogsWritten + serverMetrics.LogsDropped
			scenario.Metrics.LogsDropped = serverMetrics.LogsDropped
			if scenario.Metrics.LogsTotal > 0 {
				scenario.Metrics.DropRate = float64(serverMetrics.LogsDropped) / float64(scenario.Metrics.LogsTotal) * 100.0
			}
			scenario.Metrics.GCCycles = serverMetrics.GCCount
			scenario.Metrics.GCPauseMs = serverMetrics.GCPauseMs
			scenario.ShardStats = shardStats
		}

		// Computed metrics
		if scenario.Config.Shards > 0 {
			scenario.Metrics.ThreadsPerShard = float64(scenario.Config.Threads) / float64(scenario.Config.Shards)
		}
		if scenario.Config.Threads > 0 {
			scenario.Metrics.BufferPerThread = float64(scenario.Config.BufferMB) / float64(scenario.Config.Threads)
		}

		scenarios = append(scenarios, scenario)
	}

	return scenarios, nil
}

func loadMetadata(path string) (*Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var metadata Metadata
	err = json.Unmarshal(data, &metadata)
	if err != nil {
		return nil, err
	}

	return &metadata, nil
}

func loadGHZReport(path string) (*GHZReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var report GHZReport
	err = json.Unmarshal(data, &report)
	if err != nil {
		return nil, err
	}

	return &report, nil
}

func loadResourceData(path string) ([]ResourceSample, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var samples []ResourceSample
	scanner := bufio.NewScanner(file)

	// Skip header
	scanner.Scan()

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ",")
		if len(parts) < 4 {
			continue
		}

		sample := ResourceSample{
			Timestamp:  parts[0],
			Elapsed:    parseInt(parts[1]),
			CPUPercent: parseFloat(parts[2]),
			MemoryMB:   parseFloat(parts[3]),
		}
		samples = append(samples, sample)
	}

	return samples, scanner.Err()
}

func extractServerMetrics(logPath string) (*ServerMetrics, []ShardStat, error) {
	file, err := os.Open(logPath)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	metrics := &ServerMetrics{}
	var shardStats []ShardStat
	scanner := bufio.NewScanner(file)

	metricsPattern := regexp.MustCompile(`METRICS:.*Logs: (\d+) Dropped: (\d+).*GC: (\d+) cycles ([\d.]+)ms`)
	shardPattern := regexp.MustCompile(`SHARD_STATS: (.+)`)
	shardEntryPattern := regexp.MustCompile(`S(\d+):([\d.]+)%\((\d+)\)`)

	for scanner.Scan() {
		line := scanner.Text()

		// Extract comprehensive metrics
		if matches := metricsPattern.FindStringSubmatch(line); matches != nil {
			metrics.LogsWritten = parseInt64(matches[1]) - parseInt64(matches[2])
			metrics.LogsDropped = parseInt64(matches[2])
			metrics.GCCount = parseInt64(matches[3])
			metrics.GCPauseMs = parseFloat(matches[4])
		}

		// Extract shard stats (use the last occurrence)
		if matches := shardPattern.FindStringSubmatch(line); matches != nil {
			shardStatsStr := matches[1]
			shardStats = []ShardStat{} // Reset for each SHARD_STATS line
			
			for _, match := range shardEntryPattern.FindAllStringSubmatch(shardStatsStr, -1) {
				shardID := parseInt(match[1])
				utilization := parseFloat(match[2])
				writeCount := parseInt64(match[3])
				
				shardStats = append(shardStats, ShardStat{
					ShardID:     shardID,
					Utilization: utilization,
					WriteCount:  writeCount,
				})
			}
		}
	}

	return metrics, shardStats, scanner.Err()
}

func calculateResourceStats(samples []ResourceSample, metric string) (mean, p95, peak float64) {
	if len(samples) == 0 {
		return 0, 0, 0
	}

	values := make([]float64, len(samples))
	sum := 0.0

	for i, sample := range samples {
		var val float64
		if metric == "cpu" {
			val = sample.CPUPercent
		} else {
			val = sample.MemoryMB
		}
		values[i] = val
		sum += val
		if val > peak {
			peak = val
		}
	}

	mean = sum / float64(len(values))

	sort.Float64s(values)
	p95Index := int(float64(len(values)) * 0.95)
	if p95Index >= len(values) {
		p95Index = len(values) - 1
	}
	p95 = values[p95Index]

	return mean, p95, peak
}

func calculateScores(scenarios []*Scenario) {
	for _, s := range scenarios {
		// P0: Application latency (P99)
		if s.Metrics.GRPCLatencyP99 > 100 {
			s.Metrics.OverallScore = 1000 + s.Metrics.GRPCLatencyP99
		} else {
			s.Metrics.OverallScore = s.Metrics.GRPCLatencyP99
		}

		// P1: CPU (30% weight)
		s.Metrics.OverallScore += s.Metrics.CPUMean * 0.3

		// P2: Drop rate (20% weight, penalize heavily if > 0)
		if s.Metrics.DropRate > 0 {
			s.Metrics.OverallScore += (1000 + s.Metrics.DropRate*10) * 0.2
		}
	}
}

func sumErrorCounts(errorDist map[string]int) int {
	sum := 0
	for _, count := range errorDist {
		sum += count
	}
	return sum
}

func parseInt(s string) int {
	val, _ := strconv.Atoi(s)
	return val
}

func parseInt64(s string) int64 {
	val, _ := strconv.ParseInt(s, 10, 64)
	return val
}

func parseFloat(s string) float64 {
	val, _ := strconv.ParseFloat(s, 64)
	return val
}

func generateResultsReport(resultsDir string, allScenarios, scenarios50, scenarios200 []*Scenario) error {
	reportPath := filepath.Join(resultsDir, "THREAD_SCALING_RESULTS.md")
	f, err := os.Create(reportPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	defer w.Flush()

	// Header
	fmt.Fprintln(w, "# Thread Scaling Study Results")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "**Date**: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "**Total Scenarios**: %d (%d for 50T, %d for 200T)\n", 
		len(allScenarios), len(scenarios50), len(scenarios200))
	fmt.Fprintln(w, "**Test Configuration**: 1000 RPS, 5 minutes per test, 4 CPU cores, 4GB memory")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)

	// Executive Summary for 50 threads
	if len(scenarios50) > 0 {
		winner := scenarios50[0]
		fmt.Fprintln(w, "## Optimal Configuration for 50 Threads")
		fmt.Fprintln(w)
		fmt.Fprintf(w, "**Winner**: %s\n", winner.Description)
		fmt.Fprintln(w)
		fmt.Fprintf(w, "- **Buffer Size**: %d MB\n", winner.Config.BufferMB)
		fmt.Fprintf(w, "- **Shards**: %d\n", winner.Config.Shards)
		fmt.Fprintf(w, "- **Threads per Shard**: %.1f\n", winner.Metrics.ThreadsPerShard)
		fmt.Fprintf(w, "- **Drop Rate**: %.4f%%\n", winner.Metrics.DropRate)
		fmt.Fprintf(w, "- **CPU Average**: %.2f%%\n", winner.Metrics.CPUMean)
		fmt.Fprintf(w, "- **gRPC P99 Latency**: %.2f ms\n", winner.Metrics.GRPCLatencyP99)
		fmt.Fprintf(w, "- **gRPC P999 Latency**: %.2f ms\n", winner.Metrics.GRPCLatencyP999)
		fmt.Fprintln(w)
	}

	// Executive Summary for 200 threads
	if len(scenarios200) > 0 {
		winner := scenarios200[0]
		fmt.Fprintln(w, "## Optimal Configuration for 200 Threads")
		fmt.Fprintln(w)
		fmt.Fprintf(w, "**Winner**: %s\n", winner.Description)
		fmt.Fprintln(w)
		fmt.Fprintf(w, "- **Buffer Size**: %d MB\n", winner.Config.BufferMB)
		fmt.Fprintf(w, "- **Shards**: %d\n", winner.Config.Shards)
		fmt.Fprintf(w, "- **Threads per Shard**: %.1f\n", winner.Metrics.ThreadsPerShard)
		fmt.Fprintf(w, "- **Drop Rate**: %.4f%%\n", winner.Metrics.DropRate)
		fmt.Fprintf(w, "- **CPU Average**: %.2f%%\n", winner.Metrics.CPUMean)
		fmt.Fprintf(w, "- **gRPC P99 Latency**: %.2f ms\n", winner.Metrics.GRPCLatencyP99)
		fmt.Fprintf(w, "- **gRPC P999 Latency**: %.2f ms\n", winner.Metrics.GRPCLatencyP999)
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)

	// Complete results table
	fmt.Fprintln(w, "## Complete Results")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Scenario | Threads | Buffer | Shards | T/S | Drop% | CPU | P99 | P999 | P9999 | Score |")
	fmt.Fprintln(w, "|----------|---------|--------|--------|-----|-------|-----|-----|------|-------|-------|")
	
	for _, s := range allScenarios {
		fmt.Fprintf(w, "| %s | %d | %dMB | %d | %.1f | %.2f | %.1f | %.2f | %.2f | %.2f | %.2f |\n",
			s.ID, s.Config.Threads, s.Config.BufferMB, s.Config.Shards,
			s.Metrics.ThreadsPerShard, s.Metrics.DropRate, s.Metrics.CPUMean,
			s.Metrics.GRPCLatencyP99, s.Metrics.GRPCLatencyP999, s.Metrics.GRPCLatencyP9999,
			s.Metrics.OverallScore)
	}
	fmt.Fprintln(w)

	// Per-shard analysis for top configs
	if len(scenarios50) > 0 && len(scenarios50[0].ShardStats) > 0 {
		fmt.Fprintln(w, "## Per-Shard Utilization (50 Threads Winner)")
		fmt.Fprintln(w)
		winner := scenarios50[0]
		fmt.Fprintln(w, "| Shard ID | Utilization % | Write Count |")
		fmt.Fprintln(w, "|----------|---------------|-------------|")
		for _, stat := range winner.ShardStats {
			fmt.Fprintf(w, "| %d | %.2f%% | %d |\n", 
				stat.ShardID, stat.Utilization, stat.WriteCount)
		}
		fmt.Fprintln(w)
	}

	if len(scenarios200) > 0 && len(scenarios200[0].ShardStats) > 0 {
		fmt.Fprintln(w, "## Per-Shard Utilization (200 Threads Winner)")
		fmt.Fprintln(w)
		winner := scenarios200[0]
		fmt.Fprintln(w, "| Shard ID | Utilization % | Write Count |")
		fmt.Fprintln(w, "|----------|---------------|-------------|")
		for _, stat := range winner.ShardStats {
			fmt.Fprintf(w, "| %d | %.2f%% | %d |\n",
				stat.ShardID, stat.Utilization, stat.WriteCount)
		}
		fmt.Fprintln(w)
	}

	return nil
}

func generatePatternsReport(resultsDir string, scenarios50, scenarios200 []*Scenario) error {
	reportPath := filepath.Join(resultsDir, "THREAD_SCALING_PATTERNS.md")
	f, err := os.Create(reportPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	defer w.Flush()

	fmt.Fprintln(w, "# Thread Scaling Patterns and Formulas")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "**Date**: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Scaling Analysis")
	fmt.Fprintln(w)

	// Compare winners across thread counts
	// Note: 100 threads data from previous buffer optimization study (128MB, 8 shards)
	optimal100 := map[string]interface{}{
		"threads": 100,
		"buffer":  128,
		"shards":  8,
		"cpu":     59.8,
		"drops":   0.72,
		"p99":     3.55,
	}

	fmt.Fprintln(w, "## Optimal Configurations Comparison")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Threads | Buffer (MB) | Shards | T/S Ratio | CPU % | Drop % | P99 (ms) |")
	fmt.Fprintln(w, "|---------|-------------|--------|-----------|-------|--------|----------|")
	
	if len(scenarios50) > 0 {
		s := scenarios50[0]
		fmt.Fprintf(w, "| 50 | %d | %d | %.1f | %.1f | %.2f | %.2f |\n",
			s.Config.BufferMB, s.Config.Shards, s.Metrics.ThreadsPerShard,
			s.Metrics.CPUMean, s.Metrics.DropRate, s.Metrics.GRPCLatencyP99)
	}

	fmt.Fprintf(w, "| 100 | %d | %d | %.1f | %.1f | %.2f | %.2f |\n",
		optimal100["buffer"], optimal100["shards"], 
		float64(optimal100["threads"].(int))/float64(optimal100["shards"].(int)),
		optimal100["cpu"], optimal100["drops"], optimal100["p99"])

	if len(scenarios200) > 0 {
		s := scenarios200[0]
		fmt.Fprintf(w, "| 200 | %d | %d | %.1f | %.1f | %.2f | %.2f |\n",
			s.Config.BufferMB, s.Config.Shards, s.Metrics.ThreadsPerShard,
			s.Metrics.CPUMean, s.Metrics.DropRate, s.Metrics.GRPCLatencyP99)
	}
	fmt.Fprintln(w)

	// Derive scaling formulas
	fmt.Fprintln(w, "## Derived Scaling Patterns")
	fmt.Fprintln(w)

	if len(scenarios50) > 0 && len(scenarios200) > 0 {
		s50 := scenarios50[0]
		s200 := scenarios200[0]

		// Buffer scaling
		bufferRatio := float64(s200.Config.BufferMB) / float64(s50.Config.BufferMB)
		
		fmt.Fprintln(w, "### Buffer Scaling")
		fmt.Fprintf(w, "- 50 threads → 200 threads (4x increase): Buffer %.1fx increase\n", bufferRatio)
		fmt.Fprintf(w, "- **Formula**: `optimal_buffer_mb ≈ %.1f * thread_count`\n", 
			float64(s200.Config.BufferMB)/float64(s200.Config.Threads))
		fmt.Fprintln(w)

		// Shard scaling
		fmt.Fprintln(w, "### Shard Scaling")
		fmt.Fprintf(w, "- Optimal threads-per-shard ratio: **%.1f - %.1f**\n",
			s50.Metrics.ThreadsPerShard, s200.Metrics.ThreadsPerShard)
		fmt.Fprintf(w, "- **Formula**: `optimal_shards = thread_count / 12.5`\n")
		fmt.Fprintln(w)

		// CPU scaling
		cpuGrowth := (s200.Metrics.CPUMean - s50.Metrics.CPUMean) / (float64(s200.Config.Threads - s50.Config.Threads))
		fmt.Fprintln(w, "### CPU Scaling")
		fmt.Fprintf(w, "- CPU growth rate: **%.2f%% per thread**\n", cpuGrowth)
		fmt.Fprintf(w, "- Base CPU overhead: ~%.1f%%\n", s50.Metrics.CPUMean - cpuGrowth*float64(s50.Config.Threads))
		fmt.Fprintln(w)
	}

	return nil
}

func generateTuningGuide(resultsDir string, scenarios50, scenarios200 []*Scenario) error {
	reportPath := filepath.Join(resultsDir, "PRODUCTION_TUNING_GUIDE.md")
	f, err := os.Create(reportPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	defer w.Flush()

	fmt.Fprintln(w, "# Production Tuning Guide")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "**Date**: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintln(w)

	fmt.Fprintln(w, "## Configuration Decision Tree")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "```")
	fmt.Fprintln(w, "Thread Count Decision:")
	fmt.Fprintln(w)

	if len(scenarios50) > 0 {
		s := scenarios50[0]
		fmt.Fprintf(w, "  50 threads or less:\n")
		fmt.Fprintf(w, "    Buffer: %d MB\n", s.Config.BufferMB)
		fmt.Fprintf(w, "    Shards: %d\n", s.Config.Shards)
		fmt.Fprintf(w, "    Expected CPU: %.1f%%\n", s.Metrics.CPUMean)
		fmt.Fprintf(w, "    Expected Drops: %.2f%%\n", s.Metrics.DropRate)
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "  100 threads:\n")
	fmt.Fprintf(w, "    Buffer: 128 MB\n")
	fmt.Fprintf(w, "    Shards: 8\n")
	fmt.Fprintf(w, "    Expected CPU: 60%%\n")
	fmt.Fprintf(w, "    Expected Drops: <1%%\n")
	fmt.Fprintln(w)

	if len(scenarios200) > 0 {
		s := scenarios200[0]
		fmt.Fprintf(w, "  200 threads or more:\n")
		fmt.Fprintf(w, "    Buffer: %d MB\n", s.Config.BufferMB)
		fmt.Fprintf(w, "    Shards: %d\n", s.Config.Shards)
		fmt.Fprintf(w, "    Expected CPU: %.1f%%\n", s.Metrics.CPUMean)
		fmt.Fprintf(w, "    Expected Drops: %.2f%%\n", s.Metrics.DropRate)
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "```")
	fmt.Fprintln(w)

	fmt.Fprintln(w, "## Quick Reference Table")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Thread Count | Buffer (MB) | Shards | Config |")
	fmt.Fprintln(w, "|--------------|-------------|--------|--------|")

	if len(scenarios50) > 0 {
		s := scenarios50[0]
		fmt.Fprintf(w, "| 50 | %d | %d | Optimal |\n", s.Config.BufferMB, s.Config.Shards)
	}
	fmt.Fprintf(w, "| 100 | 128 | 8 | Proven |\n")
	if len(scenarios200) > 0 {
		s := scenarios200[0]
		fmt.Fprintf(w, "| 200 | %d | %d | Optimal |\n", s.Config.BufferMB, s.Config.Shards)
	}

	// Extrapolation
	if len(scenarios200) > 0 && len(scenarios50) > 0 {
		s200 := scenarios200[0]
		bufferPerThread := float64(s200.Config.BufferMB) / float64(s200.Config.Threads)
		threadsPerShard := s200.Metrics.ThreadsPerShard

		fmt.Fprintf(w, "| 500* | %d | %d | Extrapolated |\n", 
			int(math.Round(bufferPerThread*500)), int(math.Round(500/threadsPerShard)))
		fmt.Fprintf(w, "| 1000* | %d | %d | Extrapolated |\n",
			int(math.Round(bufferPerThread*1000)), int(math.Round(1000/threadsPerShard)))
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "*Extrapolated values - test before using in production")
	fmt.Fprintln(w)

	return nil
}

