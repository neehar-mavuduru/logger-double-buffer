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
echo -e "${CYAN}    Single Logger Test - VM Execution                      ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/single_logger_test_${TIMESTAMP}"
LOG_DIR="logs"
DURATION="10m"
THREADS=100
LOG_SIZE_KB=300
TARGET_RPS=1000
BUFFER_MB=64
SHARDS=8
FLUSH_INTERVAL="10s"
ROTATION_INTERVAL="24h"

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
        --log-size-kb)
            LOG_SIZE_KB="$2"
            shift 2
            ;;
        --rotation-interval)
            ROTATION_INTERVAL="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --duration DURATION        Test duration (default: 10m)"
            echo "  --threads THREADS          Number of threads (default: 100)"
            echo "  --rps RPS                  Target RPS (default: 1000)"
            echo "  --buffer-mb MB             Buffer size in MB (default: 64)"
            echo "  --shards SHARDS           Number of shards (default: 8)"
            echo "  --log-size-kb KB           Log size in KB (default: 300)"
            echo "  --rotation-interval DURATION  File rotation interval (default: 24h, use 0 to disable)"
            echo "  --help                     Show this help"
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
    
    # Kill resource monitoring
    if [ -n "${MONITOR_PID:-}" ]; then
        kill "$MONITOR_PID" 2>/dev/null || true
        wait "$MONITOR_PID" 2>/dev/null || true
    fi
    
    echo -e "${GREEN}✓ Cleanup complete${NC}"
}

trap cleanup EXIT INT TERM

# Check if binary exists
if [ ! -f "bin/direct_logger_test" ]; then
    echo -e "${YELLOW}Binary not found. Building...${NC}"
    go build -o bin/direct_logger_test ./cmd/direct_logger_test
    if [ ! -f "bin/direct_logger_test" ]; then
        echo -e "${RED}Failed to build binary${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ Binary built successfully${NC}"
fi

# Resource monitoring function
start_resource_monitoring() {
    echo -e "${BLUE}Starting resource monitoring...${NC}"
    
    MONITOR_LOG="$RESULTS_DIR/resource_monitor.log"
    (
        echo "Timestamp,CPU%,Mem%,DiskReadMB/s,DiskWriteMB/s,DiskIOPS"
        while true; do
            TIMESTAMP=$(date +%s)
            
            # CPU usage
            CPU=$(top -bn1 | grep "Cpu(s)" | sed "s/.*, *\([0-9.]*\)%* id.*/\1/" | awk '{print 100 - $1}')
            
            # Memory usage
            MEM=$(free | grep Mem | awk '{printf "%.1f", $3/$2 * 100.0}')
            
            # Disk I/O stats
            if command -v iostat >/dev/null 2>&1; then
                DISK_READ=$(iostat -d 1 1 2>/dev/null | tail -n +4 | awk '{sum+=$3} END {print sum+0}')
                DISK_WRITE=$(iostat -d 1 1 2>/dev/null | tail -n +4 | awk '{sum+=$4} END {print sum+0}')
                IOSTAT=$(iostat -x 1 1 2>/dev/null | tail -n +4 | awk '{sum+=$6+$7} END {print sum+0}')
            else
                # Fallback: use /proc/diskstats
                DISK_STATS=$(cat /proc/diskstats 2>/dev/null | grep -E "sd[a-z]|nvme" | head -1 || echo "")
                if [ -n "$DISK_STATS" ]; then
                    DISK_READ=$(echo $DISK_STATS | awk '{print $6*512/1024/1024}')
                    DISK_WRITE=$(echo $DISK_STATS | awk '{print $10*512/1024/1024}')
                    IOSTAT="0"
                else
                    DISK_READ="0"
                    DISK_WRITE="0"
                    IOSTAT="0"
                fi
            fi
            
            echo "$TIMESTAMP,$CPU,$MEM,$DISK_READ,$DISK_WRITE,$IOSTAT"
            sleep 5
        done
    ) > "$MONITOR_LOG" 2>&1 &
    MONITOR_PID=$!
    echo -e "${GREEN}✓ Resource monitoring started (PID: $MONITOR_PID)${NC}"
}

# Print configuration
echo -e "${CYAN}Test Configuration:${NC}"
echo "  Duration: $DURATION"
echo "  Threads: $THREADS"
echo "  Target RPS: $TARGET_RPS"
echo "  Buffer: ${BUFFER_MB}MB, Shards: $SHARDS"
echo "  Log Size: ${LOG_SIZE_KB}KB"
echo "  Rotation Interval: $ROTATION_INTERVAL"
echo ""

# Start resource monitoring
start_resource_monitoring

# Clean up old log files
echo -e "${BLUE}Cleaning up old log files...${NC}"
rm -f "$LOG_DIR"/direct_test*.log 2>/dev/null || true
echo -e "${GREEN}✓ Cleanup complete${NC}"

# Run test
echo -e "${CYAN}Starting single logger test...${NC}"
TEST_LOG="$RESULTS_DIR/test_output.log"

./bin/direct_logger_test \
    --threads "$THREADS" \
    --log-size-kb "$LOG_SIZE_KB" \
    --rps "$TARGET_RPS" \
    --duration "$DURATION" \
    --buffer-mb "$BUFFER_MB" \
    --shards "$SHARDS" \
    --flush-interval "$FLUSH_INTERVAL" \
    --rotation-interval "$ROTATION_INTERVAL" \
    --log-dir "$LOG_DIR" \
    > "$TEST_LOG" 2>&1 &
TEST_PID=$!

echo -e "${GREEN}✓ Test started (PID: $TEST_PID)${NC}"
echo "  Monitor logs: tail -f $TEST_LOG"
echo ""

# Wait for test to complete
wait $TEST_PID
TEST_EXIT=$?

# Stop resource monitoring
kill $MONITOR_PID 2>/dev/null || true
wait $MONITOR_PID 2>/dev/null || true

# Check exit code
if [ $TEST_EXIT -ne 0 ]; then
    echo -e "${RED}Test failed with exit code $TEST_EXIT${NC}"
    exit 1
fi

# Extract final metrics
FINAL_METRICS=$(grep "Final Metrics:" "$TEST_LOG" -A 1 | tail -1 || echo "N/A")

# Generate summary report
SUMMARY_FILE="$RESULTS_DIR/summary.txt"
cat > "$SUMMARY_FILE" << EOF
════════════════════════════════════════════════════════════
    Single Logger Test Results
    Timestamp: $(date)
════════════════════════════════════════════════════════════

Configuration:
  Duration: $DURATION
  Threads: $THREADS
  Target RPS: $TARGET_RPS
  Buffer: ${BUFFER_MB}MB, Shards: $SHARDS
  Log Size: ${LOG_SIZE_KB}KB
  Rotation Interval: $ROTATION_INTERVAL

════════════════════════════════════════════════════════════
Results:
════════════════════════════════════════════════════════════

$FINAL_METRICS

EOF

# Add resource usage summary
if [ -f "$RESULTS_DIR/resource_monitor.log" ]; then
    echo "════════════════════════════════════════════════════════════" >> "$SUMMARY_FILE"
    echo "Resource Usage Summary:" >> "$SUMMARY_FILE"
    echo "════════════════════════════════════════════════════════════" >> "$SUMMARY_FILE"
    
    # Calculate averages (skip header)
    tail -n +2 "$RESULTS_DIR/resource_monitor.log" | awk -F',' '{
        cpu_sum+=$2; mem_sum+=$3; disk_read_sum+=$4; disk_write_sum+=$5; iops_sum+=$6; count++
    } END {
        if (count > 0) {
            printf "Average CPU: %.2f%%\n", cpu_sum/count
            printf "Average Memory: %.2f%%\n", mem_sum/count
            printf "Average Disk Read: %.2f MB/s\n", disk_read_sum/count
            printf "Average Disk Write: %.2f MB/s\n", disk_write_sum/count
            printf "Average Disk IOPS: %.2f\n", iops_sum/count
        }
    }' >> "$SUMMARY_FILE"
fi

# Check for flush errors
FLUSH_ERRORS=$(grep -c "FLUSH_ERROR" "$TEST_LOG" 2>/dev/null || echo "0")
if [ "$FLUSH_ERRORS" -gt 0 ]; then
    echo "" >> "$SUMMARY_FILE"
    echo "⚠ Flush Errors: $FLUSH_ERRORS" >> "$SUMMARY_FILE"
    grep "FLUSH_ERROR" "$TEST_LOG" | tail -5 >> "$SUMMARY_FILE"
fi

# Display summary
cat "$SUMMARY_FILE"

echo ""
echo -e "${GREEN}✓ Test completed${NC}"
echo -e "${CYAN}Results saved to: $RESULTS_DIR${NC}"
echo "  Summary: $SUMMARY_FILE"
echo "  Test output: $TEST_LOG"
echo "  Resource monitor: $RESULTS_DIR/resource_monitor.log"
echo "  Log file: $LOG_DIR/direct_test.log"

# List rotated files if any
ROTATED_FILES=$(ls -1 "$LOG_DIR"/direct_test_*.log 2>/dev/null | wc -l)
if [ "$ROTATED_FILES" -gt 0 ]; then
    echo ""
    echo -e "${CYAN}Rotated files found: $ROTATED_FILES${NC}"
    ls -lh "$LOG_DIR"/direct_test_*.log
fi

