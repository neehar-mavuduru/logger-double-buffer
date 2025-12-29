# VM Test Quick Start Guide

Quick reference for running tests on VM.

## Prerequisites Check

```bash
# Verify Go is installed
go version

# Verify you're in the repo root
pwd
# Should be: /path/to/logger-double-buffer

# Verify filesystem is ext4 (for O_DIRECT)
df -T | grep $(df . | tail -1 | awk '{print $1}')
# Should show: ext4
```

## 1. Single Logger Test

### Basic Test (10 minutes, default config)

```bash
# Build binary
go build -o bin/direct_logger_test ./cmd/direct_logger_test

# Run test
./scripts/run_single_logger_test_vm.sh
```

### Custom Configuration

```bash
./scripts/run_single_logger_test_vm.sh \
    --duration 5m \
    --threads 200 \
    --rps 2000 \
    --buffer-mb 128 \
    --shards 16 \
    --rotation-interval 1h
```

### Monitor During Test

```bash
# Watch metrics (in another terminal)
tail -f results/single_logger_test_*/test_output.log | grep METRICS

# Watch resources
tail -f results/single_logger_test_*/resource_monitor.log
```

### Check Results

```bash
# View summary
cat results/single_logger_test_*/summary.txt

# Check log file
ls -lh logs/direct_test.log

# Check for rotated files
ls -lh logs/direct_test_*.log
```

## 2. Multi-Event Logger Test

### Basic Test (10 minutes, 3 events)

```bash
# Build binary (if not already built)
go build -o bin/direct_logger_test ./cmd/direct_logger_test

# Run test
./scripts/run_multi_event_test_vm.sh
```

### Custom Configuration

```bash
./scripts/run_multi_event_test_vm.sh \
    --duration 5m \
    --threads 200 \
    --event1-rps 400 \
    --event2-rps 400 \
    --event3-rps 200 \
    --rotation-interval 1h
```

### Monitor During Test

```bash
# Watch all events
tail -f results/multi_event_test_*/event*_test.log | grep METRICS

# Watch specific event
tail -f results/multi_event_test_*/event1_test.log

# Watch resources
tail -f results/multi_event_test_*/resource_monitor.log
```

### Check Results

```bash
# View summary
cat results/multi_event_test_*/summary.txt

# Check log files
ls -lh logs/event*.log

# Check for rotated files
ls -lh logs/event*_*.log
```

## Test File Rotation

To test rotation with shorter intervals:

```bash
# Single logger - rotate every hour
./scripts/run_single_logger_test_vm.sh \
    --duration 2h \
    --rotation-interval 1h

# Multi-event - rotate every 30 minutes
./scripts/run_multi_event_test_vm.sh \
    --duration 2h \
    --rotation-interval 30m
```

After test, verify rotation:

```bash
# List all log files
ls -lh logs/

# Should see timestamped files:
# direct_test.log
# direct_test_2025-01-06_12-00-00.log
# direct_test_2025-01-06_13-00-00.log
```

## Expected Output Locations

### Single Logger
- **Results:** `results/single_logger_test_<TIMESTAMP>/`
- **Log File:** `logs/direct_test.log`
- **Rotated Files:** `logs/direct_test_<TIMESTAMP>.log`

### Multi-Event Logger
- **Results:** `results/multi_event_test_<TIMESTAMP>/`
- **Log Files:** `logs/event1.log`, `logs/event2.log`, `logs/event3.log`
- **Rotated Files:** `logs/event1_<TIMESTAMP>.log`, etc.

## Key Metrics to Monitor

- **Drop Rate:** Should be < 1%
- **Flush Duration:** Should be < 100ms (depends on disk)
- **Disk Throughput:** Should match disk capabilities (~300MB/s for SSD)
- **CPU Usage:** Should be < 15%
- **Memory:** Should be stable (200-1200MB depending on config)

## Troubleshooting

### Test fails to start
```bash
# Check binary exists
ls -lh bin/direct_logger_test

# Rebuild if needed
go build -o bin/direct_logger_test ./cmd/direct_logger_test
```

### High drop rate
```bash
# Increase buffer size
--buffer-mb 128

# Increase shards
--shards 16

# Reduce RPS
--rps 500
```

### Check for errors
```bash
# Check test output for errors
grep ERROR results/*/test_output.log

# Check flush errors
grep FLUSH_ERROR results/*/test_output.log
```

## Quick Commands Reference

```bash
# Build
go build -o bin/direct_logger_test ./cmd/direct_logger_test

# Single logger test
./scripts/run_single_logger_test_vm.sh

# Multi-event test
./scripts/run_multi_event_test_vm.sh

# View latest results
cat results/*/summary.txt | tail -50

# Check log files
ls -lh logs/

# Monitor live
tail -f results/*/test_output.log | grep METRICS
```







