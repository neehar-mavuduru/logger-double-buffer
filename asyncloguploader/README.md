# Async Log Uploader Module

A high-performance, asynchronous logging system with integrated cloud storage upload capabilities. Uses a sharded double-buffer architecture optimized for Direct I/O operations, supporting multiple events with separate log files.

## Features

- **Sharded Double Buffer CAS**: Lock-free writes using Compare-and-Swap operations
- **Per-Shard Double Buffers**: Each shard has its own double buffer that swaps independently
- **Semaphore-Based Swap Coordination**: Ensures only one swap happens per shard when multiple writers see it full
- **Direct I/O**: Bypasses OS page cache for predictable performance
- **Anonymous mmap**: All buffers allocated via anonymous mmap for optimal performance
- **Round-Robin Shard Selection**: Simple atomic counter for shard selection
- **25% Threshold Flush**: Flushes when 25% of shards are ready (e.g., 2 out of 8 shards)
- **Batch Flush**: Single Pwritev syscall for all ready shards
- **Multiple Events**: Support for multiple event-based loggers with separate files
- **Size-Based Rotation**: File rotation based on size with fallocate preallocation
- **GCS Upload**: Integrated Google Cloud Storage uploader with parallel chunk uploads
- **Write Completion Guarantees**: Ensures all writes complete before flushing

## Architecture

### High-Level Design

The module consists of several key components:

1. **LoggerManager**: Manages multiple Logger instances, one per event
2. **Logger**: Core logging engine with semaphore-based swap coordination
3. **ShardCollection**: Collection of shards with 25% threshold tracking
4. **Shard**: Single merged struct combining Buffer and Shard functionality with double buffer
5. **FileWriter**: Direct I/O file writer with size-based rotation
6. **Uploader**: GCS upload service with chunk management

### Low-Level Design

- **Buffer Allocation**: Anonymous mmap only (OS-managed, page-aligned)
- **Shard Selection**: Round-robin only (atomic counter)
- **Swap Strategy**: Per-shard swap (each shard swaps independently)
- **Flush Strategy**: Batch flush (single syscall for all ready shards)
- **Shard Threshold**: Fixed at 25% (e.g., 2 out of 8 shards)
- **Swap Coordination**: Semaphore-based (30 permits) to coordinate multiple writers

## Configuration

```go
import "github.com/neeharmavuduru/logger-double-buffer/asyncloguploader"

// Create base configuration
config := asyncloguploader.DefaultConfig("/var/logs/app.log")
config.BufferSize = 64 * 1024 * 1024  // 64MB
config.NumShards = 8
config.MaxFileSize = 10 * 1024 * 1024 * 1024  // 10GB
config.PreallocateFileSize = 10 * 1024 * 1024 * 1024  // 10GB
config.FlushInterval = 10 * time.Second
config.FlushTimeout = 10 * time.Millisecond

// Optional: Configure GCS upload
if enableGCS {
    gcsConfig := asyncloguploader.DefaultGCSUploadConfig("my-bucket")
    gcsConfig.ObjectPrefix = "logs/"
    gcsConfig.ChunkSize = 32 * 1024 * 1024  // 32MB chunks
    config.GCSUploadConfig = &gcsConfig
    
    // Create upload channel
    uploadChan := make(chan string, 100)
    config.UploadChannel = uploadChan
}
```

## Usage

### Single Logger

```go
// Create logger
logger, err := asyncloguploader.NewLogger(config)
if err != nil {
    log.Fatal(err)
}
defer logger.Close()

// Write logs
logger.LogBytes([]byte("Hello, World!"))
logger.Log("Hello, World!")  // Convenience API

// Get statistics
stats := logger.GetStatsSnapshot()
fmt.Printf("Total logs: %d, Dropped: %d\n", stats.TotalLogs, stats.DroppedLogs)
```

### Multiple Events (LoggerManager)

The `LoggerManager` enables multi-event logging where each event writes to its own log file. This is ideal for applications that need to separate logs by event type (e.g., payment events, login events, search events).

#### Basic Usage

```go
package main

import (
    "log"
    "time"
    "github.com/neeharmavuduru/logger-double-buffer/asyncloguploader"
)

func main() {
    // Create base configuration
    // The LogFilePath determines the base directory for all event log files
    config := asyncloguploader.DefaultConfig("/var/logs/app.log")
    config.BufferSize = 64 * 1024 * 1024  // 64MB per event
    config.NumShards = 8
    config.MaxFileSize = 10 * 1024 * 1024 * 1024  // 10GB per file
    config.PreallocateFileSize = 10 * 1024 * 1024 * 1024  // 10GB preallocation
    config.FlushInterval = 10 * time.Second
    config.FlushTimeout = 10 * time.Millisecond

    // Create logger manager
    manager, err := asyncloguploader.NewLoggerManager(config)
    if err != nil {
        log.Fatalf("Failed to create logger manager: %v", err)
    }
    defer manager.Close()

    // Write logs with event names
    // Each event automatically gets its own logger and log file
    manager.LogBytesWithEvent("payment", []byte("Payment processed: $100.00"))
    manager.LogBytesWithEvent("login", []byte("User logged in: user@example.com"))
    manager.LogBytesWithEvent("search", []byte("Search query: 'golang tutorial'"))
    
    // Convenience API for string messages
    manager.LogWithEvent("payment", "Payment completed successfully")
    manager.LogWithEvent("login", "User logout")

    // Get aggregated statistics across all events
    totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, _ := manager.GetAggregatedStats()
    log.Printf("Total logs: %d, Dropped: %d, Bytes: %d, Flushes: %d, Errors: %d",
        totalLogs, droppedLogs, bytesWritten, flushes, flushErrors)
}
```

#### File Naming Convention

The `LoggerManager` creates separate log files for each event:

- **Base Directory**: Extracted from `config.LogFilePath` (directory part)
- **File Pattern**: `{baseDir}/{eventName}.log`
- **Example**: If `LogFilePath = "/var/logs/app.log"`, then:
  - Event "payment" → `/var/logs/payment.log`
  - Event "login" → `/var/logs/login.log`
  - Event "search" → `/var/logs/search.log`

**Note**: Event names are automatically sanitized:
- Invalid filesystem characters (`/`, `\`, `:`, `*`, `?`, `"`, `<`, `>`, `|`) are replaced with `_`
- Spaces are replaced with `_`
- Length is limited to 255 characters

#### Event Name Sanitization

```go
// These event names will be sanitized:
manager.LogWithEvent("payment/order", "data")     // → "payment_order.log"
manager.LogWithEvent("user login", "data")        // → "user_login.log"
manager.LogWithEvent("event:test", "data")        // → "event_test.log"

// Invalid event names will return errors:
manager.LogWithEvent("", "data")                  // Error: event name cannot be empty
```

#### Advanced Usage: Pre-initialize Event Loggers

You can pre-initialize loggers for specific events to avoid lazy creation overhead:

```go
// Initialize loggers for known events upfront
events := []string{"payment", "login", "search", "checkout"}
for _, event := range events {
    if err := manager.InitializeEventLogger(event); err != nil {
        log.Printf("Failed to initialize logger for %s: %v", event, err)
    }
}

// Now writes to these events will be faster (no lazy creation)
manager.LogBytesWithEvent("payment", []byte("Payment data"))
```

#### Event Logger Management

```go
// Check if an event logger exists
if manager.HasEventLogger("payment") {
    log.Println("Payment logger is active")
}

// List all active event loggers
activeEvents := manager.ListEventLoggers()
log.Printf("Active events: %v", activeEvents)

// Close a specific event logger (flushes and closes its file)
if err := manager.CloseEventLogger("payment"); err != nil {
    log.Printf("Failed to close payment logger: %v", err)
}
```

#### Statistics and Monitoring

```go
// Get aggregated statistics across all events
totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, _ := manager.GetAggregatedStats()
log.Printf("Aggregated Stats:")
log.Printf("  Total Logs: %d", totalLogs)
log.Printf("  Dropped Logs: %d (%.2f%%)", droppedLogs, 
    float64(droppedLogs)/float64(totalLogs)*100)
log.Printf("  Bytes Written: %d (%.2f GB)", bytesWritten, 
    float64(bytesWritten)/(1024*1024*1024))
log.Printf("  Flushes: %d", flushes)
log.Printf("  Flush Errors: %d", flushErrors)

// Get aggregated flush metrics
metrics := manager.GetAggregatedFlushMetrics()
log.Printf("Flush Metrics:")
log.Printf("  Avg Flush Duration: %v", metrics.AvgFlushDuration)
log.Printf("  Max Flush Duration: %v", metrics.MaxFlushDuration)
log.Printf("  Avg Write Duration: %v (%.1f%% of flush)", 
    metrics.AvgWriteDuration, metrics.WritePercent)
log.Printf("  Avg Pwritev Duration: %v (%.1f%% of flush)", 
    metrics.AvgPwritevDuration, metrics.PwritevPercent)
```

#### Complete Example: Multi-Event with GCS Upload

```go
package main

import (
    "fmt"
    "log"
    "time"
    "github.com/neeharmavuduru/logger-double-buffer/asyncloguploader"
)

func main() {
    // Create upload channel
    uploadChan := make(chan string, 100)

    // Configure GCS upload
    gcsConfig := asyncloguploader.DefaultGCSUploadConfig("my-log-bucket")
    gcsConfig.ObjectPrefix = "logs/production/"  // All events will be under this prefix
    gcsConfig.ChunkSize = 32 * 1024 * 1024      // 32MB chunks
    gcsConfig.MaxRetries = 3
    gcsConfig.RetryDelay = 5 * time.Second

    // Create base configuration
    config := asyncloguploader.DefaultConfig("/var/logs/app.log")
    config.BufferSize = 64 * 1024 * 1024
    config.NumShards = 8
    config.MaxFileSize = 1 * 1024 * 1024 * 1024      // 1GB per file
    config.PreallocateFileSize = 1 * 1024 * 1024 * 1024
    config.FlushInterval = 10 * time.Second
    config.FlushTimeout = 10 * time.Millisecond
    config.UploadChannel = uploadChan
    config.GCSUploadConfig = &gcsConfig

    // Create GCS uploader
    uploader, err := asyncloguploader.NewUploader(gcsConfig)
    if err != nil {
        log.Fatalf("Failed to create uploader: %v", err)
    }
    uploader.Start()
    defer uploader.Stop()

    // Create logger manager
    manager, err := asyncloguploader.NewLoggerManager(config)
    if err != nil {
        log.Fatalf("Failed to create logger manager: %v", err)
    }
    defer manager.Close()

    // Write logs to different events
    events := []string{"payment", "login", "search", "checkout"}
    for i := 0; i < 1000; i++ {
        for _, event := range events {
            data := []byte(fmt.Sprintf("Event %s: Log entry %d", event, i))
            manager.LogBytesWithEvent(event, data)
        }
    }

    // Wait for flushes to complete
    time.Sleep(2 * time.Second)

    // Get statistics
    totalLogs, droppedLogs, bytesWritten, flushes, flushErrors, _ := manager.GetAggregatedStats()
    log.Printf("Final Stats: Logs=%d, Dropped=%d, Bytes=%d, Flushes=%d, Errors=%d",
        totalLogs, droppedLogs, bytesWritten, flushes, flushErrors)

    // Get upload statistics
    uploadStats := uploader.GetStats()
    log.Printf("Upload Stats: Files=%d, Successful=%d, Failed=%d, Bytes=%d",
        uploadStats.TotalFiles, uploadStats.Successful, uploadStats.Failed, uploadStats.TotalBytes)
}
```

#### GCS Object Naming for Multi-Event

When using GCS upload with `LoggerManager`, completed files are uploaded with the following naming:

- **Pattern**: `{ObjectPrefix}{eventName}_{timestamp}.log`
- **Example**: If `ObjectPrefix = "logs/production/"` and event is "payment":
  - Local file: `/var/logs/payment.log` → `/var/logs/payment_2026-01-02_18-55-02.log` (on rotation)
  - GCS object: `logs/production/payment_2026-01-02_18-55-02.log`

Each event's files are uploaded independently and can be processed separately.

#### Best Practices

1. **Pre-initialize Known Events**: If you know all event types upfront, initialize them at startup:
   ```go
   events := []string{"payment", "login", "search"}
   for _, event := range events {
       manager.InitializeEventLogger(event)
   }
   ```

2. **Monitor Dropped Logs**: Check `droppedLogs` in aggregated stats to detect buffer pressure:
   ```go
   if droppedLogs > 0 {
       log.Printf("WARNING: %d logs dropped (%.2f%%)", droppedLogs, 
           float64(droppedLogs)/float64(totalLogs)*100)
   }
   ```

3. **Use Consistent Event Names**: Use consistent naming conventions (e.g., lowercase, underscores):
   ```go
   // Good
   manager.LogWithEvent("payment_success", "data")
   manager.LogWithEvent("payment_failure", "data")
   
   // Avoid
   manager.LogWithEvent("Payment Success", "data")  // Will become "Payment_Success"
   manager.LogWithEvent("payment-success", "data")   // Will become "payment-success"
   ```

4. **Graceful Shutdown**: Always call `Close()` to ensure all pending data is flushed:
   ```go
   defer manager.Close()  // Flushes all event loggers
   ```

5. **Resource Management**: Each event logger uses its own buffer (64MB default). For many events, consider:
   - Reducing `BufferSize` per event
   - Using fewer shards (`NumShards`)
   - Monitoring memory usage

### With GCS Upload

```go
// Create upload channel
uploadChan := make(chan string, 100)

// Configure GCS
gcsConfig := asyncloguploader.DefaultGCSUploadConfig("my-bucket")
gcsConfig.ObjectPrefix = "logs/event1/"
config.GCSUploadConfig = &gcsConfig
config.UploadChannel = uploadChan

// Create uploader
uploader, err := asyncloguploader.New(uploaderConfig)
if err != nil {
    log.Fatal(err)
}
uploader.Start()
defer uploader.Stop()

// Create logger with upload channel
logger, err := asyncloguploader.NewLogger(config)
if err != nil {
    log.Fatal(err)
}
defer logger.Close()

// Completed files will be automatically uploaded to GCS
```

## Design Decisions

### Single Merged Struct

The `Shard` struct combines Buffer and Shard functionality into a single struct:
- Contains two buffers (bufferA and bufferB) for double buffering
- Active buffer pointer atomically swapped
- Write completion tracking for both buffers
- Mutex only held during flush operations

### Anonymous mmap Only

All buffers are allocated via anonymous mmap:
- Page-aligned (4096 bytes) for Direct I/O
- GC-safe with `runtime.KeepAlive` and finalizers
- Reused after flush (not unmapped during normal operation)

### Per-Shard Swap with Semaphore Coordination

When multiple writers see a shard full:
1. First write attempt (lock-free)
2. If fails, acquire semaphore permit (non-blocking with 10ms timeout)
3. Re-check if swap already happened
4. If not, perform swap (CAS-protected)
5. Retry write to newly swapped buffer
6. Release semaphore

This ensures:
- Only one swap happens per shard
- Other writers wait for swap completion
- Non-blocking hot path (timeout if semaphore busy)

### 25% Threshold Flush

Flush is triggered when 25% of shards are ready:
- For 8 shards: threshold = 2 shards
- For 4 shards: threshold = 1 shard
- All ready shards flushed together in single Pwritev syscall

### Round-Robin Shard Selection

Simple atomic counter for round-robin selection:
```go
shardIdx := counter.Add(1) % numShards
```

## Performance Considerations

- **Lock-Free Hot Path**: Writes use CAS operations, no mutexes
- **Batch Flush**: Single syscall for multiple shards reduces overhead
- **Direct I/O**: Bypasses page cache for predictable latency
- **Preallocation**: fallocate preallocates files to avoid extent allocation during writes
- **Write Completion Tracking**: Ensures all writes complete before flush

## File Structure

```
asyncloguploader/
├── config.go              # Simplified configuration
├── shard.go               # Single merged Shard struct with double buffer
├── shard_collection.go    # Collection with 25% threshold and round-robin
├── logger.go              # Main logger with semaphore-based swap coordination
├── logger_manager.go      # Multiple event logger manager
├── file_writer.go         # File writer interface
├── file_writer_linux.go   # Linux Direct I/O with size-based rotation
├── file_writer_default.go # Non-Linux fallback
├── uploader.go            # GCS uploader
├── chunk_manager.go       # Chunk manager for 32-chunk limit
└── README.md              # This file
```

## Requirements

- Go 1.21 or later
- Linux for Direct I/O support (non-Linux systems use fallback)
- Google Cloud Storage client library (for GCS upload)

## License

[Your License Here]
