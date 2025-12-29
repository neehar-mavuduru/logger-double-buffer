# VM Test Commands - Direct Logger Performance Testing

This document provides commands to build and test the direct logger on a VM with different scenarios.

## Prerequisites

1. **VM Setup**: Ensure you have a VM with:
   - Local disk (preferred) or network disk
   - Go 1.21+ installed
   - Root access (for Direct I/O)

2. **Repository**: Clone the repository on the VM:
   ```bash
   cd /mnt/localdisk  # or your preferred location
   git clone <repo-url>
   cd logger-double-buffer
   ```

## Build Commands

### Build Single Logger Test
```bash
go build -o bin/direct_logger_test ./cmd/direct_logger_test
```

### Build Multi-Event Logger Test
```bash
go build -o bin/multi_event_test ./cmd/multi_event_test
```

### Build Disk Benchmark Tool
```bash
go build -o bin/disk_benchmark ./cmd/disk_benchmark
```

### Build All Binaries
```bash
mkdir -p bin
go build -o bin/direct_logger_test ./cmd/direct_logger_test
go build -o bin/multi_event_test ./cmd/multi_event_test
go build -o bin/disk_benchmark ./cmd/disk_benchmark
```

## Test Scenarios

### Scenario 1: Single Logger - Baseline (32MB buffer, 8 shards)

**Configuration:**
- Buffer: 32MB
- Shards: 8
- Threads: 100
- RPS: 1000
- Log size: 300KB
- Duration: 10 minutes
- Rotation: Disabled (24h default)

**Command:**
```bash
mkdir -p logs
./bin/direct_logger_test \
  --threads=100 \
  --log-size-kb=300 \
  --rps=1000 \
  --duration=10m \
  --buffer-mb=32 \
  --shards=8 \
  --flush-interval=10s \
  --rotation-interval=0 \
  --log-dir=logs
```

**Expected Output:**
- Look for `AvgPwritev` metric (pure disk I/O time)
- Compare `AvgFlush` vs `AvgPwritev` to see overhead
- Check `PwritevPct` to see % of flush time in syscall

---

### Scenario 2: Single Logger - High Load (64MB buffer, 8 shards)

**Configuration:**
- Buffer: 64MB
- Shards: 8
- Threads: 200
- RPS: 2000
- Log size: 300KB
- Duration: 10 minutes

**Command:**
```bash
mkdir -p logs
./bin/direct_logger_test \
  --threads=200 \
  --log-size-kb=300 \
  --rps=2000 \
  --duration=10m \
  --buffer-mb=64 \
  --shards=8 \
  --flush-interval=10s \
  --rotation-interval=0 \
  --log-dir=logs
```

---

### Scenario 3: Single Logger - More Shards (32MB buffer, 16 shards)

**Configuration:**
- Buffer: 32MB
- Shards: 16
- Threads: 100
- RPS: 1000
- Log size: 300KB
- Duration: 10 minutes

**Command:**
```bash
mkdir -p logs
./bin/direct_logger_test \
  --threads=100 \
  --log-size-kb=300 \
  --rps=1000 \
  --duration=10m \
  --buffer-mb=32 \
  --shards=16 \
  --flush-interval=10s \
  --rotation-interval=0 \
  --log-dir=logs
```

**Purpose:** Test if more shards reduce contention and improve Pwritev time

---

### Scenario 4: Single Logger - Fewer Shards (32MB buffer, 4 shards)

**Configuration:**
- Buffer: 32MB
- Shards: 4
- Threads: 100
- RPS: 1000
- Log size: 300KB
- Duration: 10 minutes

**Command:**
```bash
mkdir -p logs
./bin/direct_logger_test \
  --threads=100 \
  --log-size-kb=300 \
  --rps=1000 \
  --duration=10m \
  --buffer-mb=32 \
  --shards=4 \
  --flush-interval=10s \
  --rotation-interval=0 \
  --log-dir=logs
```

**Purpose:** Test if fewer shards increase contention and affect Pwritev time

---

### Scenario 5: Multi-Event Logger - 3 Events (32MB buffer, 8 shards)

**Configuration:**
- Buffer: 32MB per event
- Shards: 8 per event
- Events: 3 (event1: 500 RPS, event2: 300 RPS, event3: 200 RPS)
- Threads: 50 per event
- Log size: 300KB
- Duration: 10 minutes

**Command:**
```bash
mkdir -p logs
./bin/multi_event_test \
  --buffer-mb=32 \
  --shards=8 \
  --flush-interval=10s \
  --rotation-interval=0 \
  --log-dir=logs \
  --duration=10m \
  --events=event1:500:50:300,event2:300:50:300,event3:200:50:300
```

**Expected Output:**
- Compare `AvgPwritev` with single logger scenario
- Check if multi-event increases Pwritev time due to disk contention

---

### Scenario 6: Multi-Event Logger - High Load (64MB buffer, 8 shards)

**Configuration:**
- Buffer: 64MB per event
- Shards: 8 per event
- Events: 3 (event1: 1000 RPS, event2: 800 RPS, event3: 600 RPS)
- Threads: 100 per event
- Log size: 300KB
- Duration: 10 minutes

**Command:**
```bash
mkdir -p logs
./bin/multi_event_test \
  --buffer-mb=64 \
  --shards=8 \
  --flush-interval=10s \
  --rotation-interval=0 \
  --log-dir=logs \
  --duration=10m \
  --events=event1:1000:100:300,event2:800:100:300,event3:600:100:300
```

---

### Scenario 7: Disk Benchmark - Baseline Performance

**Configuration:**
- Buffer size: 32MB
- Duration: 5 minutes

**Command:**
```bash
mkdir -p logs
./bin/disk_benchmark \
  --buffer-mb=32 \
  --duration=5m \
  --output=logs/benchmark.log
```

**Purpose:** Establish baseline disk I/O performance without logger overhead

---

### Scenario 8: Disk Benchmark - Different Buffer Sizes

**Test 1: 16MB buffer**
```bash
./bin/disk_benchmark --buffer-mb=16 --duration=5m --output=logs/benchmark_16mb.log
```

**Test 2: 32MB buffer**
```bash
./bin/disk_benchmark --buffer-mb=32 --duration=5m --output=logs/benchmark_32mb.log
```

**Test 3: 64MB buffer**
```bash
./bin/disk_benchmark --buffer-mb=64 --duration=5m --output=logs/benchmark_64mb.log
```

**Purpose:** Compare Pwritev latency across different buffer sizes

---

## Key Metrics to Monitor

### From Test Output:
1. **AvgPwritev**: Average Pwritev syscall time (pure disk I/O)
2. **MaxPwritev**: Maximum Pwritev syscall time
3. **PwritevPct**: Percentage of flush time spent in Pwritev syscall
4. **AvgFlush**: Total flush time (includes GetData wait, semaphore, etc.)
5. **AvgWrite**: WriteVectored time (includes rotation checks)

### Analysis:
- **PwritevPct**: Should be high (>80%) if overhead is minimal
- **AvgFlush vs AvgPwritev**: Difference shows overhead (GetData wait, semaphore, etc.)
- **MaxPwritev**: Indicates worst-case disk latency

---

## Quick Test Script

Create a file `quick_test.sh`:

```bash
#!/bin/bash
set -e

echo "=== Building binaries ==="
mkdir -p bin
go build -o bin/direct_logger_test ./cmd/direct_logger_test
go build -o bin/multi_event_test ./cmd/multi_event_test
go build -o bin/disk_benchmark ./cmd/disk_benchmark

echo "=== Running Scenario 1: Single Logger Baseline ==="
mkdir -p logs
./bin/direct_logger_test \
  --threads=100 \
  --log-size-kb=300 \
  --rps=1000 \
  --duration=2m \
  --buffer-mb=32 \
  --shards=8 \
  --flush-interval=10s \
  --rotation-interval=0 \
  --log-dir=logs

echo "=== Running Scenario 5: Multi-Event Logger ==="
./bin/multi_event_test \
  --buffer-mb=32 \
  --shards=8 \
  --flush-interval=10s \
  --rotation-interval=0 \
  --log-dir=logs \
  --duration=2m \
  --events=event1:500:50:300,event2:300:50:300,event3:200:50:300

echo "=== Running Disk Benchmark ==="
./bin/disk_benchmark \
  --buffer-mb=32 \
  --duration=2m \
  --output=logs/benchmark.log

echo "=== Tests Complete ==="
```

Make it executable and run:
```bash
chmod +x quick_test.sh
./quick_test.sh
```

---

## Troubleshooting

### "Invalid argument" errors:
- Ensure buffer size is aligned (multiples of 4096 bytes)
- Check filesystem block size: `stat -f . | grep "Block size"`
- Verify O_DIRECT is supported on your filesystem

### High drop rates:
- Increase buffer size (`--buffer-mb`)
- Increase number of shards (`--shards`)
- Reduce RPS (`--rps`)
- Reduce log size (`--log-size-kb`)

### High flush times:
- Check `AvgPwritev` vs `AvgFlush` to identify overhead
- If `PwritevPct` is low, investigate GetData() wait times
- Consider disk I/O limits (network disk vs local disk)

---

## Notes

- **Rotation**: Set `--rotation-interval=0` to disable rotation for performance testing
- **Log Directory**: Use a fast disk (local SSD preferred)
- **Monitoring**: Use `iostat -x 1` in another terminal to monitor disk I/O
- **CPU**: Monitor CPU usage with `top` or `htop`
- **Memory**: Check memory usage with `free -h`







