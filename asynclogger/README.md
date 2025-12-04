# Async Logger

High-performance async logger with Direct I/O support for Go applications.

## Overview

This package provides a lock-free, high-throughput async logger using a Sharded Double Buffer CAS (Compare-and-Swap) architecture with Linux Direct I/O for predictable write performance.

## Features

- **Zero-Allocation API**: `LogBytes()` eliminates GC pressure (99% reduction) for hot paths
- **High Performance**: High-throughput logging with minimal CPU overhead
- **Low Drop Rate**: 0.0000% under normal load
- **Direct I/O**: Bypasses OS page cache for predictable writes (Linux only)
- **Lock-Free Writes**: Uses atomic CAS for contention-free logging
- **Optimal Defaults**: 64MB buffer, 8 shards
- **Thread-Safe**: Handles 8-100+ concurrent goroutines efficiently
- **Graceful Shutdown**: Ensures all logs are flushed before exit
- **Statistics**: Built-in metrics for monitoring
- **Dual API**: Choose between convenience (`Log`) and performance (`LogBytes`)

## Architecture

### Sharded Double Buffer CAS

```
┌─────────────────────────────────────────────┐
│           Active Buffer Set                 │
│  ┌───────┐ ┌───────┐ ┌───────┐ ┌───────┐  │
│  │Shard 0│ │Shard 1│ │Shard 2│ │Shard 3│  │
│  └───────┘ └───────┘ └───────┘ └───────┘  │
│         ... (8 shards total) ...            │
└─────────────────────────────────────────────┘
                    ↕ Atomic CAS Swap
┌─────────────────────────────────────────────┐
│          Flushing Buffer Set                │
│  ┌───────┐ ┌───────┐ ┌───────┐ ┌───────┐  │
│  │Shard 0│ │Shard 1│ │Shard 2│ │Shard 3│  │
│  └───────┘ └───────┘ └───────┘ └───────┘  │
│         ... (8 shards total) ...            │
└─────────────────────────────────────────────┘
                    ↓
            ┌───────────────┐
            │  Direct I/O   │
            │  (O_DIRECT)   │
            └───────────────┘
                    ↓
            ┌───────────────┐
            │   Log File    │
            └───────────────┘
```

### Key Components

1. **Buffer**: 512-byte aligned buffer for Direct I/O compatibility
2. **Shard**: Independent buffer with mutex protection
3. **BufferSet**: Collection of shards for parallel writes
4. **Logger**: Orchestrates double buffering, swapping, and flushing

## Log File Structure

The logger writes logs to disk with a two-level header system for data integrity and recovery:

### 1. Shard Header (8 bytes)

Each shard's data is prefixed with an 8-byte header:

```
┌──────────────────┬──────────────────────┐
│ Capacity (4 bytes)│ Valid Data (4 bytes)│
│   uint32 LE       │   uint32 LE         │
└──────────────────┴──────────────────────┘
```

- **Bytes 0-3**: Shard buffer capacity (total allocated size)
- **Bytes 4-7**: Valid data bytes (actual data written)

**Purpose:**
- Boundary validation (distinguish valid data from padding)
- Recovery from incomplete flushes
- Direct I/O alignment handling (buffers are 512-byte aligned)

### 2. Log Entry Header (4 bytes)

Each log entry is prefixed with a 4-byte length header:

```
┌─────────────────┬──────────────────────┐
│ Length (4 bytes) │ Log Data (N bytes)   │
│  uint32 LE      │  Actual log message  │
└─────────────────┴──────────────────────┘
```

- **Bytes 0-3**: Length of log data (little-endian uint32)
- **Bytes 4+**: Actual log message content

**Purpose:**
- Boundary detection (know where each entry starts/ends)
- Corruption detection (invalid length values)
- Recovery support (identify complete vs incomplete entries)

### Complete File Layout Example

```
File Structure:
┌─────────────────────────────────────────────────────────────┐
│ Shard 0 Combined Buffer (8 bytes header + 512KB data + padding) │
│ ├─ Header (8 bytes):                                        │
│ │  ├─ Capacity: 1MB                                         │
│ │  └─ Valid Data: 512KB                                     │
│ ├─ Data (512KB valid):                                      │
│ │  ├─ [4 bytes: length] "Log entry 1\n"                    │
│ │  ├─ [4 bytes: length] "Log entry 2\n"                    │
│ │  └─ ...                                                   │
│ └─ Padding (to 512-byte alignment, ignored by readers)    │
├─────────────────────────────────────────────────────────────┤
│ Shard 1 Combined Buffer (8 bytes header + 256KB data + padding) │
│ ├─ Header (8 bytes):                                        │
│ │  ├─ Capacity: 1MB                                         │
│ │  └─ Valid Data: 256KB                                     │
│ ├─ Data (256KB valid):                                      │
│ │  └─ [4 bytes: length] "Log entry N\n"                    │
│ └─ Padding (to 512-byte alignment, ignored by readers)     │
└─────────────────────────────────────────────────────────────┘
```

**Key Points:**
- Headers and shard data are written together as a single aligned buffer
- Padding occurs only at the END of each combined buffer (after shard data)
- Headers and data are contiguous - no padding between them
- Readers use `validDataBytes` from the header to know where valid data ends

**Reading Process:**
1. Read 8-byte shard header → extract capacity and valid data size
2. Read shard data starting immediately after header, up to `validDataBytes`
   - Padding at the end is automatically ignored since readers stop at `validDataBytes`
3. Parse log entries:
   - Read 4-byte length prefix
   - Read `length` bytes of log data
   - Repeat until end of valid data
4. Skip to next 512-byte aligned boundary (if needed for Direct I/O) and read next shard header

## Installation

```bash
# Copy the asynclogger package into your Go project
cp -r asynclogger/ /path/to/your/project/asynclogger/
```

## Usage

### Basic Usage

```go
package main

import (
    "log"
    "github.com/yourorg/go-core/asynclogger"
)

func main() {
    // Create logger with optimal defaults
    config := asynclogger.DefaultConfig("/var/log/app.log")
    logger, err := asynclogger.New(config)
    if err != nil {
        log.Fatal(err)
    }
    defer logger.Close() // Ensures all logs are flushed

    // Log messages using convenience API
    logger.Log("Application started")
    logger.Log("Processing request")
    logger.Log("Request completed")
}
```

### High-Performance Logging with LogBytes

For maximum performance and minimal GC pressure, use the `LogBytes` API with reusable buffers:

```go
import (
    "strconv"
    "github.com/yourorg/go-core/asynclogger"
)

// Worker goroutine with reusable buffer (zero allocations)
type Worker struct {
    logger *asynclogger.Logger
    msgBuf [256]byte  // Reused for every log
}

func (w *Worker) ProcessRequest(userID int, status string) {
    // Format message directly into buffer
    n := copy(w.msgBuf[:], "Processing request for user ")
    n += copy(w.msgBuf[n:], strconv.Itoa(userID))
    n += copy(w.msgBuf[n:], " status=")
    n += copy(w.msgBuf[n:], status)
    w.msgBuf[n] = '\n'
    
    // Log with zero allocations
    w.logger.LogBytes(w.msgBuf[:n+1])
}
```

**Performance Impact:**
- String API (`Log`): Allocates memory for each log message
- Bytes API (`LogBytes`) with reuse: **~0 MB/sec allocations** (99% reduction)
- GC pressure reduced by **70-99%** depending on usage pattern

### Using sync.Pool for Message Buffers

```go
import (
    "fmt"
    "sync"
    "github.com/yourorg/go-core/asynclogger"
)

var msgPool = sync.Pool{
    New: func() interface{} {
        buf := make([]byte, 256)
        return &buf
    },
}

func logRequest(logger *asynclogger.Logger, userID int) {
    bufPtr := msgPool.Get().(*[]byte)
    buf := *bufPtr
    defer msgPool.Put(bufPtr)
    
    // Format into pooled buffer
    n := copy(buf, fmt.Sprintf("Request for user %d\n", userID))
    logger.LogBytes(buf[:n])
}
```

### Monitoring Statistics

```go
// Get snapshot of current statistics
totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, setSwaps := logger.GetStatsSnapshot()

fmt.Printf("Total Logs: %d\n", totalLogs)
fmt.Printf("Dropped Logs: %d (%.4f%%)\n", droppedLogs, float64(droppedLogs)/float64(totalLogs)*100)
fmt.Printf("Bytes Written: %d\n", bytesWritten)
fmt.Printf("Flushes: %d\n", flushes)
fmt.Printf("Flush Errors: %d\n", flushErrors)
fmt.Printf("Buffer Swaps: %d\n", setSwaps)

// Get detailed flush metrics
flushMetrics := logger.GetFlushMetrics()
fmt.Printf("Avg Flush Time: %.2fms\n", float64(flushMetrics.AvgFlushDuration.Microseconds())/1000.0)
fmt.Printf("Max Flush Time: %.2fms\n", float64(flushMetrics.MaxFlushDuration.Microseconds())/1000.0)

// Get per-shard statistics
shardStats := logger.GetShardStats()
for _, stat := range shardStats {
    fmt.Printf("Shard %d: %.1f%% utilized (%d writes)\n", 
        stat.ShardID, stat.UtilizationPct, stat.WriteCount)
}
```

### Periodic Monitoring Example

```go
// Monitor drop rate periodically
go func() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    for range ticker.C {
        total, dropped, _, _, _, _ := logger.GetStatsSnapshot()
        if total > 0 {
            dropRate := float64(dropped) / float64(total) * 100
            if dropRate > 0.01 {
                log.Printf("WARNING: High drop rate: %.4f%%", dropRate)
            }
        }
    }
}()
```

## Configuration Guide

### Default Configuration

```go
config := asynclogger.DefaultConfig("/var/log/app.log")
// BufferSize:    64MB  (baseline configuration)
// NumShards:     8     (optimal thread-to-shard ratio 1:1)
// FlushInterval: 10s   (balance between latency and throughput)
// FlushTimeout:  10ms  (wait for writes to complete)
```

### Custom Configuration

```go
import (
    "time"
    "github.com/yourorg/go-core/asynclogger"
)

config := asynclogger.Config{
    LogFilePath:   "/var/log/myapp.log",
    BufferSize:    64 * 1024 * 1024,  // 64MB
    NumShards:     8,
    FlushInterval: 10 * time.Second,
    FlushTimeout:  10 * time.Millisecond,
    UseMMap:       false,  // Set to true to use mmap-based allocation (Linux only)
}

logger, err := asynclogger.New(config)
if err != nil {
    log.Fatal(err)
}
defer logger.Close()
```

### MMap Mode (Experimental)

The logger supports an optional mmap-based buffer allocation mode that uses a single memory-mapped region split into virtual shards instead of separate allocations. This can provide better memory locality and potentially improved cache performance.

**To enable mmap mode:**

```go
config := asynclogger.DefaultConfig("/var/log/app.log")
config.UseMMap = true
logger, err := asynclogger.New(config)
```

**Platform Support:**
- **Linux**: Full support with native `syscall.Mmap()`
- **Other platforms**: Falls back to regular allocation (mmap mode disabled)

**Benefits:**
- Single allocation/deallocation (reduced overhead)
- Better memory locality (all shards in contiguous memory)
- Potential cache performance improvements
- Reduced memory fragmentation

**Considerations:**
- MMap mode is experimental and should be tested in your environment
- Performance characteristics may vary depending on workload
- Default mode (separate allocations) is well-tested and recommended for production

**Note:** When using mmap mode, the logger automatically frees the mmap regions on `Close()`. No manual cleanup is required.

## Direct I/O

### What is Direct I/O?

Direct I/O (`O_DIRECT`) bypasses the operating system's page cache, writing directly to disk. This provides:

- **Predictable write latency**: No cache eviction surprises
- **Consistent performance**: Not affected by other processes' I/O
- **Lower memory pressure**: No double-buffering in OS cache

### Platform Selection

The logger automatically selects the appropriate I/O implementation using Go build tags:

- **Linux**: Uses `directio_linux.go` with `O_DIRECT` and `O_DSYNC` flags for Direct I/O
- **Non-Linux** (macOS, Windows): Uses `directio_default.go` with standard file I/O (for testing)

The selection happens at compile time via build tags (`//go:build linux` and `//go:build !linux`), so no runtime checks are needed. Both implementations provide the same API, ensuring code compatibility across platforms.

### Requirements

- **Linux only**: Uses `syscall.O_DIRECT` (for production Direct I/O)
- **512-byte alignment**: All buffers and writes are automatically aligned
- **Block device**: Works best with physical disks and SSDs

### Trade-offs

✅ **Pros**:
- Predictable performance
- No cache pollution
- Lower system memory usage

⚠️ **Cons**:
- Slightly higher write latency (~10-20% increase)
- Linux-specific (no macOS/Windows support)

## Best Practices

### 1. Always Close the Logger

```go
logger, err := asynclogger.New(config)
if err != nil {
    log.Fatal(err)
}
defer logger.Close()  // Ensures all logs are flushed
```

### 2. Monitor Drop Rate

```go
// Periodically check drop rate
go func() {
    ticker := time.NewTicker(1 * time.Minute)
    for range ticker.C {
        total, dropped, _, _, _, _ := logger.GetStatsSnapshot()
        if total > 0 {
            dropRate := float64(dropped) / float64(total) * 100
            if dropRate > 0.01 {
                log.Printf("WARNING: High drop rate: %.4f%%", dropRate)
            }
        }
    }
}()
```

### 3. Size Buffers Appropriately

- **8MB** for most applications (8-20 concurrent writers)
- **16MB** for high-capacity systems (50+ writers)
- **4MB** for resource-constrained environments

### 4. Match Shards to Concurrency

```
Thread-to-Shard Ratio Guidelines:
- 1:1 ratio is optimal (e.g., 8 threads → 8 shards)
- Keep ratio ≤ 2:1 for best performance
- Avoid ratio > 12:1 (causes high contention)
```

### 5. Use LogBytes for Hot Paths

For high-frequency logging paths, use `LogBytes()` with reusable buffers to eliminate GC pressure:

```go
// Good: Zero allocations
var buf [256]byte
n := copy(buf[:], "message\n")
logger.LogBytes(buf[:n])

// Avoid: Allocations on every call
logger.Log("message")  // Creates string allocation
```

## Troubleshooting

### High Drop Rate (>0.01%)

**Symptoms**: Messages being discarded

**Solutions**:
1. Increase buffer size (8MB → 16MB)
2. Increase shard count (8 → 16)
3. Reduce concurrent writers if possible
4. Increase flush interval to reduce overhead

### Performance Issues

**Symptoms**: Slow logging, high latency

**Solutions**:
1. Verify Direct I/O is working (`cat /proc/<pid>/fdinfo/<fd>`)
2. Check disk I/O with `iostat -x 1`
3. Ensure SSD or fast disk is used
4. Monitor CPU usage (should be <5% for logging)

### Flush Errors

**Symptoms**: `flushErrors > 0` in statistics

**Solutions**:
1. Check disk space: `df -h`
2. Verify file permissions: `ls -l /var/log/app.log`
3. Check for disk errors: `dmesg | grep -i error`
4. Ensure Direct I/O is supported on filesystem (ext4, xfs)

## Testing

### Running Tests

Run comprehensive tests:

```bash
cd asynclogger
go test -v -race -cover
```

Run benchmarks:

```bash
go test -bench=. -benchmem
```

### Test Coverage

The test suite includes **19 test functions** with comprehensive coverage:

#### Configuration Tests
- **`TestConfig_Validate`** (4 sub-tests)
  - Valid config verification
  - Missing log path validation
  - Default value application
  - Shard size validation

#### Logger Basic Functionality Tests
- **`TestLogger_BasicLogging`** - Basic logging with file creation
- **`TestLogger_ConcurrentWrites`** - High concurrency (8, 50, 100 goroutines)
- **`TestLogger_BufferFillingAndSwapping`** - Buffer swap mechanism
- **`TestLogger_GracefulShutdown`** - Shutdown flush verification
- **`TestLogger_DoubleClose`** - Error handling (double close)
- **`TestLogger_Statistics`** - Statistics tracking verification
- **`TestLogger_MessageWithoutNewline`** - Message format handling

#### Buffer-Level Tests
- **`TestBuffer_Write`** - Buffer write with header reservation
- **`TestBuffer_FillAndFlush`** - Buffer fill threshold (90%)

#### Shard-Level Tests
- **`TestShard_ConcurrentWrites`** - Shard-level thread safety

#### BufferSet-Level Tests
- **`TestBufferSet_RoundRobin`** - Round-robin shard selection
- **`TestBufferSet_HasData`** - Data presence detection

#### API Tests
- **`TestLogger_LogBytes`** - LogBytes API functionality
- **`TestLogger_LogBytes_ZeroAllocation`** - Zero-allocation usage pattern
- **`TestLogger_LogBytes_ConcurrentWithReuse`** - Concurrent LogBytes with buffer reuse
- **`TestLogger_LogString_BackwardCompatible`** - String API compatibility
- **`TestLogger_MixedStringAndBytes`** - Mixed API usage

#### File Format Verification Tests
- **`TestLogger_FileFormatVerification`** - Comprehensive file format validation
  - ✅ 8-byte shard header structure verification
  - ✅ Data starts immediately after header (no padding)
  - ✅ Log entries have correct 4-byte length prefixes
  - ✅ **Data correctness** (what is written matches what is read)
  - ✅ End-to-end file format validation

### Test Coverage Areas

- ✅ **Configuration validation** - All config scenarios tested
- ✅ **Basic logging functionality** - Core logging operations
- ✅ **Concurrent writes** - 8, 50, and 100 goroutines
- ✅ **Buffer management** - Filling, swapping, flushing
- ✅ **Graceful shutdown** - Data flush on close
- ✅ **Error handling** - Double close, invalid config
- ✅ **Statistics tracking** - All metrics verified
- ✅ **Buffer operations** - Write, fill, flush
- ✅ **Shard operations** - Concurrent writes, thread safety
- ✅ **BufferSet operations** - Round-robin, data detection
- ✅ **API compatibility** - Log vs LogBytes, mixed usage
- ✅ **File format correctness** - Header structure, data layout
- ✅ **Data integrity** - Round-trip verification (write → read)

### Load Testing

For comprehensive load testing with Docker, use the provided load test script:

```bash
# Run baseline test (proto file and gRPC call are required)
./run_baseline_load_test.sh \
    --proto-file /path/to/service.proto \
    --grpc-call package.Service.Method

# Custom configuration
./run_baseline_load_test.sh \
    --proto-file service.proto \
    --grpc-call myapp.UserService.GetUser \
    --duration 5m \
    --rps 1500 \
    --threads 150 \
    --buffer-mb 128
```

**Prerequisites:**
- Docker installed and running
- `ghz` installed: `go install github.com/bojand/ghz/cmd/ghz@latest`
- A gRPC server Docker image that uses AsyncLogger
- Proto file for your gRPC service

**See `LOAD_TEST_GUIDE.md` for detailed instructions and configuration options.**

The load test script:
- Runs comprehensive performance tests with configurable parameters
- Collects AsyncLogger metrics (flush times, drop rates, throughput)
- Monitors resource usage (CPU, memory, I/O)
- Generates detailed reports (server logs, gRPC performance, resource timeline)

## Platform Support

- ✅ **Linux**: Full support with Direct I/O
- ❌ **macOS**: Not supported (requires different Direct I/O implementation)
- ❌ **Windows**: Not supported

## API Reference

### Logger Methods

- `New(config Config) (*Logger, error)` - Create a new logger instance
- `Log(message string)` - Log a string message (convenience API)
- `LogBytes(data []byte)` - Log raw bytes (high-performance API)
- `Close() error` - Gracefully shutdown and flush all logs
- `GetStatsSnapshot() (totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, setSwaps int64)` - Get current statistics
- `GetFlushMetrics() FlushMetrics` - Get detailed flush performance metrics
- `GetShardStats() []ShardStats` - Get per-shard statistics

### Configuration

```go
type Config struct {
    LogFilePath   string        // Path to log file (required)
    BufferSize    int           // Total buffer size in bytes (default: 64MB)
    NumShards     int           // Number of shards (default: 8)
    FlushInterval time.Duration // Time-based flush trigger (default: 10s)
    FlushTimeout  time.Duration // Max wait for writes to complete (default: 10ms)
    UseMMap       bool          // Use mmap-based allocation (default: false, Linux only)
}
```

## License

This package is intended for internal use within your organization.
