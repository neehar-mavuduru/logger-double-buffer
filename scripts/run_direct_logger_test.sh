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
echo -e "${CYAN}    Direct Logger Test - No ghz, Pure Logger Performance    ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/direct_logger_test"
LOG_DIR="logs"
DURATION="10m"
THREADS=100
LOG_SIZE_KB=300
TARGET_RPS=1000
BUFFER_MB=64
SHARDS=8
FLUSH_INTERVAL="10s"
USE_EVENTS=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --duration)
            DURATION="$2"
            shift 2
            ;;
        --threads)
            THREADS="$2"
            shift 2
            ;;
        --rps)
            TARGET_RPS="$2"
            shift 2
            ;;
        --buffer-mb)
            BUFFER_MB="$2"
            shift 2
            ;;
        --shards)
            SHARDS="$2"
            shift 2
            ;;
        --use-events)
            USE_EVENTS=true
            shift
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --duration DURATION     Test duration (default: 10m)"
            echo "  --threads THREADS       Number of threads (default: 100)"
            echo "  --rps RPS               Target RPS (default: 1000)"
            echo "  --buffer-mb MB          Buffer size in MB (default: 64)"
            echo "  --shards SHARDS         Number of shards (default: 8)"
            echo "  --use-events            Use event-based logging (LoggerManager)"
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

echo "Run started at: $(date)"
echo "Results directory: $RESULTS_DIR"
echo "Test configuration:"
echo "  - Threads: $THREADS"
echo "  - Log size: ${LOG_SIZE_KB}KB"
echo "  - Target RPS: $TARGET_RPS"
echo "  - Duration: $DURATION"
echo "  - Buffer: ${BUFFER_MB}MB"
echo "  - Shards: $SHARDS"
echo "  - Flush Interval: $FLUSH_INTERVAL"
echo "  - Event-based: $USE_EVENTS"
echo ""

# Build test program
echo -e "${BLUE}Building test program...${NC}"
if ! go build -o "$RESULTS_DIR/direct_logger_test" ./cmd/direct_logger_test; then
    echo -e "${RED}✗ Failed to build test program${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Test program built${NC}"
echo ""

# Run test
echo -e "${BLUE}Running direct logger test...${NC}"
EVENT_FLAG=""
if [ "$USE_EVENTS" = true ]; then
    EVENT_FLAG="--use-events"
fi

"$RESULTS_DIR/direct_logger_test" \
    --threads="$THREADS" \
    --log-size-kb="$LOG_SIZE_KB" \
    --rps="$TARGET_RPS" \
    --duration="$DURATION" \
    --buffer-mb="$BUFFER_MB" \
    --shards="$SHARDS" \
    --flush-interval="$FLUSH_INTERVAL" \
    --log-dir="$LOG_DIR" \
    $EVENT_FLAG \
    2>&1 | tee "$RESULTS_DIR/test_${TIMESTAMP}.log"

TEST_EXIT=$?

if [ $TEST_EXIT -eq 0 ]; then
    echo -e "${GREEN}✓ Test completed successfully!${NC}"
else
    echo -e "${RED}✗ Test failed with exit code: $TEST_EXIT${NC}"
fi

echo ""
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}                      Test Results                          ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${BLUE}Results saved to: $RESULTS_DIR/${NC}"
echo -e "${BLUE}  - Test log: test_${TIMESTAMP}.log${NC}"
echo ""

# Extract final metrics
if [ -f "$RESULTS_DIR/test_${TIMESTAMP}.log" ]; then
    echo -e "${CYAN}Final Metrics:${NC}"
    grep "METRICS:" "$RESULTS_DIR/test_${TIMESTAMP}.log" | tail -1 | sed 's/.*METRICS: //'
fi

echo ""
echo -e "${GREEN}Test completed at: $(date)${NC}"

