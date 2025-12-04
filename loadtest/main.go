package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/neeharmavuduru/logger-double-buffer/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	serverAddr = "localhost:8585"
)

// Statistics holds the load test results
type Statistics struct {
	totalRequests   int64
	successRequests int64
	failedRequests  int64
	totalLatency    int64 // in microseconds
	minLatency      int64
	maxLatency      int64
	mu              sync.Mutex
}

func (s *Statistics) recordLatency(latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	latencyMicros := latency.Microseconds()
	atomic.AddInt64(&s.totalLatency, latencyMicros)

	if s.minLatency == 0 || latencyMicros < s.minLatency {
		s.minLatency = latencyMicros
	}
	if latencyMicros > s.maxLatency {
		s.maxLatency = latencyMicros
	}
}

func (s *Statistics) print(duration time.Duration) {
	fmt.Println("\n=== Load Test Results ===")
	fmt.Printf("Total Duration:      %v\n", duration)
	fmt.Printf("Total Requests:      %d\n", s.totalRequests)
	fmt.Printf("Successful Requests: %d\n", s.successRequests)
	fmt.Printf("Failed Requests:     %d\n", s.failedRequests)
	
	if s.successRequests > 0 {
		avgLatency := float64(s.totalLatency) / float64(s.successRequests) / 1000.0 // convert to ms
		fmt.Printf("\nLatency Statistics:\n")
		fmt.Printf("  Min:     %.2f ms\n", float64(s.minLatency)/1000.0)
		fmt.Printf("  Max:     %.2f ms\n", float64(s.maxLatency)/1000.0)
		fmt.Printf("  Average: %.2f ms\n", avgLatency)
		
		rps := float64(s.successRequests) / duration.Seconds()
		fmt.Printf("\nThroughput:\n")
		fmt.Printf("  Requests/sec: %.2f\n", rps)
	}
}

// worker represents a concurrent client
func worker(id int, requestsPerWorker int, stats *Statistics, wg *sync.WaitGroup) {
	defer wg.Done()

	// Connect to server
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("Worker %d: Failed to connect: %v", id, err)
		atomic.AddInt64(&stats.failedRequests, int64(requestsPerWorker))
		return
	}
	defer conn.Close()

	client := pb.NewRandomNumberServiceClient(conn)

	// Make requests
	for i := 0; i < requestsPerWorker; i++ {
		atomic.AddInt64(&stats.totalRequests, 1)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		
		start := time.Now()
		_, err := client.GetRandomNumbers(ctx, &pb.GetRandomNumbersRequest{})
		latency := time.Since(start)
		
		cancel()

		if err != nil {
			atomic.AddInt64(&stats.failedRequests, 1)
			log.Printf("Worker %d: Request %d failed: %v", id, i+1, err)
		} else {
			atomic.AddInt64(&stats.successRequests, 1)
			stats.recordLatency(latency)
		}
	}
}

func main() {
	// Command line flags
	concurrency := flag.Int("c", 10, "Number of concurrent clients")
	requests := flag.Int("n", 100, "Total number of requests")
	showLoggerHelp := flag.Bool("logger-help", false, "Show information about logger performance testing")
	flag.Parse()

	if *showLoggerHelp {
		fmt.Println("=== Logger Performance Testing Guide ===")
		fmt.Println("\nTo compare logger strategies (atomic vs mutex):")
		fmt.Println("1. Run server with atomic strategy:")
		fmt.Println("   go run server/main.go -log-strategy=atomic -log-buffer-size=1048576")
		fmt.Println("")
		fmt.Println("2. Run load test and record results:")
		fmt.Println("   go run loadtest/main.go -c 100 -n 10000")
		fmt.Println("")
		fmt.Println("3. Stop server and check logger stats in the output")
		fmt.Println("")
		fmt.Println("4. Run server with mutex strategy:")
		fmt.Println("   go run server/main.go -log-strategy=mutex -log-buffer-size=1048576")
		fmt.Println("")
		fmt.Println("5. Run same load test:")
		fmt.Println("   go run loadtest/main.go -c 100 -n 10000")
		fmt.Println("")
		fmt.Println("6. Compare latencies and throughput between both runs")
		fmt.Println("\nThe server will print logger statistics on shutdown showing:")
		fmt.Println("  - Total logs written")
		fmt.Println("  - Number of flushes")
		fmt.Println("  - Dropped logs (if any)")
		fmt.Println("  - Total bytes written")
		fmt.Println("  - Flush errors")
		fmt.Println("\nRecommended test scenarios:")
		fmt.Println("  Low concurrency:  -c 10 -n 1000")
		fmt.Println("  Medium concurrency: -c 100 -n 10000")
		fmt.Println("  High concurrency: -c 1000 -n 100000")
		return
	}

	if *concurrency <= 0 || *requests <= 0 {
		log.Fatal("Concurrency and requests must be positive numbers")
	}

	// Calculate requests per worker
	requestsPerWorker := *requests / *concurrency
	remainingRequests := *requests % *concurrency

	fmt.Printf("Starting load test...\n")
	fmt.Printf("Concurrency:  %d workers\n", *concurrency)
	fmt.Printf("Total Requests: %d\n", *requests)
	fmt.Printf("Server:       %s\n", serverAddr)
	fmt.Printf("\nTip: Run with -logger-help to see how to compare logger strategies\n\n")

	// Initialize statistics
	stats := &Statistics{}

	// Start load test
	startTime := time.Now()
	var wg sync.WaitGroup

	// Launch workers
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		workerRequests := requestsPerWorker
		// Distribute remaining requests to first workers
		if i < remainingRequests {
			workerRequests++
		}
		go worker(i+1, workerRequests, stats, &wg)
	}

	// Wait for all workers to complete
	wg.Wait()
	duration := time.Since(startTime)

	// Print results
	stats.print(duration)

	// Additional information about logger impact
	fmt.Println("\n=== Logger Performance Impact ===")
	fmt.Println("To measure logger overhead:")
	fmt.Println("1. Check server output for logger statistics on shutdown")
	fmt.Println("2. Compare RPS between atomic and mutex strategies")
	fmt.Println("3. Examine log file size vs bytes written in stats")
	fmt.Println("\nNote: Each successful request generates one log entry.")
	fmt.Printf("Expected logs in server: %d\n", stats.successRequests)
}

