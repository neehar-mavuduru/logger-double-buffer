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
echo -e "${CYAN}    Async Log Uploader Load Test - VM Execution            ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/asyncloguploader_test"
LOG_DIR="logs"
DURATION="10m"
THREADS=100
LOG_SIZE_KB=300
TARGET_RPS=1000
BUFFER_MB=64
SHARDS=8
FLUSH_INTERVAL="10s"
FLUSH_TIMEOUT="10ms"
MAX_FILE_SIZE_GB=0
PREALLOCATE_SIZE_GB=0
USE_EVENTS=false
NUM_EVENTS=3
GCS_BUCKET=""
GCS_PREFIX=""
GCS_CHUNK_MB=32

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
        --flush-interval)
            FLUSH_INTERVAL="$2"
            shift 2
            ;;
        --flush-timeout)
            FLUSH_TIMEOUT="$2"
            shift 2
            ;;
        --max-file-size-gb)
            MAX_FILE_SIZE_GB="$2"
            shift 2
            ;;
        --preallocate-size-gb)
            PREALLOCATE_SIZE_GB="$2"
            shift 2
            ;;
        --use-events)
            USE_EVENTS=true
            shift
            ;;
        --num-events)
            NUM_EVENTS="$2"
            shift 2
            ;;
        --gcs-bucket)
            GCS_BUCKET="$2"
            shift 2
            ;;
        --gcs-prefix)
            GCS_PREFIX="$2"
            shift 2
            ;;
        --gcs-chunk-mb)
            GCS_CHUNK_MB="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --duration DURATION         Test duration (default: 10m)"
            echo "  --threads THREADS            Number of threads (default: 100)"
            echo "  --rps RPS                    Target RPS (default: 1000)"
            echo "  --buffer-mb MB               Buffer size in MB (default: 64)"
            echo "  --shards SHARDS              Number of shards (default: 8)"
            echo "  --flush-interval INTERVAL    Flush interval (default: 10s)"
            echo "  --flush-timeout TIMEOUT      Flush timeout (default: 10ms)"
            echo "  --max-file-size-gb GB        Max file size in GB (default: 0 = disabled)"
            echo "  --preallocate-size-gb GB     Preallocate size in GB (default: 0 = use max-file-size)"
            echo "  --use-events                 Use event-based logging"
            echo "  --num-events N               Number of events (default: 3)"
            echo "  --gcs-bucket BUCKET          GCS bucket name (optional)"
            echo "  --gcs-prefix PREFIX          GCS object prefix (optional)"
            echo "  --gcs-chunk-mb MB            GCS chunk size in MB (default: 32)"
            echo "  --help                       Show this help"
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
    
    # Kill test process if running
    if [ -n "${TEST_PID:-}" ]; then
        kill "$TEST_PID" 2>/dev/null || true
        wait "$TEST_PID" 2>/dev/null || true
    fi
    
    # Kill resource monitor if running
    if [ -n "${MONITOR_PID:-}" ]; then
        kill "$MONITOR_PID" 2>/dev/null || true
        wait "$MONITOR_PID" 2>/dev/null || true
    fi
    
    echo -e "${GREEN}✓ Cleanup complete${NC}"
}

trap cleanup EXIT INT TERM

# Cleanup old logs
cleanup_old_logs() {
    echo -e "${BLUE}Cleaning up old test logs...${NC}"
    rm -rf "$LOG_DIR"/*.log 2>/dev/null || true
    echo -e "${GREEN}✓ Old logs cleaned${NC}"
}

# Build test program
echo -e "${BLUE}Building test program...${NC}"
if ! go build -o "$RESULTS_DIR/asyncloguploader_test" ./cmd/asyncloguploader_test; then
    echo -e "${RED}✗ Failed to build test program${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Test program built${NC}"
echo ""

# Cleanup old logs
cleanup_old_logs

# Start resource monitoring
echo -e "${BLUE}Starting resource monitoring...${NC}"
RESOURCE_FILE="$RESULTS_DIR/resource_timeline_${TIMESTAMP}.csv"
{
    echo "timestamp,cpu_percent,mem_mb,mem_percent,disk_read_mb,disk_write_mb"
    while true; do
        TIMESTAMP_NOW=$(date +%s)
        
        # Get CPU usage (average across all cores)
        CPU=$(top -bn1 | grep "Cpu(s)" | sed "s/.*, *\([0-9.]*\)%* id.*/\1/" | awk '{print 100 - $1}')
        
        # Get memory usage
        MEM_TOTAL=$(free -m | awk '/^Mem:/ {print $2}')
        MEM_USED=$(free -m | awk '/^Mem:/ {print $3}')
        MEM_PCT=$(awk "BEGIN {printf \"%.2f\", ($MEM_USED/$MEM_TOTAL)*100}")
        
        # Get disk I/O (if iostat available, use it; otherwise use /proc/diskstats)
        if command -v iostat >/dev/null 2>&1; then
            DISK_STATS=$(iostat -x 1 1 | awk '/^[a-z]/ {if (NR>1) print $6,$7}' | tail -1)
            DISK_READ=$(echo "$DISK_STATS" | awk '{print $1/1024}')  # Convert to MB
            DISK_WRITE=$(echo "$DISK_STATS" | awk '{print $2/1024}')  # Convert to MB
        else
            # Fallback: Use /proc/diskstats for basic disk I/O metrics
            # Read sectors (sector 5) and write sectors (sector 9) from first disk
            if [ -f /proc/diskstats ]; then
                # Get first non-loopback disk
                DISK_STATS=$(grep -v "loop\|ram" /proc/diskstats | head -1)
                if [ -n "$DISK_STATS" ]; then
                    # Sectors are typically 512 bytes, convert to MB
                    READ_SECTORS=$(echo "$DISK_STATS" | awk '{print $6}')
                    WRITE_SECTORS=$(echo "$DISK_STATS" | awk '{print $10}')
                    DISK_READ=$(awk "BEGIN {printf \"%.2f\", $READ_SECTORS * 512 / 1024 / 1024}")
                    DISK_WRITE=$(awk "BEGIN {printf \"%.2f\", $WRITE_SECTORS * 512 / 1024 / 1024}")
                else
                    DISK_READ=0
                    DISK_WRITE=0
                fi
            else
                DISK_READ=0
                DISK_WRITE=0
            fi
        fi
        
        echo "$TIMESTAMP_NOW,$CPU,$MEM_USED,$MEM_PCT,$DISK_READ,$DISK_WRITE"
        sleep 1
    done
} > "$RESOURCE_FILE" 2>/dev/null &
MONITOR_PID=$!
echo -e "${GREEN}✓ Resource monitoring started (PID: $MONITOR_PID)${NC}"
echo ""

# Run test
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}    Running Async Log Uploader Load Test                   ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}Configuration:${NC}"
echo "  Threads:          $THREADS"
echo "  Log size:         ${LOG_SIZE_KB}KB"
echo "  Target RPS:       $TARGET_RPS"
echo "  Duration:         $DURATION"
echo "  Buffer:           ${BUFFER_MB}MB"
echo "  Shards:           $SHARDS"
echo "  Flush Interval:   $FLUSH_INTERVAL"
echo "  Flush Timeout:    $FLUSH_TIMEOUT"
echo "  Max File Size:    ${MAX_FILE_SIZE_GB}GB"
echo "  Preallocate Size: ${PREALLOCATE_SIZE_GB}GB"
echo "  Event-based:      $USE_EVENTS"
if [ "$USE_EVENTS" = true ]; then
    echo "  Number of events: $NUM_EVENTS"
fi
if [ -n "$GCS_BUCKET" ]; then
    echo "  GCS Bucket:       $GCS_BUCKET"
    echo "  GCS Prefix:      $GCS_PREFIX"
    echo "  GCS Chunk Size:   ${GCS_CHUNK_MB}MB"
fi
echo ""

EVENT_FLAG=""
if [ "$USE_EVENTS" = true ]; then
    EVENT_FLAG="--use-events --num-events=$NUM_EVENTS"
fi

GCS_FLAGS=""
if [ -n "$GCS_BUCKET" ]; then
    GCS_FLAGS="--gcs-bucket=$GCS_BUCKET --gcs-prefix=$GCS_PREFIX --gcs-chunk-mb=$GCS_CHUNK_MB"
fi

TEST_LOG="$RESULTS_DIR/test_${TIMESTAMP}.log"
START_TIME=$(date +%s)

# Run test in background and capture PID
"$RESULTS_DIR/asyncloguploader_test" \
    --threads="$THREADS" \
    --log-size-kb="$LOG_SIZE_KB" \
    --rps="$TARGET_RPS" \
    --duration="$DURATION" \
    --buffer-mb="$BUFFER_MB" \
    --shards="$SHARDS" \
    --flush-interval="$FLUSH_INTERVAL" \
    --flush-timeout="$FLUSH_TIMEOUT" \
    --max-file-size-gb="$MAX_FILE_SIZE_GB" \
    --preallocate-size-gb="$PREALLOCATE_SIZE_GB" \
    --log-dir="$LOG_DIR" \
    $EVENT_FLAG \
    $GCS_FLAGS \
    > "$TEST_LOG" 2>&1 &
TEST_PID=$!

echo -e "${BLUE}Test started (PID: $TEST_PID)${NC}"
echo -e "${BLUE}Logging to: $TEST_LOG${NC}"
echo ""

# Wait for test to complete
echo -e "${BLUE}Waiting for test to complete...${NC}"
wait $TEST_PID
TEST_EXIT=$?
END_TIME=$(date +%s)
DURATION_SEC=$((END_TIME - START_TIME))

# Stop resource monitoring
kill $MONITOR_PID 2>/dev/null || true
wait $MONITOR_PID 2>/dev/null || true

if [ $TEST_EXIT -eq 0 ]; then
    echo -e "${GREEN}✓ Test completed successfully in ${DURATION_SEC}s${NC}"
else
    echo -e "${RED}✗ Test failed with exit code: $TEST_EXIT${NC}"
fi

echo ""
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}                      Test Results                          ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Extract final metrics
if [ -f "$TEST_LOG" ]; then
    echo -e "${CYAN}Final Metrics:${NC}"
    FINAL_METRICS=$(grep "METRICS:" "$TEST_LOG" | tail -1 | sed 's/.*METRICS: //')
    if [ -n "$FINAL_METRICS" ]; then
        echo "$FINAL_METRICS"
    else
        echo -e "${YELLOW}No metrics found in log${NC}"
    fi
    echo ""
    
    # Extract flush errors if any
    FLUSH_ERROR_COUNT=$(grep -c "FLUSH_ERROR" "$TEST_LOG" 2>/dev/null || echo "0")
    # Remove any newlines/whitespace from the count
    FLUSH_ERROR_COUNT=$(echo "$FLUSH_ERROR_COUNT" | tr -d '\n\r ')
    if [ -n "$FLUSH_ERROR_COUNT" ] && [ "$FLUSH_ERROR_COUNT" -gt 0 ] 2>/dev/null; then
        echo -e "${YELLOW}⚠ Flush Errors: $FLUSH_ERROR_COUNT${NC}"
        grep "FLUSH_ERROR" "$TEST_LOG" | tail -5
        echo ""
    fi
    
    # Extract final statistics
    echo -e "${CYAN}Final Statistics:${NC}"
    grep "Final Statistics:" -A 10 "$TEST_LOG" | tail -10
    echo ""
fi

# Summary
echo -e "${CYAN}Results Location:${NC}"
echo "  Test log:      $TEST_LOG"
echo "  Resource data: $RESOURCE_FILE"
echo "  Log files:     $LOG_DIR/"
echo ""
echo -e "${GREEN}Test completed at: $(date)${NC}"

