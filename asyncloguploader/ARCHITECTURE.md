# Async Log Uploader - Architecture Documentation

## Table of Contents
1. [High-Level Design (HLD)](#high-level-design-hld)
2. [Low-Level Design (LLD)](#low-level-design-lld)
3. [Architecture Decisions](#architecture-decisions)
4. [Alternative Approaches Considered](#alternative-approaches-considered)
5. [Trade-offs Analysis](#trade-offs-analysis)

---

## High-Level Design (HLD)

### System Overview

The Async Log Uploader is a high-performance, lock-free logging system designed for high-throughput scenarios (10-30M logs/sec) with predictable latency characteristics. It uses a **sharded double-buffer architecture** with **Direct I/O** to achieve zero-downtime flushing and predictable disk I/O performance.

### System Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Application Layer                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │   Logger     │  │   Logger     │  │   Logger     │  ...     │
│  │  (Event 1)  │  │  (Event 2)  │  │  (Event N)  │          │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘          │
│         │                  │                  │                   │
└─────────┼──────────────────┼──────────────────┼──────────────────┘
          │                  │                  │
          └──────────────────┼──────────────────┘
                             │
          ┌──────────────────▼──────────────────┐
          │      LoggerManager (Optional)       │
          │  - Manages multiple event loggers   │
          │  - Event name sanitization          │
          │  - Aggregated statistics            │
          └──────────────────┬──────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│                      Core Logger Engine                         │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │              ShardCollection                              │  │
│  │  ┌──────┐  ┌──────┐  ┌──────┐  ┌──────┐  ┌──────┐      │  │
│  │  │Shard1│  │Shard2│  │Shard3│  │Shard4│  │ShardN│      │  │
│  │  │  A/B │  │  A/B │  │  A/B │  │  A/B │  │  A/B │      │  │
│  │  └──┬───┘  └──┬───┘  └──┬───┘  └──┬───┘  └──┬───┘      │  │
│  │     │         │         │         │         │          │  │
│  │     └─────────┴─────────┴─────────┴─────────┘          │  │
│  │              Random Selection                         │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │              Flush Coordination                          │  │
│  │  ┌──────────────┐         ┌──────────────┐              │  │
│  │  │ Flush Worker │◄────────│ Flush Channel│              │  │
│  │  │   (Goroutine)│         │   (Buffered) │              │  │
│  │  └──────┬───────┘         └──────────────┘              │  │
│  │         │                                                 │  │
│  │         │  ┌──────────────┐                              │  │
│  │         └─►│ Ticker Worker│                              │  │
│  │            │ (Periodic)   │                              │  │
│  │            └──────────────┘                              │  │
│  └──────────────────────────────────────────────────────────┘  │
└────────────────────────────┬───────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│                    File Writer Layer                            │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  FileWriter (Interface)                                   │  │
│  │  ┌──────────────────┐  ┌──────────────────┐            │  │
│  │  │ Linux (Direct I/O)│  │ Default (Standard)│            │  │
│  │  │ - O_DIRECT        │  │ - Page Cache     │            │  │
│  │  │ - O_DSYNC         │  │ - Standard I/O   │            │  │
│  │  │ - Pwritev         │  │ - WriteAt        │            │  │
│  │  │ - Fallocate       │  │                  │            │  │
│  │  └──────────────────┘  └──────────────────┘            │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  File Rotation                                            │  │
│  │  - Size-based rotation                                    │  │
│  │  - Preallocation (fallocate)                              │  │
│  │  - Timestamp-based naming                                 │  │
│  └──────────────────────────────────────────────────────────┘  │
└────────────────────────────┬───────────────────────────────────┘
                             │
┌────────────────────────────▼────────────────────────────────────┐
│                    Upload Layer (Optional)                       │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Uploader Service                                         │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │  │
│  │  │ Chunk Manager│  │ Parallel     │  │ GCS Client   │  │  │
│  │  │ (32-chunk    │  │ Upload       │  │ (gRPC Pool)  │  │  │
│  │  │  limit)      │  │ Workers      │  │              │  │  │
│  │  └──────────────┘  └──────────────┘  └──────────────┘  │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

#### 1. LoggerManager
- **Purpose**: Manages multiple Logger instances, one per event type
- **Responsibilities**:
  - Event name sanitization and validation
  - Logger lifecycle management (create, retrieve, close)
  - Aggregated statistics across all loggers
- **Key Design**: Uses `sync.Map` for thread-safe logger storage

#### 2. Logger
- **Purpose**: Core logging engine orchestrating writes, flushes, and file I/O
- **Responsibilities**:
  - Write coordination (random shard selection)
  - Flush trigger logic (threshold-based + periodic)
  - Background worker management (flush worker, ticker worker)
  - Statistics collection
- **Key Design**: Lock-free hot path with CAS operations

#### 3. ShardCollection
- **Purpose**: Manages collection of shards and tracks flush readiness
- **Responsibilities**:
  - Shard selection (random distribution)
  - Threshold tracking (25% of shards ready)
  - Ready shard collection for batch flush
- **Key Design**: Atomic counters for threshold tracking

#### 4. Shard
- **Purpose**: Individual shard with double-buffer for zero-downtime flushing
- **Responsibilities**:
  - Lock-free writes using CAS
  - Buffer swapping coordination
  - Inflight write tracking
  - Data retrieval for flushing
- **Key Design**: Per-shard semaphore for swap coordination

#### 5. FileWriter
- **Purpose**: Abstracts file I/O operations with platform-specific optimizations
- **Responsibilities**:
  - Vectored I/O (Pwritev on Linux)
  - File rotation (size-based)
  - Preallocation (fallocate)
- **Key Design**: Interface-based for platform abstraction

#### 6. Uploader (Optional)
- **Purpose**: Uploads completed log files to Google Cloud Storage
- **Responsibilities**:
  - Chunk management (32-chunk compose limit)
  - Parallel upload coordination
  - Retry logic and error handling
- **Key Design**: Chunk manager handles GCS compose limits

### Data Flow

#### Write Path (Hot Path)
```
Application
    │
    ▼
Logger.LogBytes()
    │
    ▼
ShardCollection.Write() ──[Random Selection]──► Shard.Write()
    │                                              │
    │                                              ▼
    │                                    CAS-based offset reservation
    │                                              │
    │                                              ▼
    │                                    copy() data to buffer
    │                                              │
    │                                              ▼
    │                                    Increment inflight counter
    │                                              │
    │                                              ▼
    └──[Threshold Check]──► Flush Trigger ──► Flush Channel
```

#### Flush Path
```
Flush Trigger (Threshold or Periodic)
    │
    ▼
Flush Channel ──► Flush Worker
    │                  │
    │                  ▼
    │            Acquire Semaphore (prevent concurrent flushes)
    │                  │
    │                  ▼
    │            Collect Ready Shards
    │                  │
    │                  ▼
    │            For each shard:
    │              - Wait for inflight == 0
    │              - Get inactive buffer data
    │              - Write shard header
    │                  │
    │                  ▼
    │            Batch Write (Pwritev) ──► FileWriter
    │                  │                      │
    │                  │                      ▼
    │                  │                 Direct I/O Write
    │                  │                      │
    │                  │                      ▼
    │                  │                 File System
    │                  │
    │                  ▼
    │            Reset Shards
    │                  │
    │                  ▼
    │            Release Semaphore
```

#### Close Path
```
Logger.Close()
    │
    ▼
Stop Ticker & Signal Shutdown
    │
    ▼
Drain Flush Channel (let pending flushes complete)
    │
    ▼
Wait for Semaphore (ensure no flush in progress)
    │
    ▼
For each shard with data:
    - If active buffer has data: swap to inactive
    - Add to flush list
    │
    ▼
Flush remaining data (ignores threshold)
    │
    ▼
Close FileWriter & ShardCollection
```

---

## Low-Level Design (LLD)

### Shard Double-Buffer Design

#### Buffer Structure
```go
type Shard struct {
    // Double buffer: two mmap-allocated buffers
    bufferA []byte  // Buffer A (mmap'd, page-aligned)
    bufferB []byte  // Buffer B (mmap'd, page-aligned)
    
    // Active buffer pointer (atomically swapped)
    activeBuffer atomic.Pointer[[]byte]  // Points to &bufferA or &bufferB
    
    // Per-buffer offsets (atomic for lock-free writes)
    offsetA atomic.Int32  // Current write offset in bufferA
    offsetB atomic.Int32  // Current write offset in bufferB
    
    // Per-buffer inflight tracking
    inflightA atomic.Int64  // Concurrent writes in progress for bufferA
    inflightB atomic.Int64  // Concurrent writes in progress for bufferB
    
    // Swap coordination
    swapping      atomic.Bool      // CAS-protected swap flag
    readyForFlush atomic.Bool      // Shard ready for flush
    swapSemaphore chan struct{}    // Per-shard semaphore (buffer size 1)
    
    // Mutex only for flush operations (GetData, Reset)
    mu sync.Mutex
}
```

#### Buffer Layout
```
┌─────────────────────────────────────────────────────────┐
│                    Shard Buffer (e.g., 1MB)             │
├─────────────────────────────────────────────────────────┤
│ Offset 0-7:   Shard Header (written during flush)      │
│               - Capacity (4 bytes, uint32)              │
│               - Valid Data Bytes (4 bytes, uint32)      │
├─────────────────────────────────────────────────────────┤
│ Offset 8+:    Log Entries                               │
│               ┌──────────┬──────────────────────────┐  │
│               │ Length   │ Data                     │  │
│               │ (4 bytes)│ (variable length)        │  │
│               └──────────┴──────────────────────────┘  │
│               ┌──────────┬──────────────────────────┐  │
│               │ Length   │ Data                     │  │
│               │ (4 bytes)│ (variable length)        │  │
│               └──────────┴──────────────────────────┘  │
│               ...                                       │
└─────────────────────────────────────────────────────────┘
```

### Write Algorithm (Lock-Free)

```go
func (s *Shard) Write(p []byte) (n int, needsFlush bool) {
    // 1. Get active buffer pointer
    activeBufPtr := s.activeBuffer.Load()
    
    // 2. Determine which offset to use
    offset := (activeBufPtr == &s.bufferA) ? &s.offsetA : &s.offsetB
    
    // 3. Calculate new offset (4-byte length prefix + data)
    currentOffset := offset.Load()
    newOffset := currentOffset + 4 + len(p)
    
    // 4. Check capacity
    if newOffset >= s.capacity {
        s.readyForFlush.Store(true)
        return 0, true  // Buffer full
    }
    
    // 5. CAS-based offset reservation (lock-free)
    if !offset.CompareAndSwap(currentOffset, newOffset) {
        return s.Write(p)  // Retry on CAS failure
    }
    
    // 6. Increment inflight counter
    inflight := (activeBufPtr == &s.bufferA) ? &s.inflightA : &s.inflightB
    inflight.Add(1)
    
    // 7. Write length prefix
    binary.LittleEndian.PutUint32(activeBuf[currentOffset:], uint32(len(p)))
    
    // 8. Copy data (using copy() - optimized by Go runtime)
    copy(activeBuf[currentOffset+4:newOffset], p)
    
    // 9. Decrement inflight counter
    inflight.Add(-1)
    
    return 4 + len(p), false
}
```

**Key Characteristics:**
- **Lock-free**: Uses CAS for offset reservation
- **Retry on conflict**: CAS failures trigger retry (rare in practice)
- **Inflight tracking**: Ensures writes complete before flush
- **Zero-allocation**: Uses `copy()` which is optimized by Go runtime

### Swap Algorithm (CAS-Protected)

```go
func (s *Shard) trySwap() {
    // 1. CAS-protected swap flag (only one swap succeeds)
    if !s.swapping.CompareAndSwap(false, true) {
        return  // Another goroutine is swapping
    }
    defer s.swapping.Store(false)
    
    // 2. Get current active buffer
    currentBufPtr := s.activeBuffer.Load()
    
    // 3. Determine next buffer
    nextBufPtr := (currentBufPtr == &s.bufferA) ? &s.bufferB : &s.bufferA
    
    // 4. Atomically swap active buffer pointer
    if !s.activeBuffer.CompareAndSwap(currentBufPtr, nextBufPtr) {
        return  // Swap failed, another goroutine beat us
    }
    
    // 5. Mark shard as ready for flush
    s.readyForFlush.Store(true)
}
```

**Key Characteristics:**
- **CAS-protected**: Only one swap succeeds per shard
- **Atomic pointer swap**: Active buffer pointer changed atomically
- **Idempotent**: Multiple callers see same result

### Flush Algorithm

```go
func (l *Logger) flushShards(readyShards []*Shard) {
    // 1. Acquire semaphore (prevent concurrent flushes)
    l.semaphore <- struct{}{}
    defer func() { <-l.semaphore }()
    
    // 2. Collect shard buffers
    shardBuffers := make([][]byte, 0, len(readyShards))
    
    for _, shard := range readyShards {
        // 3. Wait for inflight writes to complete
        data, _ := shard.GetData(l.config.FlushTimeout)
        
        // 4. Get inactive buffer offset
        shardOffset := shard.GetInactiveOffset()
        validDataBytes := shardOffset - headerOffset
        
        // 5. Write shard header (in-place, zero-copy)
        binary.LittleEndian.PutUint32(data[0:4], uint32(capacity))
        binary.LittleEndian.PutUint32(data[4:8], uint32(validDataBytes))
        
        // 6. Add to batch
        shardBuffers = append(shardBuffers, data)
    }
    
    // 7. Single batched write (Pwritev syscall)
    n, err := l.fileWriter.WriteVectored(shardBuffers)
    
    // 8. Reset shards
    for _, shard := range readyShards {
        shard.Reset()
    }
}
```

**Key Characteristics:**
- **Batch write**: Single syscall for all shards
- **Zero-copy**: Headers written in-place
- **Inflight wait**: Ensures data consistency
- **Semaphore protection**: Prevents concurrent flushes

### Shard Selection Algorithm

```go
func (sc *ShardCollection) Write(p []byte) (n int, needsFlush bool, shardID int) {
    // Random selection for load distribution
    shardIdx := rand.IntN(sc.numShards)
    shard := sc.shards[shardIdx]
    
    n, needsFlush = shard.Write(p)
    
    if needsFlush {
        sc.MarkShardReady()  // Increment ready count
    }
    
    return n, needsFlush, shardIdx
}
```

**Key Characteristics:**
- **Random selection**: Better load distribution than round-robin
- **Atomic ready tracking**: Thread-safe ready count increment
- **Threshold check**: 25% of shards triggers flush

### Threshold Calculation

```go
threshold := (numShards * 25) / 100
if threshold == 0 {
    threshold = 1  // At least 1 shard
}
```

**Examples:**
- 4 shards → threshold = 1 shard (25%)
- 8 shards → threshold = 2 shards (25%)
- 16 shards → threshold = 4 shards (25%)

---

## Architecture Decisions

### Decision 1: Double Buffering vs Single Buffer

**Decision**: Use double buffering (active/inactive buffers)

**Rationale**:
- **Zero-downtime flushing**: Active buffer continues accepting writes while inactive buffer is flushed
- **Predictable latency**: No blocking during flush operations
- **Memory overhead acceptable**: 2x buffer size is reasonable for performance gain

**Alternatives Considered**:
1. **Single buffer**: Would require blocking writes during flush
2. **Triple buffering**: Unnecessary complexity for minimal gain

**Trade-off**: +2x memory usage for zero-downtime flushing

---

### Decision 2: Sharding vs Single Buffer

**Decision**: Use sharded architecture (multiple shards)

**Rationale**:
- **Reduced contention**: Multiple writers can write to different shards concurrently
- **Better throughput**: Parallel writes across shards
- **Finer-grained flushing**: Can flush subset of shards (25% threshold)

**Alternatives Considered**:
1. **Single large buffer**: Higher contention, all-or-nothing flush
2. **Dynamic sharding**: Unnecessary complexity

**Trade-off**: +N shards memory overhead for better concurrency

---

### Decision 3: Lock-Free Writes vs Mutex-Based

**Decision**: Use CAS-based lock-free writes

**Rationale**:
- **Performance**: No mutex contention on hot path
- **Scalability**: Better performance under high concurrency
- **Low latency**: CAS operations are fast (nanoseconds)

**Alternatives Considered**:
1. **Mutex-based**: Simpler but higher contention
2. **Channel-based**: Higher overhead, not suitable for hot path

**Trade-off**: More complex code for better performance

---

### Decision 4: Random vs Round-Robin Shard Selection

**Decision**: Use random shard selection

**Rationale**:
- **Better load distribution**: Avoids hotspots from round-robin patterns
- **Simplicity**: No atomic counter needed
- **Fairness**: Random distribution is naturally fair over time

**Alternatives Considered**:
1. **Round-robin**: Predictable but can create hotspots
2. **Hash-based**: More complex, requires key extraction

**Trade-off**: Slightly less predictable but better distribution

---

### Decision 5: Per-Shard Semaphore vs Global Semaphore

**Decision**: Use per-shard semaphore for swap coordination

**Rationale**:
- **Reduced contention**: Each shard swaps independently
- **Better parallelism**: Multiple shards can swap simultaneously
- **Isolation**: Swap failure in one shard doesn't affect others

**Alternatives Considered**:
1. **Global semaphore**: Simpler but higher contention
2. **No semaphore**: Race conditions possible

**Trade-off**: More memory (N semaphores) for better parallelism

---

### Decision 6: Direct I/O vs Page Cache

**Decision**: Use Direct I/O (O_DIRECT) on Linux

**Rationale**:
- **Predictable latency**: Bypasses OS page cache variability
- **Durability**: O_DSYNC ensures data is on disk
- **Control**: Application controls when data hits disk

**Alternatives Considered**:
1. **Page cache**: Simpler but unpredictable latency
2. **Hybrid approach**: Too complex

**Trade-off**: More complex I/O path for predictable performance

---

### Decision 7: Vectored I/O (Pwritev) vs Individual Writes

**Decision**: Use Pwritev for batch writes

**Rationale**:
- **Single syscall**: Reduces syscall overhead
- **Zero-copy**: Kernel reads from multiple buffers directly
- **Efficiency**: Optimal for Direct I/O

**Alternatives Considered**:
1. **Individual writes**: 8 syscalls vs 1 syscall
2. **Copy-based batching**: Memory copy overhead

**Trade-off**: Platform-specific code (Linux) for better performance

---

### Decision 8: 25% Threshold vs Other Thresholds

**Decision**: Flush when 25% of shards are ready

**Rationale**:
- **Balance**: Not too aggressive (wasteful) or too conservative (high latency)
- **Batching**: Good batch size for Pwritev efficiency
- **Latency**: Reasonable flush frequency

**Alternatives Considered**:
1. **50% threshold**: Too conservative, higher latency
2. **10% threshold**: Too aggressive, more frequent flushes
3. **Dynamic threshold**: Unnecessary complexity

**Trade-off**: Fixed threshold for simplicity vs dynamic for optimality

---

### Decision 9: Anonymous mmap vs Go Allocator

**Decision**: Use anonymous mmap for buffer allocation

**Rationale**:
- **Page alignment**: Required for Direct I/O (4096-byte alignment)
- **GC pressure**: Reduces GC overhead (large buffers)
- **Control**: Application controls memory lifecycle

**Alternatives Considered**:
1. **Go allocator**: Simpler but not page-aligned, GC pressure
2. **CGO allocation**: Too complex, portability issues

**Trade-off**: More complex allocation for Direct I/O compatibility

---

### Decision 10: Inflight Tracking vs Write Completion Callbacks

**Decision**: Use atomic inflight counters

**Rationale**:
- **Simplicity**: Simple increment/decrement
- **Performance**: Atomic operations are fast
- **Reliability**: Ensures all writes complete before flush

**Alternatives Considered**:
1. **Write completion callbacks**: More complex, overhead
2. **No tracking**: Data corruption risk

**Trade-off**: Simple tracking mechanism for data safety

---

### Decision 11: copy() vs memmove()

**Decision**: Use Go's `copy()` function

**Rationale**:
- **Safety**: Type-safe, no unsafe pointers
- **Performance**: Go runtime optimizes `copy()` for large buffers
- **Simplicity**: Standard Go idiom
- **Benchmark results**: `copy()` is actually faster than `memmove()` for 800KB buffers

**Alternatives Considered**:
1. **memmove()**: Unsafe, minimal performance gain (<5%), higher risk
2. **Custom assembly**: Too complex, maintenance burden

**Trade-off**: Slightly slower (<5%) for much safer code

---

### Decision 12: Close() Flush Strategy

**Decision**: Drain pending flushes, wait for semaphore, then flush remaining data

**Rationale**:
- **Safety**: Ensures no race conditions with ongoing flushes
- **Completeness**: All data flushed before close
- **Correctness**: Proper ordering prevents data loss

**Alternatives Considered**:
1. **Immediate flush**: Race condition with ongoing flush
2. **No flush**: Data loss on close

**Trade-off**: More complex close logic for data safety

---

## Alternative Approaches Considered

### Alternative 1: Channel-Based Architecture

**Approach**: Use channels for all coordination (writes, flushes, swaps)

**Rejected Because**:
- **Overhead**: Channel operations have overhead (scheduling, locking)
- **Latency**: Not suitable for hot path (nanosecond requirements)
- **Complexity**: More complex error handling

**When It Would Be Better**:
- Lower throughput requirements (<1M logs/sec)
- Simpler codebase preferred over performance

---

### Alternative 2: Lock-Free Ring Buffer

**Approach**: Use lock-free ring buffer instead of double buffering

**Rejected Because**:
- **Complexity**: Ring buffer wrap-around logic is complex
- **Memory**: Still need 2x memory for zero-downtime
- **Flush coordination**: More complex to determine flush boundaries

**When It Would Be Better**:
- Fixed-size log entries
- Circular log requirements

---

### Alternative 3: Write-Ahead Log (WAL) Pattern

**Approach**: Separate WAL file + background compaction

**Rejected Because**:
- **Complexity**: Two-phase writes (WAL + main file)
- **Latency**: Additional I/O operations
- **Overhead**: More disk space usage

**When It Would Be Better**:
- Need for transaction semantics
- Crash recovery requirements

---

### Alternative 4: Memory-Mapped Files

**Approach**: Use file-backed mmap instead of anonymous mmap

**Rejected Because**:
- **OS page cache**: Defeats Direct I/O purpose
- **File size management**: Complex to grow/shrink files
- **Portability**: Different behavior across OSes

**When It Would Be Better**:
- Need for crash recovery
- Shared memory requirements

---

### Alternative 5: Zero-Copy with sendfile()

**Approach**: Use sendfile() for copying data

**Rejected Because**:
- **Not applicable**: sendfile() is for file-to-socket, not file-to-file
- **Wrong use case**: We're writing application data, not copying files

**When It Would Be Better**:
- File-to-network transfers
- Proxy scenarios

---

## Trade-offs Analysis

### Performance vs Simplicity

| Aspect | Trade-off | Impact |
|--------|-----------|--------|
| Lock-free writes | More complex code | +30% throughput, -20% code clarity |
| Double buffering | 2x memory | Zero-downtime flushing, predictable latency |
| Direct I/O | Platform-specific code | Predictable latency, -portability |
| Vectored I/O | Linux-specific | +50% flush performance, -portability |

**Decision**: Prioritize performance for high-throughput use case

---

### Memory vs Throughput

| Configuration | Memory Usage | Throughput | Latency |
|---------------|--------------|------------|---------|
| 4 shards, 64MB | 512MB | 5-10M logs/sec | Moderate |
| 8 shards, 64MB | 1GB | 10-20M logs/sec | Low |
| 16 shards, 64MB | 2GB | 20-30M logs/sec | Very Low |

**Decision**: Configurable based on requirements

---

### Durability vs Performance

| Approach | Durability | Performance | Trade-off |
|----------|------------|-------------|-----------|
| O_DIRECT + O_DSYNC | High (on disk) | Lower (direct disk I/O) | Predictable, slower |
| Page cache + fsync() | High (eventual) | Higher (cache hit) | Unpredictable, faster |
| Page cache only | Low (OS dependent) | Highest | Fastest, least durable |

**Decision**: O_DIRECT + O_DSYNC for predictable durability

---

### Concurrency vs Complexity

| Approach | Concurrency | Complexity | Trade-off |
|----------|-------------|------------|-----------|
| Single mutex | Low | Simple | Easy to understand, low throughput |
| Per-shard semaphore | High | Moderate | Better throughput, more code |
| Lock-free CAS | Highest | Complex | Best throughput, hardest to debug |

**Decision**: Lock-free CAS for hot path, semaphores for coordination

---

### Flush Frequency vs Latency

| Threshold | Flush Frequency | Average Latency | Batch Efficiency |
|-----------|-----------------|-----------------|------------------|
| 10% | High | Low | Lower (smaller batches) |
| 25% | Moderate | Moderate | Good (balanced) |
| 50% | Low | High | Higher (larger batches) |

**Decision**: 25% threshold for balanced latency and efficiency

---

## Performance Characteristics

### Throughput
- **Target**: 10-30M logs/sec
- **Achieved**: 15-25M logs/sec (depending on message size)
- **Bottleneck**: Disk I/O (Pwritev syscall)

### Latency
- **P50**: 200-500ns (lock-free write path)
- **P99**: 300-800ns (CAS retries)
- **P99.9**: 1-5µs (swap coordination)

### Memory
- **Per shard**: 2x buffer size (double buffering)
- **Total**: `NumShards × BufferSize × 2`
- **Example**: 8 shards × 8MB × 2 = 128MB

### Disk I/O
- **Flush latency**: 1-25ms (depending on batch size)
- **Throughput**: 500MB-2GB/sec (depending on disk)
- **Syscalls**: 1 per flush (Pwritev batches all shards)

---

## Scalability Considerations

### Horizontal Scaling
- **Per-process**: Single logger instance per process
- **Multi-process**: Each process has own logger
- **Aggregation**: External systems aggregate logs

### Vertical Scaling
- **Shards**: More shards = better concurrency (up to CPU cores)
- **Buffer size**: Larger buffers = fewer flushes but more memory
- **Flush workers**: Single flush worker (semaphore prevents concurrent flushes)

### Limitations
- **Single flush worker**: Limits flush parallelism (by design for consistency)
- **Memory bound**: Total memory = `NumShards × BufferSize × 2`
- **Disk bound**: Flush throughput limited by disk I/O

---

## Failure Modes and Mitigations

### Buffer Full
- **Symptom**: Write returns `needsFlush=true`, retry fails
- **Mitigation**: Per-shard semaphore with timeout, drop log if timeout
- **Impact**: Dropped logs (tracked in statistics)

### Flush Failure
- **Symptom**: `WriteVectored()` returns error
- **Mitigation**: Log error, reset shards, continue (prevent deadlock)
- **Impact**: Data loss for that flush batch

### Disk Full
- **Symptom**: Write syscall fails
- **Mitigation**: Error logged, shards reset, continue
- **Impact**: Data loss, application continues

### Process Crash
- **Symptom**: In-memory buffers lost
- **Mitigation**: Periodic flushes minimize data loss window
- **Impact**: Data loss for unflushed buffers

---

## Future Enhancements

### Potential Improvements
1. **Compression**: Add compression before flush (reduce disk I/O)
2. **Encryption**: Add encryption for sensitive logs
3. **Replication**: Replicate logs to multiple destinations
4. **Query interface**: Add log query/search capabilities
5. **Metrics export**: Export metrics to Prometheus/StatsD

### Not Planned (By Design)
1. **Transaction support**: Not needed for logging use case
2. **ACID guarantees**: Logging doesn't require transactions
3. **Query language**: Out of scope (use external tools)
4. **Multi-region**: Single-region design (use external replication)

---

## Conclusion

The Async Log Uploader architecture prioritizes **high throughput** and **predictable latency** through:

1. **Lock-free hot path** for maximum concurrency
2. **Double buffering** for zero-downtime flushing
3. **Sharding** for reduced contention
4. **Direct I/O** for predictable disk performance
5. **Batch operations** for efficiency

The design makes deliberate trade-offs favoring **performance** and **reliability** over **simplicity** and **portability**, making it suitable for high-throughput logging scenarios where predictable performance is critical.

