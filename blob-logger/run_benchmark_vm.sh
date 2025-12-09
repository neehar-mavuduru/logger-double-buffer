#!/bin/bash

# Script to run blob-logger shard_handler benchmark test on VM
# Usage: ./run_benchmark_vm.sh [options]

set -e

# Default values
TEST_DIR="${BLOB_LOGGER_TEST_DIR:-/tmp/blob-logger-test}"
BENCH_TIME="${BENCH_TIME:-1000x}"
BENCH_ARGS=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --test-dir)
            TEST_DIR="$2"
            shift 2
            ;;
        --bench-time)
            BENCH_TIME="$2"
            shift 2
            ;;
        --cpu-profile)
            BENCH_ARGS="$BENCH_ARGS -cpuprofile=cpu.prof"
            shift
            ;;
        --mem-profile)
            BENCH_ARGS="$BENCH_ARGS -memprofile=mem.prof"
            shift
            ;;
        --cleanup)
            CLEANUP=true
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  --test-dir DIR      Test directory (default: /tmp/blob-logger-test)"
            echo "  --bench-time TIME   Benchmark time (default: 1000x)"
            echo "                     Examples: 1000x, 30s, 5m"
            echo "  --cpu-profile       Enable CPU profiling"
            echo "  --mem-profile       Enable memory profiling"
            echo "  --cleanup           Clean up test files after benchmark"
            echo "  -h, --help          Show this help message"
            echo ""
            echo "Environment Variables:"
            echo "  BLOB_LOGGER_TEST_DIR  Override test directory"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}    Blob-Logger ShardHandler Benchmark Test                 ${NC}"
echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Check prerequisites
echo "Checking prerequisites..."

# Check Go
if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed"
    exit 1
fi
echo "✓ Go version: $(go version)"

# Check Linux
if [[ "$(uname)" != "Linux" ]]; then
    echo -e "${YELLOW}Warning: Not running on Linux. O_DIRECT may not work.${NC}"
fi

# Check filesystem
FS_TYPE=$(df -T . | tail -1 | awk '{print $2}')
if [[ "$FS_TYPE" != "ext4" ]]; then
    echo -e "${YELLOW}Warning: Filesystem is $FS_TYPE, not ext4. O_DIRECT alignment may differ.${NC}"
else
    echo "✓ Filesystem: ext4"
fi

# Check disk space
AVAILABLE=$(df . | tail -1 | awk '{print $4}')
AVAILABLE_GB=$((AVAILABLE / 1024 / 1024))
if [[ $AVAILABLE_GB -lt 10 ]]; then
    echo -e "${YELLOW}Warning: Less than 10GB disk space available (${AVAILABLE_GB}GB)${NC}"
else
    echo "✓ Disk space: ${AVAILABLE_GB}GB available"
fi

echo ""

# Create test directory
echo "Setting up test directory: $TEST_DIR"
mkdir -p "$TEST_DIR"
export BLOB_LOGGER_TEST_DIR="$TEST_DIR"
echo "✓ Test directory ready"

# Navigate to shard directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SHARD_DIR="$SCRIPT_DIR/shard"

if [[ ! -d "$SHARD_DIR" ]]; then
    echo "Error: shard directory not found: $SHARD_DIR"
    exit 1
fi

cd "$SHARD_DIR"

echo ""
echo "Test Configuration:"
echo "  Test Directory: $TEST_DIR"
echo "  Benchmark Time: $BENCH_TIME"
echo "  Benchmark Args: $BENCH_ARGS"
echo ""

# Clean up old test files if requested
if [[ "$CLEANUP" == "true" ]]; then
    echo "Cleaning up old test files..."
    rm -rf "$TEST_DIR"/* 2>/dev/null || true
    echo "✓ Cleanup complete"
    echo ""
fi

# Run benchmark
echo -e "${GREEN}Running benchmark...${NC}"
echo ""

BENCH_CMD="go test -bench=BenchmarkShardHandler_Write_concurrent -benchtime=$BENCH_TIME -benchmem -v $BENCH_ARGS"

echo "Command: $BENCH_CMD"
echo ""

eval "$BENCH_CMD"

BENCH_EXIT_CODE=$?

echo ""
if [[ $BENCH_EXIT_CODE -eq 0 ]]; then
    echo -e "${GREEN}✓ Benchmark completed successfully${NC}"
    
    # Show test files created
    if [[ -d "$TEST_DIR" ]]; then
        FILE_COUNT=$(find "$TEST_DIR" -name "swap_shard_*.bin" 2>/dev/null | wc -l)
        if [[ $FILE_COUNT -gt 0 ]]; then
            echo ""
            echo "Test files created: $FILE_COUNT"
            echo "Location: $TEST_DIR"
            echo ""
            echo "Sample files:"
            find "$TEST_DIR" -name "swap_shard_*.bin" | head -5 | while read -r file; do
                SIZE=$(du -h "$file" | cut -f1)
                echo "  - $(basename "$file"): $SIZE"
            done
        fi
    fi
    
    # Show profiling files if created
    if [[ -f "cpu.prof" ]] || [[ -f "mem.prof" ]]; then
        echo ""
        echo "Profiling files created:"
        [[ -f "cpu.prof" ]] && echo "  - cpu.prof"
        [[ -f "mem.prof" ]] && echo "  - mem.prof"
        echo ""
        echo "To analyze profiles:"
        echo "  go tool pprof cpu.prof"
        echo "  go tool pprof mem.prof"
    fi
else
    echo -e "${YELLOW}Benchmark exited with code: $BENCH_EXIT_CODE${NC}"
fi

echo ""
echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"

exit $BENCH_EXIT_CODE
