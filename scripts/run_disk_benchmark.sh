#!/bin/bash

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}    Disk I/O Benchmark Tool                                  ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/disk_benchmark_${TIMESTAMP}"
LOG_DIR="logs"
DURATION="5m"
BUFFER_MB=32
NUM_BUFFERS=10

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --duration)
            DURATION="$2"
            shift 2
            ;;
        --buffer-mb)
            BUFFER_MB="$2"
            shift 2
            ;;
        --num-buffers)
            NUM_BUFFERS="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --duration DURATION     Test duration (default: 5m)"
            echo "  --buffer-mb MB          Buffer size in MB (default: 32)"
            echo "  --num-buffers N         Number of pre-generated buffers (default: 10)"
            echo "  --help                  Show this help"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Create directories
mkdir -p "$RESULTS_DIR"
mkdir -p "$LOG_DIR"

# Cleanup function
cleanup() {
    echo ""
    echo -e "${YELLOW}Cleaning up...${NC}"
    
    # Kill benchmark if running
    if [ -n "${BENCHMARK_PID:-}" ]; then
        kill "$BENCHMARK_PID" 2>/dev/null || true
        wait "$BENCHMARK_PID" 2>/dev/null || true
    fi
    
    echo -e "${GREEN}✓ Cleanup complete${NC}"
}

trap cleanup EXIT INT TERM

# Check if binary exists
if [ ! -f "bin/disk_benchmark" ]; then
    echo -e "${YELLOW}Binary not found. Building...${NC}"
    # Build for Linux (required for O_DIRECT support)
    # On macOS, this will build a Linux binary (use on VM)
    # On Linux VM, regular go build works
    if command -v go >/dev/null 2>&1; then
        GOOS=linux go build -o bin/disk_benchmark ./cmd/disk_benchmark 2>&1 || \
        go build -o bin/disk_benchmark ./cmd/disk_benchmark 2>&1
    fi
    if [ ! -f "bin/disk_benchmark" ]; then
        echo -e "${RED}Failed to build binary${NC}"
        echo -e "${YELLOW}Note: This benchmark requires Linux (O_DIRECT support)${NC}"
        echo -e "${YELLOW}Please build on Linux VM: go build -o bin/disk_benchmark ./cmd/disk_benchmark${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Binary built successfully${NC}"
fi

# Print configuration
echo -e "${CYAN}Test Configuration:${NC}"
echo "  Duration: $DURATION"
echo "  Buffer Size: ${BUFFER_MB}MB"
echo "  Pre-generated Buffers: $NUM_BUFFERS"
echo ""

# Clean up old log file
LOG_FILE="$LOG_DIR/disk_benchmark.log"
rm -f "$LOG_FILE" 2>/dev/null || true

# Run benchmark
echo -e "${CYAN}Starting disk benchmark...${NC}"
OUTPUT_FILE="$RESULTS_DIR/benchmark_output.log"

./bin/disk_benchmark \
    --duration "$DURATION" \
    --buffer-mb "$BUFFER_MB" \
    --num-buffers "$NUM_BUFFERS" \
    --log-path "$LOG_FILE" \
    > "$OUTPUT_FILE" 2>&1 &
BENCHMARK_PID=$!

echo -e "${GREEN}✓ Benchmark started (PID: $BENCHMARK_PID)${NC}"
echo "  Monitor output: tail -f $OUTPUT_FILE"
echo ""

# Wait for benchmark to complete
wait $BENCHMARK_PID
EXIT_CODE=$?

if [ $EXIT_CODE -ne 0 ]; then
    echo -e "${RED}Benchmark failed with exit code $EXIT_CODE${NC}"
    cat "$OUTPUT_FILE"
    exit 1
fi

# Extract results
echo ""
echo -e "${GREEN}✓ Benchmark completed${NC}"
echo ""
cat "$OUTPUT_FILE"

# Save summary
SUMMARY_FILE="$RESULTS_DIR/summary.txt"
cat > "$SUMMARY_FILE" << EOF
════════════════════════════════════════════════════════════
    Disk I/O Benchmark Results
    Timestamp: $(date)
════════════════════════════════════════════════════════════

Configuration:
  Duration: $DURATION
  Buffer Size: ${BUFFER_MB}MB
  Pre-generated Buffers: $NUM_BUFFERS

EOF

# Extract key metrics from output
grep -E "(Min Duration|Avg Duration|Max Duration|P50|P95|P99|Throughput)" "$OUTPUT_FILE" >> "$SUMMARY_FILE" 2>/dev/null || true

echo ""
echo -e "${CYAN}Results saved to: $RESULTS_DIR${NC}"
echo "  Summary: $SUMMARY_FILE"
echo "  Full output: $OUTPUT_FILE"
echo "  Log file: $LOG_FILE"

# Check log file size
if [ -f "$LOG_FILE" ]; then
    LOG_SIZE=$(du -h "$LOG_FILE" | cut -f1)
    echo "  Log file size: $LOG_SIZE"
fi

