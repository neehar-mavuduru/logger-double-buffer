# VM Test Execution Steps

Detailed step-by-step guide for running tests on VM.

## Pre-Test Setup

### Step 1: Connect to VM

```bash
# SSH into your VM
ssh user@vm-ip-address

# Or if using GCP
gcloud compute ssh vm-instance-name --zone=us-central1-a
```

### Step 2: Navigate to Repository

```bash
cd /path/to/logger-double-buffer
# Or wherever you cloned the repository
```

### Step 3: Verify Environment

```bash
# Check Go version (should be 1.21+)
go version

# Check filesystem type (should be ext4 for O_DIRECT)
df -T | grep $(df . | tail -1 | awk '{print $1}')

# Check disk space (should have at least 50GB free)
df -h .

# Check you're on Linux
uname -a
```

## Test 1: Single Logger

### Step 1: Build Binary

```bash
go build -o bin/direct_logger_test ./cmd/direct_logger_test
```

**Expected output:**
```
# No errors, binary created
```

**Verify:**
```bash
ls -lh bin/direct_logger_test
# Should show executable file
```

### Step 2: Run Test

**Option A: Default Configuration (10 minutes)**
```bash
./scripts/run_single_logger_test_vm.sh
```

**Option B: Custom Configuration**
```bash
./scripts/run_single_logger_test_vm.sh \
    --duration 5m \
    --threads 200 \
    --rps 2000 \
    --buffer-mb 128 \
    --shards 16 \
    --rotation-interval 1h
```

**Expected output:**
```
════════════════════════════════════════════════════════════
    Single Logger Test - VM Execution                      
════════════════════════════════════════════════════════════

Test Configuration:
  Duration: 10m
  Threads: 100
  Target RPS: 1000
  Buffer: 64MB, Shards: 8
  Log Size: 300KB
  Rotation Interval: 24h

Starting resource monitoring...
✓ Resource monitoring started (PID: 12345)
Cleaning up old log files...
✓ Cleanup complete
Starting single logger test...
✓ Test started (PID: 12346)
  Monitor logs: tail -f results/single_logger_test_.../test_output.log
```

### Step 3: Monitor Progress (Optional)

**In another terminal:**

```bash
# Watch metrics
tail -f results/single_logger_test_*/test_output.log | grep METRICS

# Watch resources
watch -n 5 'tail -1 results/single_logger_test_*/resource_monitor.log'
```

**Expected metrics output (every 5 seconds):**
```
METRICS: Logs: 5000 Dropped: 5 (0.1000%) | Bytes: 1500000000 | Flushes: 12 Errors: 0 Swaps: 15 | AvgFlush: 45.23ms MaxFlush: 78.45ms | AvgWrite: 44.98ms MaxWrite: 78.12ms WritePct: 99.4% | GC: 2 cycles 0.12ms pause | Mem: 72.31MB
```

### Step 4: Wait for Completion

Test will run for the specified duration (default: 10 minutes).

**You'll see:**
```
✓ Test completed
Results saved to: results/single_logger_test_20250106_120000/
  Summary: results/single_logger_test_20250106_120000/summary.txt
  Test output: results/single_logger_test_20250106_120000/test_output.log
  Resource monitor: results/single_logger_test_20250106_120000/resource_monitor.log
  Log file: logs/direct_test.log
```

### Step 5: Review Results

```bash
# View summary
cat results/single_logger_test_*/summary.txt

# Check final metrics
grep "Final Metrics" results/single_logger_test_*/test_output.log

# Check log file size
ls -lh logs/direct_test.log

# Check for rotated files (if rotation occurred)
ls -lh logs/direct_test_*.log
```

**Expected summary:**
```
════════════════════════════════════════════════════════════
    Single Logger Test Results
    Timestamp: Mon Jan  6 12:10:00 UTC 2025
════════════════════════════════════════════════════════════

Configuration:
  Duration: 10m
  Threads: 100
  Target RPS: 1000
  Buffer: 64MB, Shards: 8
  Log Size: 300KB
  Rotation Interval: 24h

════════════════════════════════════════════════════════════
Results:
════════════════════════════════════════════════════════════

METRICS: Logs: 600000 Dropped: 500 (0.0833%) | Bytes: 180000000000 | Flushes: 450 Errors: 0 Swaps: 2105 | AvgFlush: 45.23ms MaxFlush: 78.45ms | AvgWrite: 44.98ms MaxWrite: 78.12ms WritePct: 99.4% | GC: 5 cycles 0.24ms pause | Mem: 72.31MB

════════════════════════════════════════════════════════════
Resource Usage Summary:
════════════════════════════════════════════════════════════
Average CPU: 8.45%
Average Memory: 12.34%
Average Disk Read: 0.12 MB/s
Average Disk Write: 294.56 MB/s
Average Disk IOPS: 1250.34
```

## Test 2: Multi-Event Logger

### Step 1: Build Binary (if not already built)

```bash
go build -o bin/direct_logger_test ./cmd/direct_logger_test
```

### Step 2: Run Test

**Option A: Default Configuration (10 minutes, 3 events)**
```bash
./scripts/run_multi_event_test_vm.sh
```

**Option B: Custom Configuration**
```bash
./scripts/run_multi_event_test_vm.sh \
    --duration 5m \
    --threads 200 \
    --event1-rps 400 \
    --event2-rps 400 \
    --event3-rps 200 \
    --rotation-interval 1h
```

**Expected output:**
```
════════════════════════════════════════════════════════════
    Multi-Event Logger Test - VM Execution                  
════════════════════════════════════════════════════════════

Test Configuration:
  Duration: 10m
  Total Threads: 100
  Total RPS: 1000
  Buffer: 64MB, Shards: 8
  Log Size: 300KB
  Rotation Interval: 24h

Event Distribution:
  event1: 350 RPS, 35 threads
  event2: 350 RPS, 35 threads
  event3: 300 RPS, 30 threads

Starting resource monitoring...
✓ Resource monitoring started (PID: 12345)
Cleaning up old log files...
✓ Cleanup complete
Starting event1 test...
✓ event1 started (PID: 12346)
Starting event2 test...
✓ event2 started (PID: 12347)
Starting event3 test...
✓ event3 started (PID: 12348)

All tests started. Waiting for completion...
  Monitor logs: tail -f results/multi_event_test_*/event*_test.log
```

### Step 3: Monitor Progress (Optional)

**In another terminal:**

```bash
# Watch all events
tail -f results/multi_event_test_*/event*_test.log | grep METRICS

# Watch specific event
tail -f results/multi_event_test_*/event1_test.log

# Watch resources
tail -f results/multi_event_test_*/resource_monitor.log
```

### Step 4: Wait for Completion

All three event tests will run concurrently for the specified duration.

**You'll see:**
```
✓ Test completed
Results saved to: results/multi_event_test_20250106_120000/
  Summary: results/multi_event_test_20250106_120000/summary.txt
  Event logs: results/multi_event_test_20250106_120000/*_test.log
  Resource monitor: results/multi_event_test_20250106_120000/resource_monitor.log
  Log files: logs/event*.log
```

### Step 5: Review Results

```bash
# View summary
cat results/multi_event_test_*/summary.txt

# Check per-event metrics
grep "Final Metrics" results/multi_event_test_*/event*_test.log

# Check log files
ls -lh logs/event*.log

# Check for rotated files
ls -lh logs/event*_*.log
```

**Expected summary:**
```
════════════════════════════════════════════════════════════
    Multi-Event Logger Test Results
    Timestamp: Mon Jan  6 12:10:00 UTC 2025
════════════════════════════════════════════════════════════

Configuration:
  Duration: 10m
  Total Threads: 100
  Total RPS: 1000
  Buffer: 64MB, Shards: 8
  Log Size: 300KB

Event Distribution:
  event1: 350 RPS, 35 threads
  event2: 350 RPS, 35 threads
  event3: 300 RPS, 30 threads

════════════════════════════════════════════════════════════
Results:
════════════════════════════════════════════════════════════

Event: event1
----------------------------------------
METRICS: Logs: 210000 Dropped: 105 (0.0500%) | Bytes: 63000000000 | Flushes: 450 Errors: 0 Swaps: 2105 | AvgFlush: 45.23ms MaxFlush: 78.45ms | AvgWrite: 44.98ms MaxWrite: 78.12ms WritePct: 99.4% | GC: 5 cycles 0.24ms pause | Mem: 72.31MB

Event: event2
----------------------------------------
METRICS: Logs: 210000 Dropped: 98 (0.0467%) | Bytes: 63000000000 | Flushes: 448 Errors: 0 Swaps: 2098 | AvgFlush: 44.87ms MaxFlush: 76.23ms | AvgWrite: 44.65ms MaxWrite: 75.98ms WritePct: 99.5% | GC: 5 cycles 0.22ms pause | Mem: 71.89MB

Event: event3
----------------------------------------
METRICS: Logs: 180000 Dropped: 87 (0.0483%) | Bytes: 54000000000 | Flushes: 385 Errors: 0 Swaps: 1807 | AvgFlush: 43.12ms MaxFlush: 74.56ms | AvgWrite: 42.98ms MaxWrite: 74.34ms WritePct: 99.7% | GC: 4 cycles 0.19ms pause | Mem: 68.45MB

════════════════════════════════════════════════════════════
Resource Usage Summary:
════════════════════════════════════════════════════════════
Average CPU: 12.45%
Average Memory: 18.34%
Average Disk Read: 0.15 MB/s
Average Disk Write: 294.56 MB/s
Average Disk IOPS: 1250.34
```

## Testing File Rotation

### Quick Rotation Test (1-hour intervals)

```bash
# Single logger with 1-hour rotation
./scripts/run_single_logger_test_vm.sh \
    --duration 2h \
    --rotation-interval 1h

# Multi-event with 30-minute rotation
./scripts/run_multi_event_test_vm.sh \
    --duration 2h \
    --rotation-interval 30m
```

### Verify Rotation

```bash
# After test completes, check for rotated files
ls -lh logs/

# Should see files like:
# direct_test.log (current)
# direct_test_2025-01-06_12-00-00.log (rotated)
# direct_test_2025-01-06_13-00-00.log (rotated)

# For multi-event:
# event1.log (current)
# event1_2025-01-06_12-00-00.log (rotated)
# event2.log (current)
# event2_2025-01-06_12-00-00.log (rotated)
# etc.
```

## Verification Checklist

After each test, verify:

- [ ] Test completed without errors
- [ ] Summary file generated
- [ ] Log files created and have content
- [ ] Drop rate < 1%
- [ ] No flush errors (or minimal)
- [ ] Resource usage is reasonable
- [ ] File rotation works (if enabled and interval elapsed)
- [ ] Rotated files have correct timestamps
- [ ] Data integrity: log files are readable

## Common Issues and Solutions

### Issue: Binary not found
```bash
# Solution: Build binary
go build -o bin/direct_logger_test ./cmd/direct_logger_test
```

### Issue: Permission denied
```bash
# Solution: Make scripts executable
chmod +x scripts/*.sh
```

### Issue: High drop rate
```bash
# Solution: Increase buffer or reduce RPS
./scripts/run_single_logger_test_vm.sh --buffer-mb 128 --rps 500
```

### Issue: Test hangs
```bash
# Solution: Check for disk space
df -h .

# Kill test if needed
pkill -f direct_logger_test
```

### Issue: Rotation not working
```bash
# Solution: Check rotation interval is set
grep RotationInterval results/*/summary.txt

# Use shorter interval for testing
--rotation-interval 1h
```

## Next Steps After Testing

1. **Compare Results:**
   - Single logger vs multi-event performance
   - Different buffer sizes
   - Different shard counts

2. **Analyze Metrics:**
   - Drop rates
   - Flush durations
   - Resource usage
   - File rotation behavior

3. **Verify Data:**
   - Check log files are readable
   - Verify rotated files contain correct data
   - Confirm no data corruption

4. **Document Findings:**
   - Save summary files
   - Note any issues or anomalies
   - Compare with previous test runs







