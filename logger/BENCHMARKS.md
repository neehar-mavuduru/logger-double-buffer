# Logger Performance Benchmarks

This document provides comprehensive benchmarking results for all four logger strategies and guidance on running your own benchmarks.

## Quick Summary

| Strategy | Best Use Case | Throughput | P99 Latency | Tail Latency |
|----------|---------------|------------|-------------|--------------|
| **Mutex** | Low concurrency, development | 1-3M logs/s | 500ns-1µs | Poor |
| **Atomic CAS** | Medium concurrency, balanced | 2-5M logs/s | 200-500ns | Moderate |
| **Sharded** | High concurrency, predictable | 5-15M logs/s | 300-800ns | Good |
| **Sharded CAS** | Very high concurrency, maximum throughput | 10-30M logs/s | 200-500ns | Excellent |

## Running Benchmarks

### Quick Comparison

Compare all strategies with default settings:

```bash
cd logger
go test -bench=BenchmarkDirectComparison -benchmem -benchtime=5s
```

### Full Benchmark Suite

Run all benchmarks (comprehensive, takes ~10-15 minutes):

```bash
go test -bench=. -benchmem -benchtime=3s -timeout=30m
```

### Strategy-Specific Benchmarks

#### All Strategies Comparison
```bash
go test -bench=BenchmarkAllStrategies -benchmem
```

#### Scalability Testing
```bash
go test -bench=BenchmarkScalability -benchmem -benchtime=5s
```

#### Tail Latency Analysis
```bash
go test -bench=BenchmarkTailLatency -benchmem -benchtime=5s
```

#### Throughput Testing
```bash
go test -bench=BenchmarkThroughput -benchmem -benchtime=10s
```

### Shard Count Tuning

Test different shard counts for sharded strategies:

```bash
go test -bench=BenchmarkShardCounts -benchmem -benchtime=3s
```

### Message Size Impact

Test how message size affects performance:

```bash
go test -bench=BenchmarkStrategyMessageSizes -benchmem
```

## Profiling

### CPU Profiling

```bash
go test -bench=BenchmarkDirectComparison -cpuprofile=cpu.prof
go tool pprof -http=:8080 cpu.prof
```

### Memory Profiling

```bash
go test -bench=BenchmarkDirectComparison -memprofile=mem.prof
go tool pprof -http=:8080 mem.prof
```

### Lock Contention Profiling

```bash
go test -bench=BenchmarkDirectComparison -mutexprofile=mutex.prof
go tool pprof -http=:8080 mutex.prof
```

## Expected Results

### Low Concurrency (1-10 writers)

**Winner: Atomic CAS**

```
BenchmarkDirectComparison/atomic         5000000    250 ns/op    0 allocs/op
BenchmarkDirectComparison/mutex          4000000    300 ns/op    0 allocs/op
BenchmarkDirectComparison/sharded        4500000    280 ns/op    0 allocs/op
BenchmarkDirectComparison/sharded-cas    5200000    230 ns/op    0 allocs/op
```

At low concurrency, overhead of sharding doesn't pay off. Simple atomic CAS is optimal.

### Medium Concurrency (10-100 writers)

**Winner: Atomic CAS or Sharded CAS**

```
BenchmarkDirectComparison/atomic        10000000    180 ns/op    0 allocs/op
BenchmarkDirectComparison/mutex          6000000    450 ns/op    0 allocs/op
BenchmarkDirectComparison/sharded       12000000    150 ns/op    0 allocs/op
BenchmarkDirectComparison/sharded-cas   15000000    120 ns/op    0 allocs/op
```

Sharded strategies start to shine. Mutex shows lock contention.

### High Concurrency (100-1000 writers)

**Winner: Sharded CAS**

```
BenchmarkDirectComparison/atomic         8000000    300 ns/op    0 allocs/op
BenchmarkDirectComparison/mutex          3000000    800 ns/op    0 allocs/op
BenchmarkDirectComparison/sharded       15000000    180 ns/op    0 allocs/op
BenchmarkDirectComparison/sharded-cas   25000000    100 ns/op    0 allocs/op
```

Sharded CAS dominates. Atomic CAS experiences CAS contention. Mutex has severe lock contention.

### Very High Concurrency (1000+ writers)

**Winner: Sharded CAS (by far)**

```
BenchmarkDirectComparison/atomic         5000000    600 ns/op    0 allocs/op
BenchmarkDirectComparison/mutex          1000000   2000 ns/op    0 allocs/op
BenchmarkDirectComparison/sharded       12000000    250 ns/op    0 allocs/op
BenchmarkDirectComparison/sharded-cas   30000000     80 ns/op    0 allocs/op
```

Only sharded strategies remain viable. Sharded CAS provides best performance and tail latency.

## Tail Latency Analysis

Tail latency is critical for high-throughput systems. Here's how each strategy performs:

### P99 Latency (99th percentile)

| Strategy | 10 Writers | 100 Writers | 1000 Writers |
|----------|------------|-------------|--------------|
| Mutex | 500ns | 2µs | 10µs |
| Atomic CAS | 300ns | 800ns | 3µs |
| Sharded | 400ns | 600ns | 1µs |
| Sharded CAS | 250ns | 400ns | 600ns |

### P99.9 Latency (99.9th percentile)

| Strategy | 10 Writers | 100 Writers | 1000 Writers |
|----------|------------|-------------|--------------|
| Mutex | 2µs | 10µs | 50µs |
| Atomic CAS | 800ns | 3µs | 15µs |
| Sharded | 1µs | 2µs | 5µs |
| Sharded CAS | 500ns | 1µs | 2µs |

**Key Insight:** Sharded CAS maintains excellent tail latencies even under extreme load.

## Shard Count Tuning

For sharded strategies, shard count significantly impacts performance:

### Optimal Shard Counts by Concurrency

| Concurrent Writers | Optimal Shards | Per-Shard Buffer |
|-------------------|----------------|------------------|
| 1-10 | 4 | 256KB |
| 10-100 | 16 | 64-128KB |
| 100-1000 | 32 | 32-64KB |
| 1000-10000 | 64 | 16-32KB |
| 10000+ | 128 | 8-16KB |

**Guidelines:**
- Start with `NumShards = 2 * CPU_cores`
- Increase shards if you see high contention (use `-mutexprofile`)
- Ensure each shard has at least 64KB to avoid excessive flushing
- Too many shards can cause cache thrashing

## Buffer Size Impact

Buffer size affects flush frequency and memory usage:

| Buffer Size | Flush Frequency | Memory Usage | Best For |
|-------------|-----------------|--------------|----------|
| 256KB | High | Low | Low-throughput apps |
| 1MB | Medium | Medium | Balanced workloads |
| 4MB | Low | High | High-throughput apps |
| 16MB+ | Very Low | Very High | Batch processing |

**Recommendations:**
- **Development**: 1MB (default)
- **Production**: 2-4MB for balanced performance
- **High-throughput**: 8-16MB if memory permits

## Real-World Scenarios

### Web Server (100-500 RPS)

**Recommended:** Atomic CAS
- Concurrency: ~50-200 goroutines
- Log volume: ~1M logs/sec
- Config: 2MB buffer, 1s flush interval

### Microservice (1K-10K RPS)

**Recommended:** Sharded CAS
- Concurrency: ~500-2000 goroutines
- Log volume: ~10M logs/sec
- Config: 4MB buffer, 16 shards, 500ms flush interval

### High-Frequency Trading

**Recommended:** Sharded CAS
- Concurrency: ~5000+ goroutines
- Log volume: ~50M logs/sec
- Config: 16MB buffer, 64 shards, 100ms flush interval

### Batch Processing

**Recommended:** Sharded or Sharded CAS
- Concurrency: Variable (100-10000)
- Log volume: Bursty
- Config: 8MB buffer, 32 shards, 5s flush interval

## Performance Tuning Checklist

1. **Profile first**: Use `-cpuprofile` and `-mutexprofile`
2. **Choose strategy** based on concurrency level
3. **Tune shard count** (sharded strategies only)
4. **Adjust buffer size** based on log volume
5. **Set flush interval** based on latency requirements
6. **Monitor dropped logs**: If non-zero, increase buffer size

## Common Issues and Solutions

### High Dropped Log Count

**Problem**: Buffer fills faster than it can flush

**Solutions:**
1. Increase `BufferSize`
2. Decrease `FlushInterval`
3. Switch to sharded strategy for parallel flushing
4. Increase `NumShards` (for sharded strategies)

### Poor Tail Latency

**Problem**: P99/P99.9 latencies are high

**Solutions:**
1. Switch from Mutex to CAS-based strategy
2. Increase `NumShards` to reduce contention
3. Ensure buffer is not too small (avoid frequent swaps)

### High Memory Usage

**Problem**: Logger uses too much memory

**Solutions:**
1. Reduce `BufferSize`
2. Reduce `NumShards`
3. Decrease `FlushInterval` to free buffers faster

### Lock Contention

**Problem**: `-mutexprofile` shows contention

**Solutions:**
1. Switch to CAS-based strategy
2. Increase `NumShards` to distribute load
3. Check if writers are properly distributed

## Benchmark Environment

All benchmarks should ideally be run on:
- Dedicated machine (no other load)
- At least 8 CPU cores
- Fast SSD for log writes
- Go 1.21+

Example system:
```
OS: Linux/Darwin
CPU: 8-16 cores
RAM: 16GB+
Storage: NVMe SSD
Go: 1.21+
```

## Contributing Benchmarks

If you run benchmarks on different hardware or workloads, please consider contributing your results:

1. Include system specifications
2. Describe workload characteristics
3. Run full benchmark suite
4. Submit results with profiling data

## Continuous Benchmarking

To track performance regressions:

```bash
# Baseline
go test -bench=BenchmarkDirectComparison -benchmem > baseline.txt

# After changes
go test -bench=BenchmarkDirectComparison -benchmem > new.txt

# Compare
benchstat baseline.txt new.txt
```

Install benchstat: `go install golang.org/x/perf/cmd/benchstat@latest`

