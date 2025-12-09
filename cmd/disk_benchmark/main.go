//go:build linux

package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const alignmentSize = 4096 // ext4 block size

// allocAlignedBuffer allocates a byte slice aligned to 4096-byte boundary for O_DIRECT
func allocAlignedBuffer(size int) []byte {
	alignedSize := ((size + alignmentSize - 1) / alignmentSize) * alignmentSize
	buf := make([]byte, alignedSize+alignmentSize)
	addr := uintptr(unsafe.Pointer(&buf[0]))
	offset := int(alignmentSize - (addr % alignmentSize))
	if offset == alignmentSize {
		offset = 0
	}
	return buf[offset : offset+alignedSize]
}

// openDirectIOBenchmark opens a file with O_DIRECT and O_DSYNC flags (same as logger)
func openDirectIOBenchmark(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	fd, err := syscall.Open(path,
		syscall.O_WRONLY|syscall.O_CREAT|syscall.O_DIRECT|syscall.O_DSYNC,
		0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file with O_DIRECT: %w", err)
	}

	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to create file descriptor")
	}

	return file, nil
}

// writeAligned writes aligned buffer using Pwritev at specific offset
func writeAligned(fd int, buffer []byte, offset int64) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}

	// Use Pwritev for offset-based write (same as logger)
	n, err := unix.Pwritev(fd, [][]byte{buffer}, offset)
	if err != nil {
		return n, fmt.Errorf("write failed: %w", err)
	}

	return n, nil
}

type Metrics struct {
	Iterations      int64
	TotalBytes      int64
	TotalDuration   time.Duration
	MinDuration     time.Duration
	MaxDuration     time.Duration
	Durations       []time.Duration // For percentile calculation
	Errors          int64
}

func (m *Metrics) CalculateStats() Stats {
	stats := Stats{
		Iterations:    m.Iterations,
		TotalBytes:     m.TotalBytes,
		Errors:         m.Errors,
		MinDuration:    m.MinDuration,
		MaxDuration:    m.MaxDuration,
		AvgDuration:    time.Duration(m.TotalDuration.Nanoseconds() / m.Iterations),
		TotalDuration:  m.TotalDuration,
	}

	if len(m.Durations) > 0 {
		// Sort for percentile calculation
		sorted := make([]time.Duration, len(m.Durations))
		copy(sorted, m.Durations)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i] < sorted[j]
		})

		stats.P50Duration = percentile(sorted, 50)
		stats.P95Duration = percentile(sorted, 95)
		stats.P99Duration = percentile(sorted, 99)
	}

	// Calculate throughput
	if m.TotalDuration > 0 {
		stats.ThroughputMBps = float64(m.TotalBytes) / m.TotalDuration.Seconds() / (1024 * 1024)
	}

	return stats
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)) * p / 100.0)
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

type Stats struct {
	Iterations     int64
	TotalBytes     int64
	Errors         int64
	MinDuration    time.Duration
	MaxDuration    time.Duration
	AvgDuration    time.Duration
	P50Duration    time.Duration
	P95Duration    time.Duration
	P99Duration    time.Duration
	TotalDuration  time.Duration
	ThroughputMBps float64
}

func main() {
	var (
		bufferSizeMB = flag.Int("buffer-mb", 32, "Buffer size in MB")
		duration     = flag.Duration("duration", 5*time.Minute, "Test duration")
		logPath      = flag.String("log-path", "logs/disk_benchmark.log", "Log file path")
		numBuffers   = flag.Int("num-buffers", 10, "Number of pre-generated buffers (for different data)")
	)
	flag.Parse()

	bufferSize := *bufferSizeMB * 1024 * 1024
	log.Printf("Starting disk benchmark:")
	log.Printf("  Buffer Size: %d MB (%d bytes)", *bufferSizeMB, bufferSize)
	log.Printf("  Duration: %v", *duration)
	log.Printf("  Log Path: %s", *logPath)
	log.Printf("  Pre-generated Buffers: %d", *numBuffers)
	log.Println()

	// Pre-generate buffers with different data (to avoid affecting measurements)
	log.Printf("Pre-generating %d buffers with different data...", *numBuffers)
	buffers := make([][]byte, *numBuffers)
	for i := 0; i < *numBuffers; i++ {
		buf := allocAlignedBuffer(bufferSize)
		// Fill with pseudo-random data (different seed for each buffer)
		rng := rand.New(rand.NewSource(int64(i + 1000)))
		for j := 0; j < len(buf); j++ {
			buf[j] = byte(rng.Intn(256))
		}
		buffers[i] = buf
	}
	log.Printf("✓ Pre-generated %d buffers", *numBuffers)
	log.Println()

	// Open file
	file, err := openDirectIOBenchmark(*logPath)
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	fd := int(file.Fd())
	metrics := &Metrics{
		Durations: make([]time.Duration, 0, 10000), // Pre-allocate for ~10000 samples
		MinDuration: time.Hour,                      // Initialize to large value
	}

	// Track offset manually (like logger)
	var offset int64

	// Run benchmark
	log.Printf("Starting benchmark (will run for %v)...", *duration)
	startTime := time.Now()
	endTime := startTime.Add(*duration)
	bufferIndex := 0
	iteration := int64(0)

	for time.Now().Before(endTime) {
		// Select buffer (rotate through pre-generated buffers)
		buffer := buffers[bufferIndex]
		bufferIndex = (bufferIndex + 1) % *numBuffers

		// Measure write duration
		writeStart := time.Now()
		n, err := writeAligned(fd, buffer, offset)
		writeDuration := time.Since(writeStart)

		if err != nil {
			metrics.Errors++
			log.Printf("Write error at iteration %d: %v", iteration, err)
		} else {
			metrics.Iterations++
			metrics.TotalBytes += int64(n)
			metrics.TotalDuration += writeDuration
			metrics.Durations = append(metrics.Durations, writeDuration)

			// Update min/max
			if writeDuration < metrics.MinDuration {
				metrics.MinDuration = writeDuration
			}
			if writeDuration > metrics.MaxDuration {
				metrics.MaxDuration = writeDuration
			}

			// Update offset
			offset += int64(n)
		}

		iteration++

		// Print progress every 1000 iterations
		if iteration%1000 == 0 {
			elapsed := time.Since(startTime)
			remaining := *duration - elapsed
			log.Printf("Progress: %d iterations, %.1f%% complete, ~%v remaining",
				iteration, float64(elapsed)*100/float64(*duration), remaining)
		}
	}

	actualDuration := time.Since(startTime)
	log.Println()
	log.Printf("Benchmark completed in %v", actualDuration)
	log.Printf("Total iterations: %d", metrics.Iterations)
	log.Printf("Errors: %d", metrics.Errors)
	log.Println()

	// Calculate and print statistics
	stats := metrics.CalculateStats()
	printStats(stats, *bufferSizeMB)
}

func printStats(stats Stats, bufferSizeMB int) {
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println("                    DISK BENCHMARK RESULTS")
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Buffer Size: %d MB\n", bufferSizeMB)
	fmt.Printf("  Total Iterations: %d\n", stats.Iterations)
	fmt.Printf("  Total Bytes Written: %d (%.2f GB)\n", stats.TotalBytes, float64(stats.TotalBytes)/(1024*1024*1024))
	fmt.Printf("  Errors: %d\n", stats.Errors)
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println("Latency Statistics:")
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Printf("  Min Duration:     %12.3f ms\n", stats.MinDuration.Seconds()*1000)
	fmt.Printf("  Avg Duration:     %12.3f ms\n", stats.AvgDuration.Seconds()*1000)
	fmt.Printf("  Max Duration:     %12.3f ms\n", stats.MaxDuration.Seconds()*1000)
	fmt.Printf("  P50 (Median):     %12.3f ms\n", stats.P50Duration.Seconds()*1000)
	fmt.Printf("  P95:              %12.3f ms\n", stats.P95Duration.Seconds()*1000)
	fmt.Printf("  P99:              %12.3f ms\n", stats.P99Duration.Seconds()*1000)
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println("Throughput:")
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Printf("  Average Throughput: %.2f MB/s\n", stats.ThroughputMBps)
	fmt.Printf("  Total Duration:     %v\n", stats.TotalDuration)
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════")
}

