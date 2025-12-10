# Direct Logger Test - No ghz Required

## Overview

A pure logger performance test that eliminates ghz and resource competition. This test directly exercises the logger with configurable threads, RPS, and log sizes.

## Why Use This?

**Problems with ghz-based testing:**
- Resource competition (CPU, memory, disk I/O)
- Indirect effects on flush duration
- Complex setup with gRPC server
- Hard to isolate logger performance

**Benefits of direct test:**
- ✅ No resource competition
- ✅ Pure logger performance measurement
- ✅ Exact control over write rate
- ✅ Simpler setup (no server required)
- ✅ Isolated logger behavior

## Quick Start

### Single Logger Test

```bash
# 100 threads, 1000 RPS, 10 minutes, 300KB logs
bash scripts/run_direct_logger_test.sh \
    --threads 100 \
    --rps 1000 \
    --duration 10m \
    --log-size-kb 300 \
    --buffer-mb 64 \
    --shards 8
```

### Event-Based Test

```bash
# Test LoggerManager with event-based logging
bash scripts/run_direct_logger_test.sh \
    --use-events \
    --threads 100 \
    --rps 1000 \
    --duration 10m
```

## Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `--threads` | 100 | Number of concurrent threads |
| `--rps` | 1000 | Target requests per second (total) |
| `--duration` | 10m | Test duration |
| `--log-size-kb` | 300 | Log size in KB per entry |
| `--buffer-mb` | 64 | Buffer size in MB |
| `--shards` | 8 | Number of shards |
| `--flush-interval` | 10s | Flush interval |
| `--use-events` | false | Use LoggerManager (event-based) |
| `--log-dir` | logs | Log directory |

## How It Works

1. **Rate Limiting**: Each thread writes at `RPS / threads` rate
   - Example: 1000 RPS / 100 threads = 10 RPS per thread
   - Interval: 100ms between writes per thread

2. **Concurrent Writes**: All threads write simultaneously
   - Each thread writes 300KB log entries
   - Logger handles concurrent writes via sharded buffers

3. **Statistics**: Real-time metrics every 5 seconds
   - Logs written, dropped, bytes
   - Flush count, errors, swaps
   - Average/max flush duration
   - GC cycles and memory usage

## Example Output

```
METRICS: Logs: 50000 Dropped: 0 (0.0000%) | Bytes: 15000000000 | Flushes: 50 Errors: 0 Swaps: 50 | AvgFlush: 32.45ms MaxFlush: 45.23ms | GC: 12 cycles 15.23ms pause | Mem: 128.45MB
```

## Test Scenarios

### Baseline Performance (Single Logger)

```bash
bash scripts/run_direct_logger_test.sh \
    --threads 100 \
    --rps 1000 \
    --duration 10m \
    --buffer-mb 64 \
    --shards 8
```

**Expected:**
- 0% drop rate
- ~30-50ms average flush duration
- Consistent performance

### High Load Test

```bash
bash scripts/run_direct_logger_test.sh \
    --threads 200 \
    --rps 2000 \
    --duration 5m \
    --buffer-mb 128 \
    --shards 16
```

### Event-Based Test (3 Events)

You can run multiple instances with different event names:

```bash
# Terminal 1: Event 1 (350 RPS)
bash scripts/run_direct_logger_test.sh \
    --use-events \
    --threads 35 \
    --rps 350 \
    --duration 10m \
    --event event1 &

# Terminal 2: Event 2 (350 RPS)
bash scripts/run_direct_logger_test.sh \
    --use-events \
    --threads 35 \
    --rps 350 \
    --duration 10m \
    --event event2 &

# Terminal 3: Event 3 (300 RPS)
bash scripts/run_direct_logger_test.sh \
    --use-events \
    --threads 30 \
    --rps 300 \
    --duration 10m \
    --event event3 &
```

## Results Location

Results are saved to: `results/direct_logger_test/`

- `test_TIMESTAMP.log` - Full test output with metrics
- `logs/` - Generated log files

## Comparison with ghz Test

| Metric | ghz Test | Direct Test |
|--------|----------|-------------|
| Resource Competition | High (CPU, memory, disk) | None |
| Setup Complexity | High (server + ghz) | Low (just logger) |
| Accuracy | Indirect effects | Pure logger |
| Flush Duration | ~1220ms (with ghz) | ~30-50ms (expected) |
| Drop Rate | ~51% (with ghz) | ~0% (expected) |

## Troubleshooting

### Build Error: "main redeclared"

The test program is in `cmd/direct_logger_test/`. Make sure you're building from the correct location:

```bash
go build -o bin/direct_logger_test ./cmd/direct_logger_test
```

### High Drop Rate

- Check buffer size: Increase `--buffer-mb` if needed
- Check shard count: More shards = less contention
- Check RPS: Reduce if too high for buffer size

### High Flush Duration

- Check disk I/O: Use `iostat` or `iotop`
- Check CPU: Use `top` or `htop`
- Check memory: Use `free` or `vmstat`

## Next Steps

1. Run baseline test to establish performance baseline
2. Compare with ghz test results
3. Tune buffer size and shards based on results
4. Test different RPS levels to find limits



