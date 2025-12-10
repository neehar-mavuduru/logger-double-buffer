# Running Direct Logger Test on Local Disk VM

## Quick Start

Since you've already cloned the repo and run `setup_gcp_vm.sh`, follow these steps:

### Step 1: Verify Setup

```bash
# Check Go is installed
go version

# Check you're in the project directory
cd ~/logger-double-buffer
pwd

# Verify test program exists
ls -la cmd/direct_logger_test/main.go
```

### Step 2: Build the Test Program

```bash
# Build the direct logger test
go build -o bin/direct_logger_test ./cmd/direct_logger_test

# Verify it was built
ls -lh bin/direct_logger_test
```

### Step 3: Run the Test

**Basic Test (Recommended for first run):**

```bash
bash scripts/run_direct_logger_test_vm.sh \
    --threads 100 \
    --rps 1000 \
    --duration 5m \
    --buffer-mb 32 \
    --shards 8
```

**Full Test (10 minutes, 64MB buffer):**

```bash
bash scripts/run_direct_logger_test_vm.sh \
    --threads 100 \
    --rps 1000 \
    --duration 10m \
    --buffer-mb 64 \
    --shards 8
```

**Quick Test (1 minute, for quick verification):**

```bash
bash scripts/run_direct_logger_test_vm.sh \
    --threads 100 \
    --rps 1000 \
    --duration 1m \
    --buffer-mb 32 \
    --shards 8
```

### Step 4: Monitor Progress

The test will show real-time metrics every 5 seconds:

```
METRICS: Logs: 50000 Dropped: 0 (0.0000%) | Bytes: 15000000000 | Flushes: 50 Errors: 0 Swaps: 50 | AvgFlush: 15.23ms MaxFlush: 25.45ms | AvgWrite: 2.34ms MaxWrite: 5.67ms WritePct: 15.4% | GC: 5 cycles 0.12ms pause | Mem: 72.45MB
```

**Key metrics to watch:**
- **AvgFlush**: Should be much lower (~15-50ms) on local disk vs network disk (~200ms)
- **WritePct**: Should be lower (~10-30%) vs network disk (100%)
- **Dropped**: Should be 0 or very low

### Step 5: Check Results

**View final metrics:**
```bash
# Get the latest test log
LATEST_LOG=$(ls -t results/direct_logger_test/test_*.log | head -1)

# View final metrics
tail -30 "$LATEST_LOG" | grep "METRICS"

# View final summary
tail -50 "$LATEST_LOG" | grep -A 20 "Final Statistics"
```

**Extract key metrics:**
```bash
# Get final metrics line
grep "METRICS:" "$LATEST_LOG" | tail -1

# Extract average flush duration
grep "METRICS:" "$LATEST_LOG" | tail -1 | sed 's/.*AvgFlush: \([0-9.]*\)ms.*/\1/'

# Extract drop rate
grep "METRICS:" "$LATEST_LOG" | tail -1 | sed 's/.*Dropped: \([0-9]*\) (\([0-9.]*\)%).*/\2/'
```

**Check resource usage:**
```bash
# View resource timeline
LATEST_RESOURCE=$(ls -t results/direct_logger_test/resource_timeline_*.csv | head -1)
head -20 "$LATEST_RESOURCE"

# Calculate average CPU
awk -F',' 'NR>1 {sum+=$2; count++} END {print "Avg CPU:", sum/count "%"}' "$LATEST_RESOURCE"

# Calculate average memory
awk -F',' 'NR>1 {sum+=$3; count++} END {print "Avg Memory:", sum/count "MB"}' "$LATEST_RESOURCE"
```

## Expected Performance on Local Disk

With **local SSD disk** (vs network disk):

| Metric | Network Disk | Local Disk (Expected) | Improvement |
|--------|--------------|----------------------|-------------|
| **AvgFlush** | ~200ms | ~15-50ms | **4-13x faster** |
| **AvgWrite** | ~200ms | ~2-10ms | **20-100x faster** |
| **WritePct** | 100% | 10-30% | **70-90% reduction** |
| **Drop Rate** | ~1-5% | <0.1% | **Much lower** |
| **Throughput** | ~294 MB/s | ~500+ MB/s | **~70% increase** |

## Troubleshooting

### Build Fails

```bash
# Check Go version (need 1.18+)
go version

# Download dependencies
go mod download

# Clean and rebuild
go clean -cache
go build -o bin/direct_logger_test ./cmd/direct_logger_test
```

### Test Fails to Start

```bash
# Check permissions
chmod +x bin/direct_logger_test

# Try running directly
./bin/direct_logger_test --help

# Check disk space
df -h

# Check if logs directory exists
mkdir -p logs
```

### High Drop Rate

```bash
# Check disk I/O
iostat -x 1 5

# Check disk space
df -h

# Increase buffer size
bash scripts/run_direct_logger_test_vm.sh --buffer-mb 64 --threads 100 --rps 1000 --duration 5m
```

### High Flush Duration

If flush duration is still high (~200ms), check:

```bash
# Verify you're using local disk (not network)
lsblk -d -o name,rota,type

# Check disk type (should show local SSD)
df -h | grep -E "^/dev"

# Check disk I/O performance
sudo hdparm -tT /dev/sda  # Replace sda with your disk
```

## Quick Reference Commands

```bash
# Run test
bash scripts/run_direct_logger_test_vm.sh --threads 100 --rps 1000 --duration 5m --buffer-mb 32

# Check latest results
ls -lt results/direct_logger_test/ | head -5

# View final metrics
tail -30 results/direct_logger_test/test_*.log | grep "METRICS"

# Monitor in real-time (if running in background)
tail -f results/direct_logger_test/test_*.log

# Check for errors
grep -i error results/direct_logger_test/test_*.log
```

## Next Steps

1. **Run baseline test**: Start with 5-minute test to verify setup
2. **Compare results**: Compare local disk vs network disk performance
3. **Tune configuration**: Adjust buffer size and shards based on results
4. **Run longer test**: Once verified, run 10-minute test for comprehensive results



