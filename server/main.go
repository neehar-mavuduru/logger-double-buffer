package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/neeharmavuduru/logger-double-buffer/asynclogger"
	pb "github.com/neeharmavuduru/logger-double-buffer/proto"
	"google.golang.org/grpc"
)

const (
	port         = ":8585"
	numRandoms   = 200
	maxRandomNum = 1000000
)

// requestBufferPool provides pre-allocated 300KB buffers for request processing
// This dramatically reduces GC pressure from ~692 MB/sec to ~100 MB/sec
var requestBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 300*1024) // 300KB
		return &buf
	},
}

// bytesBufferPool provides reusable bytes.Buffer for string building
// Used for building log payload entities
var bytesBufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// numbersBufferPool provides pre-allocated []string buffers for random number generation
// Eliminates 4.4KB []string allocation per request
var numbersBufferPool = sync.Pool{
	New: func() interface{} {
		s := make([]string, numRandoms)
		return &s
	},
}

// server is the implementation of RandomNumberService
type server struct {
	pb.UnimplementedRandomNumberServiceServer
	loggerManager *asynclogger.LoggerManager
}

// GetRandomNumbers generates 200 random numbers and returns them as a colon-separated string
// For load testing: logs 300KB per request
// Optimized with buffer pooling to minimize GC pressure
func (s *server) GetRandomNumbers(ctx context.Context, req *pb.GetRandomNumbersRequest) (*pb.GetRandomNumbersResponse, error) {
	// Get pre-allocated numbers slice from pool
	numbersPtr := numbersBufferPool.Get().(*[]string)
	numbers := *numbersPtr
	defer numbersBufferPool.Put(numbersPtr)

	// Generate 200 random numbers
	for i := 0; i < numRandoms; i++ {
		numbers[i] = strconv.Itoa(rand.Intn(maxRandomNum))
	}

	// Join with ':'
	result := strings.Join(numbers, ":")

	// Get 300KB buffer from pool (SOLUTION 2: Zero allocation!)
	const logSize = 300 * 1024 // 300KB
	logBufPtr := requestBufferPool.Get().(*[]byte)
	logBuf := *logBufPtr
	defer requestBufferPool.Put(logBufPtr)

	// Format log header
	n := copy(logBuf, "[")
	n += copy(logBuf[n:], time.Now().Format("2006-01-02 15:04:05.000"))
	n += copy(logBuf[n:], "] INFO: Request processed, result=")
	n += copy(logBuf[n:], result)
	n += copy(logBuf[n:], " | payload=")

	// Fill remaining space with realistic JSON-like data to reach 300KB
	payloadStart := n
	remaining := logSize - n - 1 // Reserve 1 byte for newline

	// Get bytes.Buffer from pool for entity building
	buf := bytesBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bytesBufferPool.Put(buf)

	// Generate realistic structured data to fill 300KB
	// Simulate multiple entities being logged
	entityCount := 0
	timestampStr := time.Now().Format(time.RFC3339Nano)

	for n < payloadStart+remaining-100 { // Leave 100 bytes margin
		// Build entity string using bytes.Buffer
		buf.Reset()
		buf.WriteString(`{"entity":`)
		buf.WriteString(strconv.Itoa(entityCount))
		buf.WriteString(`,"timestamp":"`)
		buf.WriteString(timestampStr)
		buf.WriteString(`","status":"processed","data":"`)

		n += copy(logBuf[n:], buf.Bytes())

		// Fill with data (simulating entity payload)
		chunkSize := 500 // Each entity ~500 bytes
		if n+chunkSize+10 > logSize-1 {
			chunkSize = logSize - n - 10 // Adjust for remaining space
		}

		for i := 0; i < chunkSize && n < logSize-10; i++ {
			logBuf[n] = 'X'
			n++
		}

		n += copy(logBuf[n:], `"},`)
		entityCount++
	}

	// Close the log entry
	if n < logSize-1 {
		logBuf[n] = '\n'
		n++
	}

	// Log using LogBytesWithEvent (zero-allocation path)
	// Extract event name from request, default to "random_numbers" if not provided
	eventName := req.GetEventName()
	if eventName == "" {
		eventName = "random_numbers" // Default event name for random number requests
	}
	s.loggerManager.LogBytesWithEvent(eventName, logBuf[:n])

	return &pb.GetRandomNumbersResponse{
		Numbers: result,
	}, nil
}

func main() {
	// Parse command-line flags for asynclogger configuration
	logBufferSize := flag.Int("log-buffer-size", 2*1024*1024, "Total log buffer size in bytes (default: 2MB)")
	logFlushInterval := flag.Duration("log-flush-interval", 10*time.Second, "Log flush interval (default: 10s)")
	logFilePath := flag.String("log-file", "logs/server.log", "Log file path")
	logNumShards := flag.Int("log-num-shards", 8, "Number of shards (default: 8)")
	flag.Parse()

	// Seed the random number generator
	rand.Seed(time.Now().UnixNano())

	// Configure standard logger for startup messages
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(os.Stdout)

	// Create async logger using asynclogger package
	loggerConfig := asynclogger.Config{
		BufferSize:    *logBufferSize,
		FlushInterval: *logFlushInterval,
		LogFilePath:   *logFilePath,
		NumShards:     *logNumShards,
	}

	loggerManager, err := asynclogger.NewLoggerManager(loggerConfig)
	if err != nil {
		log.Fatalf("Failed to create logger manager: %v", err)
	}

	// Print comprehensive stats periodically (every 5s for detailed investigation)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			// Aggregate logger stats
			totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, setSwaps := loggerManager.GetStatsSnapshot()
			dropRate := 0.0
			if totalLogs > 0 {
				dropRate = float64(droppedLogs) / float64(totalLogs) * 100.0
			}

			// GC stats
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)

			// Flush metrics
			flushMetrics := loggerManager.GetAggregatedFlushMetrics()
			avgFlushMs := float64(flushMetrics.AvgFlushDuration.Nanoseconds()) / 1e6
			maxFlushMs := float64(flushMetrics.MaxFlushDuration.Nanoseconds()) / 1e6

			// Overall metrics
			log.Printf("METRICS: Logs: %d Dropped: %d (%.4f%%) | Bytes: %d | Flushes: %d Errors: %d Swaps: %d | AvgFlush: %.2fms MaxFlush: %.2fms | GC: %d cycles %.2fms pause | Mem: %.2fMB",
				totalLogs, droppedLogs, dropRate, bytesWritten, flushes, flushErrors, setSwaps,
				avgFlushMs, maxFlushMs,
				memStats.NumGC, float64(memStats.PauseTotalNs)/1e6,
				float64(memStats.Alloc)/1024/1024)

			// Per-event statistics
			events := loggerManager.ListEventLoggers()
			if len(events) > 0 {
				var eventStatStrs []string
				for _, eventName := range events {
					totalLogs, droppedLogs, _, _, _, _, err := loggerManager.GetEventStats(eventName)
					if err == nil {
						eventDropRate := 0.0
						if totalLogs > 0 {
							eventDropRate = float64(droppedLogs) / float64(totalLogs) * 100.0
						}
						eventStatStrs = append(eventStatStrs, fmt.Sprintf("%s:%d(%.2f%%)", eventName, totalLogs, eventDropRate))
					}
				}
				if len(eventStatStrs) > 0 {
					log.Printf("EVENT_STATS: %s", strings.Join(eventStatStrs, " "))
				}
			}
		}
	}()

	defer func() {
		if err := loggerManager.Close(); err != nil {
			log.Printf("Error closing logger manager: %v", err)
		}
		// Print final stats
		totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, setSwaps := loggerManager.GetStatsSnapshot()
		log.Printf("Logger Stats - Total: %d, Dropped: %d, Bytes: %d, Flushes: %d, Errors: %d, Swaps: %d",
			totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, setSwaps)
	}()

	log.Printf("Async logger initialized with buffer size: %d bytes, shards: %d", *logBufferSize, *logNumShards)

	// Start pprof server for profiling
	go func() {
		log.Println("Starting pprof server on :6060")
		if err := http.ListenAndServe(":6060", nil); err != nil {
			log.Printf("pprof server error: %v", err)
		}
	}()

	// Log startup using LogBytes
	startupMsg := make([]byte, 256)
	n := copy(startupMsg, "[")
	n += copy(startupMsg[n:], time.Now().Format("2006-01-02 15:04:05.000"))
	n += copy(startupMsg[n:], "] INFO: Server starting with asynclogger (buffer=")
	n += copy(startupMsg[n:], strconv.Itoa(*logBufferSize))
	n += copy(startupMsg[n:], " shards=")
	n += copy(startupMsg[n:], strconv.Itoa(*logNumShards))
	n += copy(startupMsg[n:], ")\n")
	loggerManager.LogBytesWithEvent("server", startupMsg[:n])

	// Create TCP listener
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", port, err)
	}

	// Create gRPC server
	grpcServer := grpc.NewServer()
	pb.RegisterRandomNumberServiceServer(grpcServer, &server{
		loggerManager: loggerManager,
	})

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in server goroutine: %v", r)
				// Log panic using LogBytes
				panicMsg := make([]byte, 512)
				n := copy(panicMsg, "[")
				n += copy(panicMsg[n:], time.Now().Format("2006-01-02 15:04:05.000"))
				n += copy(panicMsg[n:], "] PANIC: Server goroutine panic: ")
				n += copy(panicMsg[n:], fmt.Sprint(r))
				panicMsg[n] = '\n'
				loggerManager.LogBytesWithEvent("server", panicMsg[:n+1])
			}
		}()
		log.Printf("Server listening on port %s", port)

		// Log listening using LogBytes
		listenMsg := make([]byte, 256)
		n := copy(listenMsg, "[")
		n += copy(listenMsg[n:], time.Now().Format("2006-01-02 15:04:05.000"))
		n += copy(listenMsg[n:], "] INFO: Server listening on port ")
		n += copy(listenMsg[n:], port)
		listenMsg[n] = '\n'
		loggerManager.LogBytesWithEvent("server", listenMsg[:n+1])

		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-sigChan
	log.Println("Shutting down server gracefully...")

	// Log shutdown using LogBytes
	shutdownMsg := make([]byte, 256)
	n = copy(shutdownMsg, "[")
	n += copy(shutdownMsg[n:], time.Now().Format("2006-01-02 15:04:05.000"))
	n += copy(shutdownMsg[n:], "] INFO: Received shutdown signal, stopping server gracefully...\n")
	loggerManager.LogBytesWithEvent("server", shutdownMsg[:n])

	grpcServer.GracefulStop()

	// Log stopped using LogBytes
	stoppedMsg := make([]byte, 256)
	n = copy(stoppedMsg, "[")
	n += copy(stoppedMsg[n:], time.Now().Format("2006-01-02 15:04:05.000"))
	n += copy(stoppedMsg[n:], "] INFO: Server stopped\n")
	loggerManager.LogBytesWithEvent("server", stoppedMsg[:n])

	fmt.Println("Server stopped")
}
