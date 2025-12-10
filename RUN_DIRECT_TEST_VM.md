# Running Direct Logger Test on VM

## Overview

This guide explains how to run the direct logger test on a GCP VM and check the results. This test eliminates ghz and provides pure logger performance measurement.

## Prerequisites

1. **GCP VM Setup** (if not already done):
   ```bash
   bash scripts/setup_gcp_vm.sh
   ```

2. **Clone Repository** (if not already cloned):
   ```bash
   git clone <your-repo-url>
   cd logger-double-buffer
   ```

## Step 1: Build the Test Program

```bash
# Build the direct logger test
go build -o bin/direct_logger_test ./cmd/direct_logger_test
```

## Step 2: Run the Test

### Basic Test (Single Logger)

```bash
bash scripts/run_direct_logger_test_vm.sh \
    --threads 100 \
    --rps 1000 \
    --duration 10m \
    --buffer-mb 64 \
    --shards 8
```

### Event-Based Test

```bash
bash scripts/run_direct_logger_test_vm.sh \
    --use-events \
    --event test \
    --threads 100 \
    --rps 1000 \
    --duration 10m
```

### Custom Configuration

```bash
bash scripts/run_direct_logger_test_vm.sh \
    --threads 200 \
    --rps 2000 \
    --duration 5m \
    --buffer-mb 128 \
    --shards 16 \
    --log-size-kb 300
```

## Step 3: Monitor Test Progress

The test runs in the foreground and shows real-time metrics every 5 seconds:

```
METRICS: Logs: 50000 Dropped: 0 (0.0000%) | Bytes: 15000000000 | Flushes: 50 Errors: 0 Swaps: 50 | AvgFlush: 32.45ms MaxFlush: 45.23ms | GC: 12 cycles 15.23ms pause | Mem: 128.45MB
```

**To run in background:**
```bash
nohup bash scripts/run_direct_logger_test_vm.sh --threads 100 --rps 1000 --duration 10m > test_output.log 2>&1 &
```

**To monitor progress:**
```bash
tail -f results/direct_logger_test/test_*.log
```

## Step 4: Check Results

### Results Location

All results are saved to: `results/direct_logger_test/`

### Files Generated

1. **Test Log**: `test_TIMESTAMP.log`
   - Full test output with all metrics
   - Final statistics summary

2. **Resource Timeline**: `resource_timeline_TIMESTAMP.csv`
   - CPU, memory, disk I/O over time
   - 1-second intervals

3. **Log Files**: `logs/`
   - Generated log files (`.log` files)

### Quick Results Check

```bash
# View final metrics
tail -20 results/direct_logger_test/test_*.log | grep "METRICS"

# View final summary
tail -30 results/direct_logger_test/test_*.log | grep -A 10 "Final Statistics"

# Check for flush errors
grep "FLUSH_ERROR" results/direct_logger_test/test_*.log | wc -l
```

### Detailed Analysis

#### 1. Extract Final Metrics

```bash
# Get the last METRICS line
grep "METRICS:" results/direct_logger_test/test_*.log | tail -1
```

**Output format:**
```
METRICS: Logs: 600000 Dropped: 0 (0.0000%) | Bytes: 180000000000 | Flushes: 60 Errors: 0 Swaps: 60 | AvgFlush: 32.12ms MaxFlush: 45.67ms | GC: 15 cycles 18.45ms pause | Mem: 132.34MB
```

**Metrics explained:**
- **Logs**: Total logs written
- **Dropped**: Logs dropped (should be 0)
- **Drop Rate**: Percentage dropped
- **Bytes**: Total bytes written
- **Flushes**: Number of flush operations
- **Errors**: Flush errors (should be 0)
- **Swaps**: Buffer set swaps
- **AvgFlush**: Average flush duration (ms)
- **MaxFlush**: Maximum flush duration (ms)
- **GC cycles**: Garbage collection cycles
- **Mem**: Memory usage (MB)

#### 2. Analyze Resource Usage

```bash
# View resource timeline CSV
head -20 results/direct_logger_test/resource_timeline_*.csv

# Calculate average CPU
awk -F',' 'NR>1 {sum+=$2; count++} END {print "Avg CPU:", sum/count "%"}' results/direct_logger_test/resource_timeline_*.csv

# Calculate average memory
awk -F',' 'NR>1 {sum+=$3; count++} END {print "Avg Memory:", sum/count "MB"}' results/direct_logger_test/resource_timeline_*.csv

# Calculate peak memory
awk -F',' 'NR>1 {if ($3>max) max=$3} END {print "Peak Memory:", max "MB"}' results/direct_logger_test/resource_timeline_*.csv
```

#### 3. Check Flush Performance

```bash
# Extract all flush durations
grep "METRICS:" results/direct_logger_test/test_*.log | \
    sed 's/.*AvgFlush: \([0-9.]*\)ms.*/\1/' | \
    awk '{sum+=$1; count++} END {print "Average Flush:", sum/count "ms"}'

# Find maximum flush duration
grep "METRICS:" results/direct_logger_test/test_*.log | \
    sed 's/.*MaxFlush: \([0-9.]*\)ms.*/\1/' | \
    sort -n | tail -1
```

#### 4. Check Drop Rate

```bash
# Extract drop rate from final metrics
grep "METRICS:" results/direct_logger_test/test_*.log | tail -1 | \
    sed 's/.*Dropped: \([0-9]*\) (\([0-9.]*\)%).*/\1 logs (\2%)/'
```

#### 5. Analyze Log Files

```bash
# Check log file size
ls -lh logs/*.log

# Count log entries (if parser available)
go run scripts/parse_log_file.go logs/*.log
```

## Step 5: Compare Results

### Expected Performance (Baseline)

With **64MB buffer, 8 shards, 100 threads, 1000 RPS**:

- **Drop Rate**: 0%
- **Avg Flush**: ~30-50ms
- **Max Flush**: ~50-100ms
- **Memory**: ~100-150MB
- **GC Cycles**: <20 cycles

### Comparison Script

Create a simple comparison:

```bash
# Compare two test runs
echo "=== Test Run 1 ==="
grep "METRICS:" results/direct_logger_test/test_TIMESTAMP1.log | tail -1

echo ""
echo "=== Test Run 2 ==="
grep "METRICS:" results/direct_logger_test/test_TIMESTAMP2.log | tail -1
```

## Step 6: Download Results (Optional)

If you want to download results to your local machine:

```bash
# From your local machine
gcloud compute scp vm-name:~/logger-double-buffer/results/direct_logger_test results/ --recurse
```

Or use `rsync`:

```bash
rsync -avz vm-name:~/logger-double-buffer/results/direct_logger_test/ results/direct_logger_test/
```

## Troubleshooting

### Test Fails to Start

```bash
# Check if test program exists
ls -lh bin/direct_logger_test

# Check permissions
chmod +x bin/direct_logger_test

# Try running directly
./bin/direct_logger_test --help
```

### High Drop Rate

- **Check buffer size**: Increase `--buffer-mb` if needed
- **Check RPS**: Reduce if too high for buffer size
- **Check disk space**: `df -h`
- **Check disk I/O**: `iostat -x 1 5`

### High Flush Duration

- **Check disk I/O**: `iostat -x 1 5`
- **Check CPU**: `top` or `htop`
- **Check memory**: `free -h`
- **Check for swap**: `swapon --show`

### Test Hangs

```bash
# Find test process
ps aux | grep direct_logger_test

# Kill if needed
kill -9 <PID>
```

## Quick Reference

### Common Commands

```bash
# Run test
bash scripts/run_direct_logger_test_vm.sh --threads 100 --rps 1000 --duration 10m

# Check latest results
ls -lt results/direct_logger_test/ | head -5

# View final metrics
tail -20 results/direct_logger_test/test_*.log | grep "METRICS"

# Monitor resource usage
tail -f results/direct_logger_test/resource_timeline_*.csv

# Check for errors
grep -i error results/direct_logger_test/test_*.log
```

### Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `--threads` | 100 | Number of concurrent threads |
| `--rps` | 1000 | Target requests per second |
| `--duration` | 10m | Test duration |
| `--log-size-kb` | 300 | Log size in KB |
| `--buffer-mb` | 64 | Buffer size in MB |
| `--shards` | 8 | Number of shards |
| `--use-events` | false | Use event-based logging |
| `--event` | test | Event name |

## Next Steps

1. **Baseline Test**: Run with default configuration
2. **Compare**: Compare with ghz test results
3. **Tune**: Adjust buffer size and shards based on results
4. **Scale**: Test different RPS levels to find limits



