package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type GHZReport struct {
	Average   float64 `json:"average"`
	Fastest   float64 `json:"fastest"`
	Slowest   float64 `json:"slowest"`
	RPS       float64 `json:"rps"`
	Total     float64 `json:"total"`
	ErrorDist map[string]int `json:"errorDistribution"`
	LatencyDistribution []struct {
		Percentage int     `json:"percentage"`
		Latency    float64 `json:"latency"`
	} `json:"latencyDistribution"`
}

type MetricPoint struct {
	Timestamp    int
	Logs         int64
	Dropped      int64
	DropRate     float64
	GCCycles     int
	GCPause      float64
	Memory       float64
	FlushAvg     float64
	FlushMax     float64
	FlushQueue   int64
	FlushBlocked int64
	FlushTotal   int64
}

func main() {
	resultsDir := "results/cliff_investigation"
	
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println("       210s Cliff Investigation - Analysis Report")
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println()
	
	// Parse server logs
	metrics := parseServerLogs(resultsDir + "/server.log")
	
	if len(metrics) == 0 {
		fmt.Println("❌ No metrics found in server logs!")
		return
	}
	
	// Parse GHZ report
	ghzReport := parseGHZReport(resultsDir + "/ghz_report.json")
	
	// Analyze the cliff
	analyzeCliff(metrics)
	
	// Analyze flush performance
	analyzeFlushPerformance(metrics)
	
	// Analyze resource usage
	analyzeResourceUsage(resultsDir + "/resource_timeline.csv")
	
	// Overall assessment
	printOverallAssessment(metrics, ghzReport)
	
	// Generate detailed timeline
	generateDetailedTimeline(metrics, resultsDir)
}

func parseServerLogs(logFile string) []MetricPoint {
	file, err := os.Open(logFile)
	if err != nil {
		fmt.Printf("❌ Error opening server log: %v\n", err)
		return nil
	}
	defer file.Close()
	
	metricsRegex := regexp.MustCompile(`METRICS: Logs: (\d+) Dropped: (\d+) \(([\d.]+)%\) \| GC: (\d+) cycles ([\d.]+)ms pause \| Mem: ([\d.]+)MB`)
	flushRegex := regexp.MustCompile(`FLUSH_METRICS: Avg: ([\d.]+)ms Max: ([\d.]+)ms Queue: (\d+) Blocked: (\d+) Total: (\d+)`)
	
	var metrics []MetricPoint
	var currentMetric *MetricPoint
	startTime := time.Time{}
	
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		
		// Extract timestamp
		if strings.Contains(line, "METRICS:") {
			// Parse timestamp from log line
			parts := strings.Split(line, " ")
			if len(parts) >= 2 {
				timeStr := parts[0] + " " + parts[1]
				t, err := time.Parse("2006/01/02 15:04:05.000000", timeStr)
				if err == nil {
					if startTime.IsZero() {
						startTime = t
					}
					currentMetric = &MetricPoint{
						Timestamp: int(t.Sub(startTime).Seconds()),
					}
				}
			}
			
			// Parse metrics
			if matches := metricsRegex.FindStringSubmatch(line); len(matches) == 7 {
				if currentMetric != nil {
					currentMetric.Logs, _ = strconv.ParseInt(matches[1], 10, 64)
					currentMetric.Dropped, _ = strconv.ParseInt(matches[2], 10, 64)
					currentMetric.DropRate, _ = strconv.ParseFloat(matches[3], 64)
					currentMetric.GCCycles, _ = strconv.Atoi(matches[4])
					currentMetric.GCPause, _ = strconv.ParseFloat(matches[5], 64)
					currentMetric.Memory, _ = strconv.ParseFloat(matches[6], 64)
				}
			}
		}
		
		// Parse flush metrics
		if strings.Contains(line, "FLUSH_METRICS:") && currentMetric != nil {
			if matches := flushRegex.FindStringSubmatch(line); len(matches) == 6 {
				currentMetric.FlushAvg, _ = strconv.ParseFloat(matches[1], 64)
				currentMetric.FlushMax, _ = strconv.ParseFloat(matches[2], 64)
				currentMetric.FlushQueue, _ = strconv.ParseInt(matches[3], 10, 64)
				currentMetric.FlushBlocked, _ = strconv.ParseInt(matches[4], 10, 64)
				currentMetric.FlushTotal, _ = strconv.ParseInt(matches[5], 10, 64)
				
				// Add complete metric
				metrics = append(metrics, *currentMetric)
				currentMetric = nil
			}
		}
	}
	
	return metrics
}

func parseGHZReport(reportFile string) *GHZReport {
	data, err := os.ReadFile(reportFile)
	if err != nil {
		fmt.Printf("⚠️  Warning: Could not read GHZ report: %v\n", err)
		return nil
	}
	
	var report GHZReport
	if err := json.Unmarshal(data, &report); err != nil {
		fmt.Printf("⚠️  Warning: Could not parse GHZ report: %v\n", err)
		return nil
	}
	
	return &report
}

func analyzeCliff(metrics []MetricPoint) {
	fmt.Println("## 🔍 Cliff Analysis")
	fmt.Println()
	
	// Find the cliff point (where drops spike)
	cliffThreshold := 1.0 // 1% drop rate
	cliffIdx := -1
	preCliffIdx := -1
	
	for i, m := range metrics {
		if m.DropRate > cliffThreshold {
			cliffIdx = i
			if i > 0 {
				preCliffIdx = i - 1
			}
			break
		}
	}
	
	if cliffIdx == -1 {
		fmt.Println("✅ No cliff detected! Performance remained stable.")
		return
	}
	
	preCliff := metrics[preCliffIdx]
	atCliff := metrics[cliffIdx]
	
	fmt.Printf("**Cliff detected at %ds**\n", atCliff.Timestamp)
	fmt.Println()
	
	fmt.Println("| Metric | Before Cliff | At Cliff | Change |")
	fmt.Println("|--------|-------------|----------|--------|")
	fmt.Printf("| Drop Rate | %.4f%% | %.4f%% | **%.1fx** |\n", 
		preCliff.DropRate, atCliff.DropRate, atCliff.DropRate/preCliff.DropRate)
	fmt.Printf("| Drops/sec | %.1f | %.1f | **+%.0f** |\n",
		float64(preCliff.Dropped)/float64(preCliff.Timestamp),
		float64(atCliff.Dropped-preCliff.Dropped)/10.0,
		(float64(atCliff.Dropped-preCliff.Dropped)/10.0)-(float64(preCliff.Dropped)/float64(preCliff.Timestamp)))
	fmt.Printf("| GC Cycles | %d | %d | %+d |\n", 
		preCliff.GCCycles, atCliff.GCCycles, atCliff.GCCycles-preCliff.GCCycles)
	fmt.Printf("| Memory | %.1fMB | %.1fMB | %+.1fMB |\n",
		preCliff.Memory, atCliff.Memory, atCliff.Memory-preCliff.Memory)
	fmt.Printf("| Flush Avg | %.2fms | %.2fms | %+.2fms |\n",
		preCliff.FlushAvg, atCliff.FlushAvg, atCliff.FlushAvg-preCliff.FlushAvg)
	fmt.Printf("| Flush Max | %.2fms | %.2fms | %+.2fms |\n",
		preCliff.FlushMax, atCliff.FlushMax, atCliff.FlushMax-preCliff.FlushMax)
	fmt.Printf("| Flush Queue | %d | %d | %+d |\n",
		preCliff.FlushQueue, atCliff.FlushQueue, atCliff.FlushQueue-preCliff.FlushQueue)
	fmt.Printf("| Blocked Swaps | %d | %d | %+d |\n",
		preCliff.FlushBlocked, atCliff.FlushBlocked, atCliff.FlushBlocked-preCliff.FlushBlocked)
	fmt.Println()
	
	// Identify likely cause
	fmt.Println("### 💡 Likely Root Cause")
	fmt.Println()
	
	gcIncrease := atCliff.GCCycles - preCliff.GCCycles
	flushAvgIncrease := atCliff.FlushAvg - preCliff.FlushAvg
	flushMaxIncrease := atCliff.FlushMax - preCliff.FlushMax
	queueIncrease := atCliff.FlushQueue - preCliff.FlushQueue
	blockedIncrease := atCliff.FlushBlocked - preCliff.FlushBlocked
	
	causes := []struct {
		name       string
		score      float64
		evidence   string
	}{
		{
			name:  "Disk I/O Saturation",
			score: flushAvgIncrease * 10 + flushMaxIncrease * 5,
			evidence: fmt.Sprintf("Flush avg increased by %.2fms, max by %.2fms", flushAvgIncrease, flushMaxIncrease),
		},
		{
			name:  "Flush Worker Blockage",
			score: float64(blockedIncrease) * 100,
			evidence: fmt.Sprintf("Blocked swaps increased by %d", blockedIncrease),
		},
		{
			name:  "Flush Queue Saturation",
			score: float64(queueIncrease) * 50,
			evidence: fmt.Sprintf("Queue depth increased by %d", queueIncrease),
		},
		{
			name:  "GC Pressure",
			score: float64(gcIncrease) * 10,
			evidence: fmt.Sprintf("GC cycles increased by %d", gcIncrease),
		},
	}
	
	// Sort by score
	sort.Slice(causes, func(i, j int) bool {
		return causes[i].score > causes[j].score
	})
	
	fmt.Println("**Top suspects (ranked by evidence):**")
	fmt.Println()
	for i, cause := range causes {
		if i >= 3 || cause.score == 0 {
			break
		}
		fmt.Printf("%d. **%s** (score: %.1f)\n", i+1, cause.name, cause.score)
		fmt.Printf("   - %s\n", cause.evidence)
		fmt.Println()
	}
}

func analyzeFlushPerformance(metrics []MetricPoint) {
	fmt.Println("## 📊 Flush Performance Analysis")
	fmt.Println()
	
	// Calculate statistics
	var avgFlushTimes []float64
	var maxFlushTimes []float64
	var blockedSwaps []int64
	
	for _, m := range metrics {
		if m.FlushTotal > 0 {
			avgFlushTimes = append(avgFlushTimes, m.FlushAvg)
			maxFlushTimes = append(maxFlushTimes, m.FlushMax)
			blockedSwaps = append(blockedSwaps, m.FlushBlocked)
		}
	}
	
	if len(avgFlushTimes) == 0 {
		fmt.Println("⚠️  No flush metrics available")
		return
	}
	
	// Calculate percentiles
	sort.Float64s(avgFlushTimes)
	sort.Float64s(maxFlushTimes)
	
	p50Avg := avgFlushTimes[len(avgFlushTimes)/2]
	p95Avg := avgFlushTimes[int(float64(len(avgFlushTimes))*0.95)]
	p50Max := maxFlushTimes[len(maxFlushTimes)/2]
	p95Max := maxFlushTimes[int(float64(len(maxFlushTimes))*0.95)]
	
	lastMetric := metrics[len(metrics)-1]
	
	fmt.Println("| Metric | Value |")
	fmt.Println("|--------|-------|")
	fmt.Printf("| Avg Flush (P50) | %.2fms |\n", p50Avg)
	fmt.Printf("| Avg Flush (P95) | %.2fms |\n", p95Avg)
	fmt.Printf("| Max Flush (P50) | %.2fms |\n", p50Max)
	fmt.Printf("| Max Flush (P95) | %.2fms |\n", p95Max)
	fmt.Printf("| Total Flushes | %d |\n", lastMetric.FlushTotal)
	fmt.Printf("| Blocked Swaps | %d |\n", lastMetric.FlushBlocked)
	fmt.Printf("| Blocked Rate | %.2f%% |\n", float64(lastMetric.FlushBlocked)/float64(lastMetric.FlushTotal)*100.0)
	fmt.Println()
	
	// Identify flush degradation
	if p95Avg > 100 || p95Max > 500 {
		fmt.Println("⚠️  **Flush performance degraded significantly!**")
		fmt.Println()
		if p95Max > 500 {
			fmt.Println("- Max flush time exceeds 500ms → likely disk I/O issue")
		}
		if lastMetric.FlushBlocked > 10 {
			fmt.Println("- Multiple blocked swaps → flush worker can't keep up")
		}
	} else {
		fmt.Println("✅ Flush performance remained reasonable")
	}
	fmt.Println()
}

func analyzeResourceUsage(csvFile string) {
	fmt.Println("## 💻 Resource Usage Analysis")
	fmt.Println()
	
	file, err := os.Open(csvFile)
	if err != nil {
		fmt.Printf("⚠️  Could not read resource timeline: %v\n", err)
		return
	}
	defer file.Close()
	
	type ResourcePoint struct {
		timestamp int
		cpu       float64
		mem       float64
		blockIn   float64
		blockOut  float64
	}
	
	var resources []ResourcePoint
	scanner := bufio.NewScanner(file)
	
	// Skip header
	scanner.Scan()
	
	startTime := 0
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Split(line, ",")
		if len(fields) < 8 {
			continue
		}
		
		timestamp, _ := strconv.Atoi(fields[0])
		if startTime == 0 {
			startTime = timestamp
		}
		
		cpu, _ := strconv.ParseFloat(fields[1], 64)
		mem, _ := strconv.ParseFloat(fields[2], 64)
		blockIn, _ := strconv.ParseFloat(fields[5], 64)
		blockOut, _ := strconv.ParseFloat(fields[6], 64)
		
		resources = append(resources, ResourcePoint{
			timestamp: timestamp - startTime,
			cpu:       cpu,
			mem:       mem,
			blockIn:   blockIn,
			blockOut:  blockOut,
		})
	}
	
	if len(resources) == 0 {
		fmt.Println("⚠️  No resource data available")
		return
	}
	
	// Calculate statistics
	var cpuValues []float64
	var memValues []float64
	var blockOutValues []float64
	
	for _, r := range resources {
		cpuValues = append(cpuValues, r.cpu)
		memValues = append(memValues, r.mem)
		blockOutValues = append(blockOutValues, r.blockOut)
	}
	
	sort.Float64s(cpuValues)
	sort.Float64s(memValues)
	sort.Float64s(blockOutValues)
	
	fmt.Println("| Metric | P50 | P95 | P99 |")
	fmt.Println("|--------|-----|-----|-----|")
	fmt.Printf("| CPU %% | %.1f%% | %.1f%% | %.1f%% |\n",
		cpuValues[len(cpuValues)/2],
		cpuValues[int(float64(len(cpuValues))*0.95)],
		cpuValues[int(float64(len(cpuValues))*0.99)])
	fmt.Printf("| Memory MB | %.0f | %.0f | %.0f |\n",
		memValues[len(memValues)/2],
		memValues[int(float64(len(memValues))*0.95)],
		memValues[int(float64(len(memValues))*0.99)])
	fmt.Printf("| Block Out MB | %.1f | %.1f | %.1f |\n",
		blockOutValues[len(blockOutValues)/2],
		blockOutValues[int(float64(len(blockOutValues))*0.95)],
		blockOutValues[int(float64(len(blockOutValues))*0.99)])
	fmt.Println()
	
	// Find cliff in block I/O
	for i := 1; i < len(resources); i++ {
		if resources[i].timestamp > 200 && resources[i].blockOut > resources[i-1].blockOut*2 {
			fmt.Printf("⚠️  **Block I/O spike detected at %ds** (%.1fMB → %.1fMB)\n",
				resources[i].timestamp, resources[i-1].blockOut, resources[i].blockOut)
			fmt.Println()
			break
		}
	}
}

func printOverallAssessment(metrics []MetricPoint, ghz *GHZReport) {
	fmt.Println("## 🎯 Overall Assessment")
	fmt.Println()
	
	lastMetric := metrics[len(metrics)-1]
	
	fmt.Println("### Test Summary")
	fmt.Println()
	fmt.Printf("- **Duration:** %ds (%.1f minutes)\n", lastMetric.Timestamp, float64(lastMetric.Timestamp)/60.0)
	fmt.Printf("- **Total Logs:** %d\n", lastMetric.Logs)
	fmt.Printf("- **Dropped:** %d (%.2f%%)\n", lastMetric.Dropped, lastMetric.DropRate)
	fmt.Printf("- **Data Written:** %.2f GB\n", float64(lastMetric.Logs)*300/1024/1024/1024)
	fmt.Printf("- **GC Cycles:** %d\n", lastMetric.GCCycles)
	fmt.Printf("- **Total Flushes:** %d\n", lastMetric.FlushTotal)
	fmt.Println()
	
	if ghz != nil {
		fmt.Println("### gRPC Performance")
		fmt.Println()
		fmt.Printf("- **Average Latency:** %.2fms\n", ghz.Average*1000)
		fmt.Printf("- **P95 Latency:** %.2fms\n", getLatencyPercentile(ghz, 95))
		fmt.Printf("- **P99 Latency:** %.2fms\n", getLatencyPercentile(ghz, 99))
		fmt.Printf("- **Throughput:** %.1f RPS\n", ghz.RPS)
		fmt.Println()
	}
	
	fmt.Println("### 🔬 Key Findings")
	fmt.Println()
	
	// Analyze the pattern
	perfectDuration := 0
	for _, m := range metrics {
		if m.DropRate < 0.1 {
			perfectDuration = m.Timestamp
		} else {
			break
		}
	}
	
	if perfectDuration > 0 {
		fmt.Printf("✅ **Perfect performance for %ds** (%.1f minutes)\n", perfectDuration, float64(perfectDuration)/60.0)
		fmt.Printf("   - Drop rate: <0.1%%\n")
		fmt.Printf("   - GC cycles: %d\n", getMetricAtTime(metrics, perfectDuration).GCCycles)
		fmt.Println()
	}
	
	if lastMetric.DropRate > 10 {
		fmt.Printf("⚠️  **Severe degradation after %ds**\n", perfectDuration)
		fmt.Printf("   - Drop rate increased to %.2f%%\n", lastMetric.DropRate)
		fmt.Println()
	}
	
	fmt.Println("### 💡 Next Steps")
	fmt.Println()
	fmt.Println("1. **Analyze profiles at 200s, 210s, 220s:**")
	fmt.Println("   ```bash")
	fmt.Println("   go tool pprof results/cliff_investigation/profiles/goroutine_210s.prof")
	fmt.Println("   go tool pprof results/cliff_investigation/profiles/heap_210s.prof")
	fmt.Println("   ```")
	fmt.Println()
	fmt.Println("2. **Check for blocked goroutines:**")
	fmt.Println("   ```bash")
	fmt.Println("   go tool pprof -list=flushSet results/cliff_investigation/profiles/block_210s.prof")
	fmt.Println("   ```")
	fmt.Println()
	fmt.Println("3. **Compare profiles before vs at cliff:**")
	fmt.Println("   ```bash")
	fmt.Println("   go tool pprof -base results/cliff_investigation/profiles/heap_200s.prof \\")
	fmt.Println("                         results/cliff_investigation/profiles/heap_210s.prof")
	fmt.Println("   ```")
	fmt.Println()
}

func getMetricAtTime(metrics []MetricPoint, targetTime int) MetricPoint {
	for _, m := range metrics {
		if m.Timestamp >= targetTime {
			return m
		}
	}
	return metrics[len(metrics)-1]
}

func getLatencyPercentile(ghz *GHZReport, percentile int) float64 {
	for _, ld := range ghz.LatencyDistribution {
		if ld.Percentage == percentile {
			return ld.Latency * 1000
		}
	}
	return 0
}

func generateDetailedTimeline(metrics []MetricPoint, resultsDir string) {
	filename := resultsDir + "/DETAILED_TIMELINE.md"
	f, err := os.Create(filename)
	if err != nil {
		fmt.Printf("⚠️  Could not create timeline file: %v\n", err)
		return
	}
	defer f.Close()
	
	fmt.Fprintln(f, "# Detailed Timeline - 210s Cliff Investigation")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "## Metrics Over Time")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "| Time | Logs | Dropped | Drop% | GC | Mem | Flush Avg | Flush Max | Queue | Blocked |")
	fmt.Fprintln(f, "|------|------|---------|-------|----|----|-----------|-----------|-------|---------|")
	
	for _, m := range metrics {
		status := "✅"
		if m.DropRate > 10 {
			status = "💥"
		} else if m.DropRate > 1 {
			status = "⚠️"
		}
		
		fmt.Fprintf(f, "| %s %3ds | %7d | %7d | %6.2f%% | %2d | %6.1f | %7.2f | %8.2f | %5d | %7d |\n",
			status, m.Timestamp, m.Logs, m.Dropped, m.DropRate, m.GCCycles, m.Memory,
			m.FlushAvg, m.FlushMax, m.FlushQueue, m.FlushBlocked)
	}
	
	fmt.Printf("✅ Detailed timeline saved: %s\n", filename)
	fmt.Println()
}

