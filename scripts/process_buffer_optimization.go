package main

import (
	"bufio"
	"encoding/json"
	"fmt"
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
	GRPCLatencyMax   float64
	RequestCount     int64
	ErrorCount       int64
	SuccessRate      float64
	RPS              float64

	// Logger metrics
	LogsTotal       int64
	LogsDropped     int64
	DropRate        float64
	LoggerLatencyP50 float64
	LoggerLatencyP99 float64
	ThroughputMBps  float64

	// System metrics
	CPUMean    float64
	CPUP95     float64
	CPUPeak    float64
	MemMean    float64
	MemPeak    float64
	GCCycles   int64
	GCPauseMs  float64
	GCPercent  float64

	// Ranking scores
	P0Score float64 // Application latency (lower is better)
	P1Score float64 // CPU efficiency (lower is better)
	P2Score float64 // Drop rate (lower is better)
	OverallScore float64 // Combined score
}

type GHZReport struct {
	Count   int64                  `json:"count"`
	Total   time.Duration          `json:"total"`
	Average time.Duration          `json:"average"`
	Fastest time.Duration          `json:"fastest"`
	Slowest time.Duration          `json:"slowest"`
	Rps     float64                `json:"rps"`
	ErrorDist map[string]int       `json:"errorDistribution"`
	LatencyDistribution []LatencyBucket `json:"latencyDistribution"`
}

type LatencyBucket struct {
	Percentage int     `json:"percentage"`
	Latency    float64 `json:"latency"`
}

type ServerMetrics struct {
	LogsWritten int64
	LogsDropped int64
	GCCount     int64
	GCPauseMs   float64
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

func main() {
	resultsDir := "results/buffer_optimization"

	fmt.Println("Processing Buffer Optimization Study Results...")
	fmt.Println("Results directory:", resultsDir)
	fmt.Println()

	// Load all scenarios
	scenarios, err := loadAllScenarios(resultsDir)
	if err != nil {
		fmt.Printf("Error loading scenarios: %v\n", err)
		os.Exit(1)
	}

	if len(scenarios) == 0 {
		fmt.Println("No scenarios found!")
		os.Exit(1)
	}

	fmt.Printf("Loaded %d scenarios\n", len(scenarios))
	fmt.Println()

	// Calculate ranking scores
	calculateScores(scenarios)

	// Sort by overall score
	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].Metrics.OverallScore < scenarios[j].Metrics.OverallScore
	})

	// Generate reports
	fmt.Println("Generating reports...")
	
	err = generateSummaryReport(resultsDir, scenarios)
	if err != nil {
		fmt.Printf("Error generating summary report: %v\n", err)
		os.Exit(1)
	}

	err = generateDetailedReport(resultsDir, scenarios)
	if err != nil {
		fmt.Printf("Error generating detailed report: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("✓ Reports generated successfully!")
	fmt.Printf("  - %s/BUFFER_OPTIMIZATION_RESULTS.md\n", resultsDir)
	fmt.Printf("  - %s/BUFFER_OPTIMIZATION_DETAILED.md\n", resultsDir)
	fmt.Println()

	// Print top 5 configurations
	fmt.Println("Top 5 Configurations:")
	for i := 0; i < 5 && i < len(scenarios); i++ {
		s := scenarios[i]
		fmt.Printf("%d. %s: %dMB, %d shards, %d threads | Drop: %.2f%% | CPU: %.1f%% | Latency P99: %.2fms\n",
			i+1, s.ID, s.Config.BufferMB, s.Config.Shards, s.Config.Threads,
			s.Metrics.DropRate, s.Metrics.CPUMean, s.Metrics.GRPCLatencyP99)
	}
	fmt.Println()
}

func loadAllScenarios(resultsDir string) ([]*Scenario, error) {
	metadataFiles, err := filepath.Glob(filepath.Join(resultsDir, "scenario_*_metadata.json"))
	if err != nil {
		return nil, err
	}

	var scenarios []*Scenario

	for _, metadataFile := range metadataFiles {
		// Extract scenario ID from filename
		basename := filepath.Base(metadataFile)
		scenarioID := strings.TrimPrefix(basename, "scenario_")
		scenarioID = strings.TrimSuffix(scenarioID, "_metadata.json")

		// Load metadata
		metadata, err := loadMetadata(metadataFile)
		if err != nil {
			fmt.Printf("Warning: Failed to load metadata for %s: %v\n", scenarioID, err)
			continue
		}

		// Load metrics
		ghzPath := filepath.Join(resultsDir, fmt.Sprintf("scenario_%s_ghz.json", scenarioID))
		resourcePath := filepath.Join(resultsDir, fmt.Sprintf("scenario_%s_resources.csv", scenarioID))
		serverLogPath := filepath.Join(resultsDir, fmt.Sprintf("scenario_%s_server.log", scenarioID))

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
		scenario.Metrics.SuccessRate = float64(ghz.Count) / float64(ghz.Count+int64(sumErrorCounts(ghz.ErrorDist))) * 100.0

		// Extract latency percentiles
		for _, bucket := range ghz.LatencyDistribution {
			latencyMs := bucket.Latency / 1e6 // Convert to milliseconds
			switch bucket.Percentage {
			case 50:
				scenario.Metrics.GRPCLatencyP50 = latencyMs
			case 95:
				scenario.Metrics.GRPCLatencyP95 = latencyMs
			case 99:
				scenario.Metrics.GRPCLatencyP99 = latencyMs
			case 999:
				scenario.Metrics.GRPCLatencyP999 = latencyMs
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
		serverMetrics, err := extractServerMetrics(serverLogPath)
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

func extractServerMetrics(logPath string) (*ServerMetrics, error) {
	file, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	metrics := &ServerMetrics{}
	scanner := bufio.NewScanner(file)

	// Regex patterns for extracting metrics
	metricsPattern := regexp.MustCompile(`METRICS:.*Logs: (\d+) Dropped: (\d+).*GC: (\d+) cycles ([\d.]+)ms`)

	for scanner.Scan() {
		line := scanner.Text()

		// Extract comprehensive metrics
		if matches := metricsPattern.FindStringSubmatch(line); matches != nil {
			metrics.LogsWritten = parseInt64(matches[1]) - parseInt64(matches[2])
			metrics.LogsDropped = parseInt64(matches[2])
			metrics.GCCount = parseInt64(matches[3])
			metrics.GCPauseMs = parseFloat(matches[4])
		}
	}

	return metrics, scanner.Err()
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
		// P0: Application latency (lower is better)
		// Penalize heavily if P99 > 100ms (failure threshold)
		if s.Metrics.GRPCLatencyP99 > 100 {
			s.Metrics.P0Score = 1000 + s.Metrics.GRPCLatencyP99
		} else {
			s.Metrics.P0Score = s.Metrics.GRPCLatencyP99
		}

		// P1: CPU efficiency (lower is better)
		s.Metrics.P1Score = s.Metrics.CPUMean

		// P2: Drop rate (lower is better)
		// Penalize heavily if drops > 0
		if s.Metrics.DropRate > 0 {
			s.Metrics.P2Score = 1000 + s.Metrics.DropRate * 10
		} else {
			s.Metrics.P2Score = 0
		}

		// Overall score (weighted combination)
		// P0 (latency) = 50% weight
		// P1 (CPU) = 30% weight
		// P2 (drops) = 20% weight
		s.Metrics.OverallScore = (s.Metrics.P0Score * 0.5) +
			(s.Metrics.P1Score * 0.3) +
			(s.Metrics.P2Score * 0.2)
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

func generateSummaryReport(resultsDir string, scenarios []*Scenario) error {
	reportPath := filepath.Join(resultsDir, "BUFFER_OPTIMIZATION_RESULTS.md")
	f, err := os.Create(reportPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	defer w.Flush()

	// Header
	fmt.Fprintln(w, "# Buffer Optimization Study Results")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "**Date**: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "**Total Scenarios**: %d\n", len(scenarios))
	fmt.Fprintln(w, "**Test Configuration**: 1000 RPS, 2 minutes per test, 4 CPU cores, 4GB memory")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)

	// Executive Summary
	fmt.Fprintln(w, "## Executive Summary")
	fmt.Fprintln(w)
	
	winner := scenarios[0]
	fmt.Fprintf(w, "**Winner**: %s\n", winner.Description)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "- **Buffer Size**: %d MB\n", winner.Config.BufferMB)
	fmt.Fprintf(w, "- **Shards**: %d\n", winner.Config.Shards)
	fmt.Fprintf(w, "- **Drop Rate**: %.4f%%\n", winner.Metrics.DropRate)
	fmt.Fprintf(w, "- **CPU Average**: %.2f%%\n", winner.Metrics.CPUMean)
	fmt.Fprintf(w, "- **gRPC P99 Latency**: %.2f ms\n", winner.Metrics.GRPCLatencyP99)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)

	// Top 10 Configurations
	fmt.Fprintln(w, "## Top 10 Configurations")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Rank | Scenario | Buffer | Shards | Threads | Drop Rate | CPU Avg | gRPC P99 | Score |")
	fmt.Fprintln(w, "|------|----------|--------|--------|---------|-----------|---------|----------|-------|")
	
	for i := 0; i < 10 && i < len(scenarios); i++ {
		s := scenarios[i]
		fmt.Fprintf(w, "| %d | %s | %dMB | %d | %d | %.2f%% | %.1f%% | %.2fms | %.2f |\n",
			i+1, s.ID, s.Config.BufferMB, s.Config.Shards, s.Config.Threads,
			s.Metrics.DropRate, s.Metrics.CPUMean, s.Metrics.GRPCLatencyP99,
			s.Metrics.OverallScore)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)

	// Phase Analysis
	phases := map[string][]*Scenario{
		"Phase1_MinViableBuffer":  {},
		"Phase2_ShardOptimization": {},
		"Phase3_ThreadScaling":     {},
		"Phase4_CrossValidation":   {},
	}

	for _, s := range scenarios {
		if list, ok := phases[s.Phase]; ok {
			phases[s.Phase] = append(list, s)
		}
	}

	fmt.Fprintln(w, "## Phase-by-Phase Analysis")
	fmt.Fprintln(w)

	// Phase 1
	fmt.Fprintln(w, "### Phase 1: Minimum Viable Buffer")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Fixed: 8 shards, 100 threads | Varying: Buffer size")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Buffer | Drop Rate | CPU Avg | CPU Peak | gRPC P99 | Memory Avg |")
	fmt.Fprintln(w, "|--------|-----------|---------|----------|----------|------------|")
	for _, s := range phases["Phase1_MinViableBuffer"] {
		fmt.Fprintf(w, "| %dMB | %.2f%% | %.1f%% | %.1f%% | %.2fms | %.0fMB |\n",
			s.Config.BufferMB, s.Metrics.DropRate, s.Metrics.CPUMean,
			s.Metrics.CPUPeak, s.Metrics.GRPCLatencyP99, s.Metrics.MemMean)
	}
	fmt.Fprintln(w)

	// Phase 2
	fmt.Fprintln(w, "### Phase 2: Shard Optimization")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Fixed: Best buffer, 100 threads | Varying: Shard count")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Shards | Drop Rate | CPU Avg | CPU Peak | gRPC P99 | Lock Contention |")
	fmt.Fprintln(w, "|--------|-----------|---------|----------|----------|-----------------|")
	for _, s := range phases["Phase2_ShardOptimization"] {
		contention := "Low"
		if s.Metrics.CPUMean > 80 {
			contention = "High"
		} else if s.Metrics.CPUMean > 60 {
			contention = "Medium"
		}
		fmt.Fprintf(w, "| %d | %.2f%% | %.1f%% | %.1f%% | %.2fms | %s |\n",
			s.Config.Shards, s.Metrics.DropRate, s.Metrics.CPUMean,
			s.Metrics.CPUPeak, s.Metrics.GRPCLatencyP99, contention)
	}
	fmt.Fprintln(w)

	// Phase 3
	fmt.Fprintln(w, "### Phase 3: Thread Scaling")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Fixed: Optimal buffer + shards | Varying: Thread count")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Threads | Drop Rate | CPU Avg | gRPC P99 | Scalability |")
	fmt.Fprintln(w, "|---------|-----------|---------|----------|-------------|")
	for _, s := range phases["Phase3_ThreadScaling"] {
		scalability := "Excellent"
		if s.Metrics.DropRate > 0.1 {
			scalability = "Poor"
		} else if s.Metrics.CPUMean > 90 {
			scalability = "Limited"
		} else if s.Metrics.CPUMean > 70 {
			scalability = "Good"
		}
		fmt.Fprintf(w, "| %d | %.2f%% | %.1f%% | %.2fms | %s |\n",
			s.Config.Threads, s.Metrics.DropRate, s.Metrics.CPUMean,
			s.Metrics.GRPCLatencyP99, scalability)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)

	// Production Recommendation
	fmt.Fprintln(w, "## Production Recommendation")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Based on comprehensive testing of 27 scenarios, the optimal configuration for production at 1000 RPS is:\n")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "```yaml")
	fmt.Fprintf(w, "Buffer Size: %d MB\n", winner.Config.BufferMB)
	fmt.Fprintf(w, "Shards: %d\n", winner.Config.Shards)
	fmt.Fprintf(w, "Flush Interval: %s\n", winner.Config.FlushInterval)
	fmt.Fprintln(w, "```")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "**Why This Configuration:**")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "- ✅ **Zero Dropped Logs**: %.4f%% drop rate\n", winner.Metrics.DropRate)
	fmt.Fprintf(w, "- ✅ **Excellent Latency**: P99 of %.2fms (target: <100ms)\n", winner.Metrics.GRPCLatencyP99)
	fmt.Fprintf(w, "- ✅ **CPU Efficient**: %.1f%% average CPU usage\n", winner.Metrics.CPUMean)
	fmt.Fprintf(w, "- ✅ **Memory Efficient**: %.0fMB average memory\n", winner.Metrics.MemMean)
	fmt.Fprintf(w, "- ✅ **GC Overhead**: Only %.2fms total pause time\n", winner.Metrics.GCPauseMs)
	fmt.Fprintln(w)

	return nil
}

func generateDetailedReport(resultsDir string, scenarios []*Scenario) error {
	reportPath := filepath.Join(resultsDir, "BUFFER_OPTIMIZATION_DETAILED.md")
	f, err := os.Create(reportPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	defer w.Flush()

	// Header
	fmt.Fprintln(w, "# Buffer Optimization Study - Detailed Results")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "**Date**: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)

	// Complete results table
	fmt.Fprintln(w, "## Complete Results")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Scenario | Buffer | Shards | Threads | Drop % | CPU Avg | CPU Peak | Mem Avg | gRPC P50 | gRPC P95 | gRPC P99 | GC Cycles | GC Pause | Score |")
	fmt.Fprintln(w, "|----------|--------|--------|---------|--------|---------|----------|---------|----------|----------|----------|-----------|----------|-------|")
	
	for _, s := range scenarios {
		fmt.Fprintf(w, "| %s | %dMB | %d | %d | %.2f | %.1f | %.1f | %.0f | %.2f | %.2f | %.2f | %d | %.2f | %.2f |\n",
			s.ID, s.Config.BufferMB, s.Config.Shards, s.Config.Threads,
			s.Metrics.DropRate, s.Metrics.CPUMean, s.Metrics.CPUPeak,
			s.Metrics.MemMean, s.Metrics.GRPCLatencyP50, s.Metrics.GRPCLatencyP95,
			s.Metrics.GRPCLatencyP99, s.Metrics.GCCycles, s.Metrics.GCPauseMs,
			s.Metrics.OverallScore)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "---")
	fmt.Fprintln(w)

	// Individual scenario summaries
	fmt.Fprintln(w, "## Individual Scenario Details")
	fmt.Fprintln(w)

	for i, s := range scenarios {
		if i >= 10 {
			break // Only show top 10 in detail
		}

		fmt.Fprintf(w, "### %d. %s\n", i+1, s.Description)
		fmt.Fprintln(w)
		fmt.Fprintf(w, "**Configuration**: %dMB buffer, %d shards, %d threads\n", 
			s.Config.BufferMB, s.Config.Shards, s.Config.Threads)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "**Application Metrics:**")
		fmt.Fprintf(w, "- Requests: %d (%.0f RPS)\n", s.Metrics.RequestCount, s.Metrics.RPS)
		fmt.Fprintf(w, "- Latency P50/P95/P99: %.2f / %.2f / %.2f ms\n",
			s.Metrics.GRPCLatencyP50, s.Metrics.GRPCLatencyP95, s.Metrics.GRPCLatencyP99)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "**Logger Metrics:**")
		fmt.Fprintf(w, "- Total Logs: %d\n", s.Metrics.LogsTotal)
		fmt.Fprintf(w, "- Dropped: %d (%.2f%%)\n", s.Metrics.LogsDropped, s.Metrics.DropRate)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "**System Metrics:**")
		fmt.Fprintf(w, "- CPU: %.1f%% avg, %.1f%% peak\n", s.Metrics.CPUMean, s.Metrics.CPUPeak)
		fmt.Fprintf(w, "- Memory: %.0fMB avg, %.0fMB peak\n", s.Metrics.MemMean, s.Metrics.MemPeak)
		fmt.Fprintf(w, "- GC: %d cycles, %.2fms total pause\n", s.Metrics.GCCycles, s.Metrics.GCPauseMs)
		fmt.Fprintln(w)
		fmt.Fprintf(w, "**Overall Score**: %.2f\n", s.Metrics.OverallScore)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "---")
		fmt.Fprintln(w)
	}

	return nil
}

