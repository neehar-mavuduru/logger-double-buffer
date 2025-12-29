# VM Testing - Complete Guide

This directory contains all scripts and documentation for running logger tests on VM.

## Quick Start

### Single Logger Test
```bash
go build -o bin/direct_logger_test ./cmd/direct_logger_test
./scripts/run_single_logger_test_vm.sh
```

### Multi-Event Logger Test
```bash
go build -o bin/direct_logger_test ./cmd/direct_logger_test
./scripts/run_multi_event_test_vm.sh
```

## Documentation Files

1. **VM_TESTING_GUIDE.md** - Comprehensive guide with all details
2. **VM_TEST_QUICK_START.md** - Quick reference for common commands
3. **VM_TEST_STEPS.md** - Step-by-step execution instructions

## Test Scripts

1. **run_single_logger_test_vm.sh** - Single logger test script
2. **run_multi_event_test_vm.sh** - Multi-event logger test script

## Test Scenarios

### Scenario 1: Single Logger
- **Purpose:** Test basic logger performance
- **Output:** Single log file (`logs/direct_test.log`)
- **Use Case:** Baseline performance testing

### Scenario 2: Multi-Event Logger
- **Purpose:** Test LoggerManager with multiple events
- **Output:** Multiple log files (`logs/event1.log`, `logs/event2.log`, `logs/event3.log`)
- **Use Case:** Event-based logging, file rotation per event

## File Structure

```
logger-double-buffer/
├── bin/
│   └── direct_logger_test          # Test binary (build this first)
├── scripts/
│   ├── run_single_logger_test_vm.sh    # Single logger test script
│   ├── run_multi_event_test_vm.sh     # Multi-event test script
│   ├── VM_TESTING_GUIDE.md             # Full guide
│   ├── VM_TEST_QUICK_START.md          # Quick reference
│   └── VM_TEST_STEPS.md                # Step-by-step guide
├── logs/
│   ├── direct_test.log                 # Single logger output
│   ├── event1.log                      # Multi-event outputs
│   ├── event2.log
│   └── event3.log
└── results/
    ├── single_logger_test_<TIMESTAMP>/  # Single logger results
    └── multi_event_test_<TIMESTAMP>/   # Multi-event results
```

## Key Features Tested

✅ **Basic Logging:** Single and multi-event scenarios
✅ **File Rotation:** Time-based rotation with configurable intervals
✅ **Performance:** Throughput, latency, drop rates
✅ **Resource Usage:** CPU, memory, disk I/O monitoring
✅ **Concurrency:** Multiple threads and events
✅ **Data Integrity:** Verified through comprehensive tests

## Getting Help

- **Full Guide:** See `VM_TESTING_GUIDE.md`
- **Quick Reference:** See `VM_TEST_QUICK_START.md`
- **Step-by-Step:** See `VM_TEST_STEPS.md`
- **Script Help:** Run `./scripts/run_*_test_vm.sh --help`







