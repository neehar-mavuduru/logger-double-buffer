# Multi-Event Logger Fix: Single Process Solution

## Problem Identified

### Issue 1: High Flush Latency (60-70ms vs 12ms)

**Root Cause:** The original test script (`run_multi_event_test_vm.sh`) runs **3 separate processes**, each with its own LoggerManager:

```bash
# Original approach - 3 SEPARATE PROCESSES
./bin/direct_logger_test --use-events --event event1 &  # Process 1
./bin/direct_logger_test --use-events --event event2 &  # Process 2
./bin/direct_logger_test --use-events --event event3 &  # Process 3
```

**Problems:**
1. **Disk Contention:** 3 processes writing to 3 files simultaneously causes severe disk contention
2. **Process Overhead:** 3x context switching, syscalls, GC heaps
3. **File System Overhead:** 3x metadata updates competing with data writes
4. **No Coordination:** Processes can't coordinate flushes to reduce contention

**Result:** Even with lower total RPS (400 vs 1000), flush latency increases from 12ms to 60-70ms.

### Issue 2: Panics with 0 RPS Events

**Root Cause:** When an event has 0 RPS:
- Process still starts and creates LoggerManager
- Thread calculation may result in 0 threads
- Process runs but never logs anything
- LoggerManager may have issues with empty/inactive loggers

**Symptoms:**
- Panics in event2 and event3 when they have 0 RPS
- Process crashes or hangs

## Solution: Single Process with LoggerManager

### New Approach

Created `cmd/multi_event_test/main.go` - a single process that:
1. Creates **one LoggerManager** for all events
2. Pre-initializes loggers for all events (prevents lazy init overhead)
3. Runs workers for each event in the same process
4. Coordinates all writes through single LoggerManager

**Benefits:**
- ✅ **No Process Overhead:** Single process = no context switching between processes
- ✅ **Better Coordination:** LoggerManager can coordinate flushes
- ✅ **Shared Resources:** Single GC heap, shared memory
- ✅ **No Panics:** Proper handling of 0 RPS events (simply don't start workers)

### Expected Performance Improvement

- **Before (3 processes):** 60-70ms flush latency
- **After (1 process):** Expected 15-20ms flush latency (~70% improvement)
- **Target:** Match single logger performance (12ms)

## Usage

### Build the New Binary

```bash
go build -o bin/multi_event_test ./cmd/multi_event_test
```

### Run Single Process Test

```bash
# Default: 350+350+300 RPS, 100 threads total
./scripts/run_multi_event_single_process_vm.sh

# Custom configuration
./scripts/run_multi_event_single_process_vm.sh \
    --duration 5m \
    --threads 200 \
    --event1-rps 500 \
    --event2-rps 0 \
    --event3-rps 0 \
    --rotation-interval 1h
```

### Key Differences from Original Script

| Aspect | Original (3 processes) | New (1 process) |
|--------|------------------------|-----------------|
| **Processes** | 3 separate | 1 single |
| **LoggerManager** | 3 instances | 1 instance |
| **Coordination** | None | Full coordination |
| **Disk Contention** | High (3 concurrent streams) | Low (coordinated writes) |
| **Expected Latency** | 60-70ms | 15-20ms |
| **0 RPS Handling** | Panics | Gracefully skipped |

## Testing Scenarios

### Scenario 1: All Events Active
```bash
./scripts/run_multi_event_single_process_vm.sh \
    --event1-rps 350 \
    --event2-rps 350 \
    --event3-rps 300
```

### Scenario 2: Single Event Active (Your Test Case)
```bash
./scripts/run_multi_event_single_process_vm.sh \
    --event1-rps 500 \
    --event2-rps 0 \
    --event3-rps 0
```

**Expected:** Should perform similar to single logger (12-15ms) since only one event is active.

### Scenario 3: Two Events Active
```bash
./scripts/run_multi_event_single_process_vm.sh \
    --event1-rps 500 \
    --event2-rps 500 \
    --event3-rps 0
```

## Verification

After running the test, compare:

1. **Flush Latency:**
   ```bash
   grep "AvgFlush" results/multi_event_single_process_*/summary.txt
   ```

2. **Disk I/O:**
   ```bash
   grep "Disk Write" results/multi_event_single_process_*/summary.txt
   ```

3. **No Panics:**
   ```bash
   grep -i panic results/multi_event_single_process_*/*.log
   # Should return nothing
   ```

## Migration Path

1. **Test new single-process approach** with your scenarios
2. **Compare performance** with original 3-process approach
3. **Verify no panics** with 0 RPS events
4. **Update documentation** if single-process approach performs better

## Files Created

1. **`cmd/multi_event_test/main.go`** - Single process multi-event test binary
2. **`scripts/run_multi_event_single_process_vm.sh`** - Test script for single process approach
3. **`docs/MULTI_EVENT_FIX.md`** - This document

## Next Steps

1. Run the new single-process test on VM
2. Compare flush latency with original 3-process test
3. Verify no panics with 0 RPS events
4. If performance is better, consider replacing original script

