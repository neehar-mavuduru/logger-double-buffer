package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GHZReport represents ghz JSON output
type GHZReport struct {
	Count         int64         `json:"count"`
	Total         time.Duration `json:"total"`
	Average       time.Duration `json:"average"`
	Fastest       time.Duration `json:"fastest"`
	Slowest       time.Duration `json:"slowest"`
	Rps           float64       `json:"rps"`
	ErrorDist     map[string]int `json:"errorDistribution"`
	StatusCodeDist map[string]int `json:"statusCodeDistribution"`
	LatencyDistribution []LatencyBucket `json:"latencyDistribution"`
	Histogram []HistogramBucket `json:"histogram"`
}

type LatencyBucket struct {
	Percentage int     `json:"percentage"`
	Latency    float64 `json:"latency"`
}

type HistogramBucket struct {
	Mark      float64 `json:"mark"`
	Count     int     `json:"count"`
	Frequency float64 `json:"frequency"`
}

// ResourceSample represents a single resource measurement
type ResourceSample struct {
	Timestamp   int
	CPUPercent  float64
	MemoryMB    float64
	MemPercent  float64
	NetRxMB     float64
	NetTxMB     float64
	BlockReadMB float64
	BlockWriteMB float64
}

// ServerMetrics extracted from server logs
type ServerMetrics struct {
	TotalRequests int64
	DropRate      float64
	LogsWritten   int64
	LogsDropped   int64
	GCCount       int64
	GCPauseMs     float64
	LatencyP50    float64
	LatencyP95    float64
	LatencyP99    float64
}

func main() {
	ghzReport := flag.String("ghz-report", "", "Path to ghz JSON report")
	resourceFile := flag.String("resource-file", "", "Path to resource CSV file")
	serverLogs := flag.String("server-logs", "", "Path to server logs")
	output := flag.String("output", "", "Output markdown file")
	scenario := flag.String("scenario", "unknown", "Test scenario name")
	flag.Parse()

	if *ghzReport == "" || *resourceFile == "" || *output == "" {
		fmt.Println("Error: Missing required arguments")
		flag.PrintDefaults()
		os.Exit(1)
	}

	fmt.Println("Processing production load test results...")

	// Load ghz report
	ghz, err := loadGHZReport(*ghzReport)
	if err != nil {
		fmt.Printf("Error loading ghz report: %v\n", err)
		os.Exit(1)
	}

	// Load resource data
	resources, err := loadResourceData(*resourceFile)
	if err != nil {
		fmt.Printf("Error loading resource data: %v\n", err)
		os.Exit(1)
	}

	// Extract server metrics from logs
	serverMetrics, err := extractServerMetrics(*serverLogs)
	if err != nil {
		fmt.Printf("Warning: Could not extract server metrics: %v\n", err)
		serverMetrics = &ServerMetrics{} // Use empty metrics
	}

	// Generate report
	err = generateReport(*output, *scenario, ghz, resources, serverMetrics)
	if err != nil {
		fmt.Printf("Error generating report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Report generated: %s\n", *output)
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
		if len(parts) < 8 {
			continue
		}

		sample := ResourceSample{
			Timestamp:    parseInt(parts[0]),
			CPUPercent:   parseFloat(parts[1]),
			MemoryMB:     parseFloat(parts[2]),
			MemPercent:   parseFloat(parts[3]),
			NetRxMB:      parseFloat(parts[4]),
			NetTxMB:      parseFloat(parts[5]),
			BlockReadMB:  parseFloat(parts[6]),
			BlockWriteMB: parseFloat(parts[7]),
		}
		samples = append(samples, sample)
	}

	return samples, scanner.Err()
}

func extractServerMetrics(path string) (*ServerMetrics, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)
	metrics := &ServerMetrics{}

	// Extract metrics using regex patterns
	if match := regexp.MustCompile(`Latency P50: ([\d.]+)us`).FindStringSubmatch(content); len(match) > 1 {
		metrics.LatencyP50 = parseFloat(match[1])
	}
	if match := regexp.MustCompile(`Latency P95: ([\d.]+)us`).FindStringSubmatch(content); len(match) > 1 {
		metrics.LatencyP95 = parseFloat(match[1])
	}
	if match := regexp.MustCompile(`Latency P99: ([\d.]+)us`).FindStringSubmatch(content); len(match) > 1 {
		metrics.LatencyP99 = parseFloat(match[1])
	}
	if match := regexp.MustCompile(`Total Requests: (\d+)`).FindStringSubmatch(content); len(match) > 1 {
		metrics.TotalRequests = int64(parseInt(match[1]))
	}
	if match := regexp.MustCompile(`Drop Rate: ([\d.]+)%`).FindStringSubmatch(content); len(match) > 1 {
		metrics.DropRate = parseFloat(match[1])
	}

	return metrics, nil
}

func generateReport(outputPath, scenario string, ghz *GHZReport, resources []ResourceSample, serverMetrics *ServerMetrics) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	w := bufio.NewWriter(file)
	defer w.Flush()

	// Header
	fmt.Fprintf(w, "# Production Load Test Results\n\n")
	fmt.Fprintf(w, "**Generated:** %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "**Scenario:** %s\n", scenario)
	fmt.Fprintf(w, "**Duration:** %s\n", ghz.Total.String())
	fmt.Fprintf(w, "\n---\n\n")

	// Executive Summary
	fmt.Fprintf(w, "## 📊 Executive Summary\n\n")
	fmt.Fprintf(w, "| Metric | Value | Target | Status |\n")
	fmt.Fprintf(w, "|--------|-------|--------|--------|\n")
	fmt.Fprintf(w, "| **RPS Achieved** | %.0f | N/A | %s |\n", ghz.Rps, checkmark(true))
	fmt.Fprintf(w, "| **Total Requests** | %d | N/A | %s |\n", ghz.Count, checkmark(true))
	fmt.Fprintf(w, "| **Error Rate** | %.2f%% | <1%% | %s |\n", 
		calculateErrorRate(ghz), checkmark(calculateErrorRate(ghz) < 1.0))
	fmt.Fprintf(w, "| **Avg Latency** | %s | <50ms | %s |\n", 
		ghz.Average, checkmark(ghz.Average < 50*time.Millisecond))
	fmt.Fprintf(w, "| **P99 Latency** | %s | <100ms | %s |\n", 
		getP99Latency(ghz), checkmark(getP99LatencyMs(ghz) < 100))
	fmt.Fprintf(w, "| **Drop Rate** | %.2f%% | <1%% | %s |\n", 
		serverMetrics.DropRate, checkmark(serverMetrics.DropRate < 1.0))
	
	// Resource utilization
	avgCPU, peakCPU := calculateCPUStats(resources)
	avgMem, peakMem := calculateMemStats(resources)
	fmt.Fprintf(w, "| **Avg CPU** | %.1f%% | <400%% | %s |\n", avgCPU, checkmark(avgCPU < 400))
	fmt.Fprintf(w, "| **Peak CPU** | %.1f%% | <400%% | %s |\n", peakCPU, checkmark(peakCPU < 400))
	fmt.Fprintf(w, "| **Avg Memory** | %.0f MB | <2GB | %s |\n", avgMem, checkmark(avgMem < 2048))
	fmt.Fprintf(w, "| **Peak Memory** | %.0f MB | <2GB | %s |\n", peakMem, checkmark(peakMem < 2048))
	fmt.Fprintf(w, "\n")

	// gRPC Performance
	fmt.Fprintf(w, "## 🚀 gRPC Performance (Client View)\n\n")
	fmt.Fprintf(w, "### Request Statistics\n\n")
	fmt.Fprintf(w, "| Metric | Value |\n")
	fmt.Fprintf(w, "|--------|-------|\n")
	fmt.Fprintf(w, "| **Total Requests** | %d |\n", ghz.Count)
	fmt.Fprintf(w, "| **Successful** | %d |\n", ghz.Count-int64(sumErrors(ghz)))
	fmt.Fprintf(w, "| **Failed** | %d |\n", sumErrors(ghz))
	fmt.Fprintf(w, "| **RPS** | %.2f req/sec |\n", ghz.Rps)
	fmt.Fprintf(w, "| **Duration** | %s |\n", ghz.Total)
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "### Latency Distribution\n\n")
	fmt.Fprintf(w, "| Percentile | Latency |\n")
	fmt.Fprintf(w, "|------------|----------|\n")
	fmt.Fprintf(w, "| **Fastest** | %s |\n", ghz.Fastest)
	fmt.Fprintf(w, "| **P50 (Median)** | %s |\n", getPercentileLatency(ghz, 50))
	fmt.Fprintf(w, "| **P75** | %s |\n", getPercentileLatency(ghz, 75))
	fmt.Fprintf(w, "| **P90** | %s |\n", getPercentileLatency(ghz, 90))
	fmt.Fprintf(w, "| **P95** | %s |\n", getPercentileLatency(ghz, 95))
	fmt.Fprintf(w, "| **P99** | %s |\n", getPercentileLatency(ghz, 99))
	fmt.Fprintf(w, "| **Slowest** | %s |\n", ghz.Slowest)
	fmt.Fprintf(w, "| **Average** | %s |\n", ghz.Average)
	fmt.Fprintf(w, "\n")

	// Logger Performance
	fmt.Fprintf(w, "## 📝 Logger Performance (Server Side)\n\n")
	fmt.Fprintf(w, "| Metric | Value |\n")
	fmt.Fprintf(w, "|--------|-------|\n")
	if serverMetrics.TotalRequests > 0 {
		fmt.Fprintf(w, "| **Total Logs** | %d |\n", serverMetrics.LogsWritten)
		fmt.Fprintf(w, "| **Dropped Logs** | %d |\n", serverMetrics.LogsDropped)
		fmt.Fprintf(w, "| **Drop Rate** | %.4f%% |\n", serverMetrics.DropRate)
		fmt.Fprintf(w, "| **Server Latency P50** | %.2f µs |\n", serverMetrics.LatencyP50)
		fmt.Fprintf(w, "| **Server Latency P95** | %.2f µs |\n", serverMetrics.LatencyP95)
		fmt.Fprintf(w, "| **Server Latency P99** | %.2f µs |\n", serverMetrics.LatencyP99)
	} else {
		fmt.Fprintf(w, "| *Metrics not available* | - |\n")
	}
	fmt.Fprintf(w, "\n")

	// Resource Utilization
	fmt.Fprintf(w, "## 💻 Resource Utilization\n\n")
	fmt.Fprintf(w, "### CPU Usage\n\n")
	fmt.Fprintf(w, "| Statistic | Value |\n")
	fmt.Fprintf(w, "|-----------|-------|\n")
	fmt.Fprintf(w, "| **Mean** | %.2f%% |\n", avgCPU)
	fmt.Fprintf(w, "| **Median** | %.2f%% |\n", calculateMedian(extractCPU(resources)))
	fmt.Fprintf(w, "| **P95** | %.2f%% |\n", calculatePercentile(extractCPU(resources), 95))
	fmt.Fprintf(w, "| **P99** | %.2f%% |\n", calculatePercentile(extractCPU(resources), 99))
	fmt.Fprintf(w, "| **Peak** | %.2f%% |\n", peakCPU)
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "### Memory Usage\n\n")
	fmt.Fprintf(w, "| Statistic | Value |\n")
	fmt.Fprintf(w, "|-----------|-------|\n")
	fmt.Fprintf(w, "| **Mean** | %.2f MB |\n", avgMem)
	fmt.Fprintf(w, "| **Median** | %.2f MB |\n", calculateMedian(extractMemory(resources)))
	fmt.Fprintf(w, "| **P95** | %.2f MB |\n", calculatePercentile(extractMemory(resources), 95))
	fmt.Fprintf(w, "| **P99** | %.2f MB |\n", calculatePercentile(extractMemory(resources), 99))
	fmt.Fprintf(w, "| **Peak** | %.2f MB |\n", peakMem)
	fmt.Fprintf(w, "\n")

	// I/O Statistics
	totalBlockWrite := calculateTotalBlockWrite(resources)
	avgWriteRate := totalBlockWrite / float64(ghz.Total.Seconds())
	fmt.Fprintf(w, "### Disk I/O\n\n")
	fmt.Fprintf(w, "| Metric | Value |\n")
	fmt.Fprintf(w, "|--------|-------|\n")
	fmt.Fprintf(w, "| **Total Written** | %.2f GB |\n", totalBlockWrite/1024)
	fmt.Fprintf(w, "| **Avg Write Rate** | %.2f MB/sec |\n", avgWriteRate)
	fmt.Fprintf(w, "| **Expected Rate** | %.2f MB/sec |\n", ghz.Rps*0.3)  // 300KB per request
	fmt.Fprintf(w, "| **Write Efficiency** | %.1f%% |\n", (avgWriteRate/(ghz.Rps*0.3))*100)
	fmt.Fprintf(w, "\n")

	// Trends over time
	fmt.Fprintf(w, "## 📈 Performance Trends\n\n")
	fmt.Fprintf(w, "### CPU Usage Over Time\n\n")
	fmt.Fprintf(w, "```\n")
	fmt.Fprintf(w, "%s\n", generateASCIIChart(extractCPU(resources), "CPU %%", 60))
	fmt.Fprintf(w, "```\n\n")

	fmt.Fprintf(w, "### Memory Usage Over Time\n\n")
	fmt.Fprintf(w, "```\n")
	fmt.Fprintf(w, "%s\n", generateASCIIChart(extractMemory(resources), "Memory MB", 60))
	fmt.Fprintf(w, "```\n\n")

	// Recommendations
	fmt.Fprintf(w, "## 💡 Analysis & Recommendations\n\n")
	
	if serverMetrics.DropRate < 0.1 {
		fmt.Fprintf(w, "✅ **Excellent:** Drop rate <0.1%% - logger is handling load very well\n\n")
	} else if serverMetrics.DropRate < 1.0 {
		fmt.Fprintf(w, "⚠️ **Good:** Drop rate <1%% - acceptable for production but room for improvement\n\n")
	} else {
		fmt.Fprintf(w, "❌ **Action Required:** Drop rate >1%% - buffer size or flush rate needs tuning\n\n")
	}

	if ghz.Average < 50*time.Millisecond {
		fmt.Fprintf(w, "✅ **Excellent:** Average latency <50ms - very responsive\n\n")
	} else if ghz.Average < 100*time.Millisecond {
		fmt.Fprintf(w, "⚠️ **Good:** Average latency <100ms - acceptable\n\n")
	} else {
		fmt.Fprintf(w, "❌ **Action Required:** Average latency >100ms - investigate bottlenecks\n\n")
	}

	if avgCPU < 300 {
		fmt.Fprintf(w, "✅ **Excellent:** CPU usage <300%% - good headroom available\n\n")
	} else if avgCPU < 400 {
		fmt.Fprintf(w, "⚠️ **Caution:** CPU usage approaching limit - consider scaling\n\n")
	} else {
		fmt.Fprintf(w, "❌ **At Capacity:** CPU usage at/above 400%% - scale horizontally\n\n")
	}

	fmt.Fprintf(w, "---\n\n")
	fmt.Fprintf(w, "*Report generated at %s*\n", time.Now().Format("2006-01-02 15:04:05"))

	return nil
}

// Helper functions

func parseInt(s string) int {
	val, _ := strconv.Atoi(strings.TrimSpace(s))
	return val
}

func parseFloat(s string) float64 {
	val, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return val
}

func checkmark(success bool) string {
	if success {
		return "✅"
	}
	return "❌"
}

func calculateErrorRate(ghz *GHZReport) float64 {
	total := float64(ghz.Count)
	errors := float64(sumErrors(ghz))
	if total == 0 {
		return 0
	}
	return (errors / total) * 100
}

func sumErrors(ghz *GHZReport) int {
	sum := 0
	for _, count := range ghz.ErrorDist {
		sum += count
	}
	return sum
}

func getP99Latency(ghz *GHZReport) string {
	return getPercentileLatency(ghz, 99)
}

func getP99LatencyMs(ghz *GHZReport) float64 {
	for _, bucket := range ghz.LatencyDistribution {
		if bucket.Percentage >= 99 {
			return bucket.Latency
		}
	}
	return float64(ghz.Slowest.Milliseconds())
}

func getPercentileLatency(ghz *GHZReport, percentile int) string {
	for _, bucket := range ghz.LatencyDistribution {
		if bucket.Percentage >= percentile {
			return fmt.Sprintf("%.2fms", bucket.Latency)
		}
	}
	return "N/A"
}

func calculateCPUStats(resources []ResourceSample) (avg, peak float64) {
	if len(resources) == 0 {
		return 0, 0
	}
	sum := 0.0
	peak = 0.0
	for _, r := range resources {
		sum += r.CPUPercent
		if r.CPUPercent > peak {
			peak = r.CPUPercent
		}
	}
	avg = sum / float64(len(resources))
	return
}

func calculateMemStats(resources []ResourceSample) (avg, peak float64) {
	if len(resources) == 0 {
		return 0, 0
	}
	sum := 0.0
	peak = 0.0
	for _, r := range resources {
		sum += r.MemoryMB
		if r.MemoryMB > peak {
			peak = r.MemoryMB
		}
	}
	avg = sum / float64(len(resources))
	return
}

func calculateTotalBlockWrite(resources []ResourceSample) float64 {
	if len(resources) == 0 {
		return 0
	}
	return resources[len(resources)-1].BlockWriteMB
}

func extractCPU(resources []ResourceSample) []float64 {
	result := make([]float64, len(resources))
	for i, r := range resources {
		result[i] = r.CPUPercent
	}
	return result
}

func extractMemory(resources []ResourceSample) []float64 {
	result := make([]float64, len(resources))
	for i, r := range resources {
		result[i] = r.MemoryMB
	}
	return result
}

func calculateMedian(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}

func calculatePercentile(values []float64, percentile int) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	idx := int(float64(len(sorted)) * float64(percentile) / 100.0)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func generateASCIIChart(values []float64, label string, width int) string {
	if len(values) == 0 {
		return "No data"
	}

	min := values[0]
	max := values[0]
	for _, v := range values {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}

	height := 15
	chart := make([][]rune, height)
	for i := range chart {
		chart[i] = make([]rune, width)
		for j := range chart[i] {
			chart[i][j] = ' '
		}
	}

	// Plot data
	step := float64(len(values)) / float64(width)
	valueRange := max - min
	if valueRange == 0 {
		valueRange = 1
	}

	for x := 0; x < width; x++ {
		idx := int(float64(x) * step)
		if idx >= len(values) {
			idx = len(values) - 1
		}
		
		normalizedValue := (values[idx] - min) / valueRange
		y := height - 1 - int(normalizedValue*float64(height-1))
		
		if y >= 0 && y < height {
			chart[y][x] = '█'
		}
	}

	// Build string
	var result strings.Builder
	result.WriteString(fmt.Sprintf("%s (%.2f - %.2f)\n\n", label, min, max))
	
	// Top value
	result.WriteString(fmt.Sprintf("%60.2f\n", max))
	
	// Chart
	for _, row := range chart {
		result.WriteString(string(row))
		result.WriteString("\n")
	}
	
	// Bottom value
	result.WriteString(fmt.Sprintf("%60.2f\n", min))
	result.WriteString(strings.Repeat("-", width))
	result.WriteString("\n")
	result.WriteString(fmt.Sprintf("Time (seconds) 0%55d", len(values)))
	
	return result.String()
}

