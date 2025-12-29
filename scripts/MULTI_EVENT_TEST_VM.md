# Multi-Event Logger Test for VM

This script tests the async logger with multiple events running simultaneously on a VM.

## Overview

The script runs 3 concurrent event loggers, each writing to separate log files:
- `event1.log` - 350 RPS
- `event2.log` - 350 RPS  
- `event3.log` - 300 RPS
- **Total: 1000 RPS**

Each event runs in a separate process using `LoggerManager` for event-based logging.

## Prerequisites

1. **Build the binary:**
   ```bash
   go build -o bin/direct_logger_test ./cmd/direct_logger_test
   ```

2. **Ensure you're on a VM with:**
   - Linux OS (for O_DIRECT support)
   - ext4 filesystem (for 4096-byte alignment)
   - Sufficient disk space (~50GB for 10-minute test)
   - Root access (for resource monitoring)

## Usage

### Basic Usage (Default Configuration)

```bash
./scripts/run_multi_event_test_vm.sh
```

**Default Configuration:**
- Duration: 10 minutes
- Total Threads: 100 (distributed: 35/35/30)
- Total RPS: 1000 (350/350/300)
- Buffer: 64MB, 8 shards
- Log Size: 300KB per log entry

### Custom Configuration

```bash
./scripts/run_multi_event_test_vm.sh \
    --duration 5m \
    --threads 200 \
    --buffer-mb 32 \
    --shards 8 \
    --log-size-kb 300 \
    --event1-rps 400 \
    --event2-rps 400 \
    --event3-rps 200
```

### Options

| Option | Default | Description |
|--------|---------|-------------|
| `--duration` | `10m` | Test duration (e.g., `5m`, `30m`, `1h`) |
| `--threads` | `100` | Total threads (distributed across events) |
| `--buffer-mb` | `64` | Buffer size in MB per event |
| `--shards` | `8` | Number of shards per event |
| `--log-size-kb` | `300` | Log entry size in KB |
| `--event1-rps` | `350` | Event1 target RPS |
| `--event2-rps` | `350` | Event2 target RPS |
| `--event3-rps` | `300` | Event3 target RPS |

## Output

### Results Directory

Results are saved to: `results/multi_event_test_<TIMESTAMP>/`

```
results/multi_event_test_20250106_120000/
├── summary.txt              # Summary report
├── event1_test.log          # Event1 test output
├── event2_test.log          # Event2 test output
├── event3_test.log          # Event3 test output
└── resource_monitor.log     # Resource usage metrics
```

### Summary Report

The `summary.txt` file contains:
- Test configuration
- Per-event metrics (logs, drops, flushes, errors)
- Flush performance (avg/max flush time, write time)
- Resource usage (CPU, memory, disk I/O)

### Log Files

Event log files are written to: `logs/`
- `logs/event1.log`
- `logs/event2.log`
- `logs/event3.log`

## Monitoring During Test

### View Live Logs

```bash
# Watch all event logs
tail -f results/multi_event_test_*/event*_test.log

# Watch specific event
tail -f results/multi_event_test_*/event1_test.log
```

### Monitor Resources

```bash
# Watch resource monitor
tail -f results/multi_event_test_*/resource_monitor.log

# Or use system tools
watch -n 1 'iostat -x 1 1'
htop
```

## Expected Metrics

For a 10-minute test with default configuration:

### Per Event (350 RPS)
- **Total Logs:** ~210,000 logs
- **Total Bytes:** ~60GB per event
- **Flush Duration:** ~20-70ms (depending on disk)
- **Drop Rate:** < 1% (target)

### Aggregated (1000 RPS)
- **Total Logs:** ~600,000 logs
- **Total Bytes:** ~180GB
- **CPU Usage:** < 10% (on 8-core VM)
- **Memory:** ~200-400MB
- **Disk Throughput:** ~300MB/s (SSD)

## Troubleshooting

### High Drop Rate

If drop rate > 1%:
1. **Increase buffer size:**
   ```bash
   --buffer-mb 128
   ```

2. **Increase shards:**
   ```bash
   --shards 16
   ```

3. **Reduce RPS:**
   ```bash
   --event1-rps 300 --event2-rps 300 --event3-rps 200
   ```

### Slow Flush Times

If flush times > 100ms:
1. **Check disk I/O:**
   ```bash
   iostat -x 1
   ```

2. **Verify O_DIRECT alignment:**
   - Ensure ext4 filesystem
   - Check filesystem block size: `tune2fs -l /dev/sdX1 | grep "Block size"`

3. **Check for disk contention:**
   - Ensure no other processes writing to disk
   - Use dedicated disk/partition

### Process Failures

If one or more events fail:
1. Check exit codes in summary
2. Review individual event logs for errors
3. Check system resources (disk space, memory)
4. Verify binary is built correctly

## Example Output

```
════════════════════════════════════════════════════════════
    Multi-Event Logger Test Results
    Timestamp: Mon Jan  6 12:00:00 UTC 2025
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
Average CPU: 8.45%
Average Memory: 12.34%
Average Disk Read: 0.12 MB/s
Average Disk Write: 294.56 MB/s
Average Disk IOPS: 1250.34
```

## Notes

- Each event runs in a separate process for isolation
- All events share the same VM resources (CPU, memory, disk)
- Resource monitoring samples every 5 seconds
- Test automatically cleans up on exit (Ctrl+C)
- Log files persist after test completion








