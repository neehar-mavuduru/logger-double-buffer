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
echo -e "${CYAN}    Multi-Event Logger Test - VM Execution                  ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/multi_event_test_${TIMESTAMP}"
LOG_DIR="logs"
DURATION="10m"
THREADS=100
LOG_SIZE_KB=300
BUFFER_MB=64
SHARDS=8
FLUSH_INTERVAL="10s"
ROTATION_INTERVAL="24h"

# Event configuration (can be overridden via command line)
EVENT1_NAME="event1"
EVENT1_RPS=350
EVENT1_THREADS=35

EVENT2_NAME="event2"
EVENT2_RPS=350
EVENT2_THREADS=35

EVENT3_NAME="event3"
EVENT3_RPS=300
EVENT3_THREADS=30

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
        --event1-rps)
            EVENT1_RPS="$2"
            shift 2
            ;;
        --event2-rps)
            EVENT2_RPS="$2"
            shift 2
            ;;
        --event3-rps)
            EVENT3_RPS="$2"
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
            echo "  --duration DURATION     Test duration (default: 10m)"
            echo "  --threads THREADS        Total threads (default: 100, distributed across events)"
            echo "  --buffer-mb MB           Buffer size in MB (default: 64)"
            echo "  --shards SHARDS         Number of shards (default: 8)"
            echo "  --log-size-kb KB         Log size in KB (default: 300)"
            echo "  --event1-rps RPS         Event1 RPS (default: 350)"
            echo "  --event2-rps RPS         Event2 RPS (default: 350)"
            echo "  --event3-rps RPS         Event3 RPS (default: 300)"
            echo "  --rotation-interval DURATION  File rotation interval (default: 24h, use 0 to disable)"
            echo "  --help                  Show this help"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Calculate total RPS and thread distribution
TOTAL_RPS=$((EVENT1_RPS + EVENT2_RPS + EVENT3_RPS))
EVENT1_THREADS=$((THREADS * EVENT1_RPS / TOTAL_RPS))
EVENT2_THREADS=$((THREADS * EVENT2_RPS / TOTAL_RPS))
EVENT3_THREADS=$((THREADS - EVENT1_THREADS - EVENT2_THREADS))

# Ensure minimum 1 thread per event
if [ $EVENT1_THREADS -lt 1 ]; then EVENT1_THREADS=1; fi
if [ $EVENT2_THREADS -lt 1 ]; then EVENT2_THREADS=1; fi
if [ $EVENT3_THREADS -lt 1 ]; then EVENT3_THREADS=1; fi

# Create directories
mkdir -p "$RESULTS_DIR"
mkdir -p "$LOG_DIR"

# Cleanup function
cleanup() {
    echo ""
    echo -e "${YELLOW}Cleaning up...${NC}"
    
    # Kill all test processes
    pkill -f "direct_logger_test.*event" 2>/dev/null || true
    sleep 1
    
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
    echo -e "${RED}Error: bin/direct_logger_test not found${NC}"
    echo "Building binary..."
    go build -o bin/direct_logger_test ./cmd/direct_logger_test
    if [ ! -f "bin/direct_logger_test" ]; then
        echo -e "${RED}Failed to build binary${NC}"
        exit 1
    fi
fi

# Resource monitoring function
start_resource_monitoring() {
    echo -e "${BLUE}Starting resource monitoring...${NC}"
    
    MONITOR_LOG="$RESULTS_DIR/resource_monitor.log"
    (
        echo "Timestamp,CPU%,Mem%,DiskReadMB/s,DiskWriteMB/s,DiskIOPS"
        while true; do
            TIMESTAMP=$(date +%s)
            CPU=$(top -bn1 | grep "Cpu(s)" | sed "s/.*, *\([0-9.]*\)%* id.*/\1/" | awk '{print 100 - $1}')
            MEM=$(free | grep Mem | awk '{printf "%.1f", $3/$2 * 100.0}')
            
            # Disk I/O stats (if iostat available)
            if command -v iostat >/dev/null 2>&1; then
                IOSTAT=$(iostat -x 1 1 | tail -n +4 | awk '{sum+=$6+$7} END {print sum}')
                DISK_READ=$(iostat -d 1 1 | tail -n +4 | awk '{sum+=$3} END {print sum}')
                DISK_WRITE=$(iostat -d 1 1 | tail -n +4 | awk '{sum+=$4} END {print sum}')
            else
                # Fallback: use /proc/diskstats
                DISK_STATS=$(cat /proc/diskstats | grep -E "sd[a-z]|nvme" | head -1)
                if [ -n "$DISK_STATS" ]; then
                    DISK_READ=$(echo $DISK_STATS | awk '{print $6*512/1024/1024}')  # sectors to MB
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
echo "  Total Threads: $THREADS"
echo "  Total RPS: $TOTAL_RPS"
echo "  Buffer: ${BUFFER_MB}MB, Shards: $SHARDS"
echo "  Log Size: ${LOG_SIZE_KB}KB"
echo "  Rotation Interval: $ROTATION_INTERVAL"
echo ""
echo -e "${CYAN}Event Distribution:${NC}"
echo "  $EVENT1_NAME: ${EVENT1_RPS} RPS, $EVENT1_THREADS threads"
echo "  $EVENT2_NAME: ${EVENT2_RPS} RPS, $EVENT2_THREADS threads"
echo "  $EVENT3_NAME: ${EVENT3_RPS} RPS, $EVENT3_THREADS threads"
echo ""

# Start resource monitoring
start_resource_monitoring

# Clean up old log files
echo -e "${BLUE}Cleaning up old log files...${NC}"
rm -f "$LOG_DIR"/event*.log 2>/dev/null || true
echo -e "${GREEN}✓ Cleanup complete${NC}"

# Start event1 test
echo -e "${CYAN}Starting $EVENT1_NAME test...${NC}"
EVENT1_LOG="$RESULTS_DIR/${EVENT1_NAME}_test.log"
./bin/direct_logger_test \
    --threads "$EVENT1_THREADS" \
    --log-size-kb "$LOG_SIZE_KB" \
    --rps "$EVENT1_RPS" \
    --duration "$DURATION" \
    --buffer-mb "$BUFFER_MB" \
    --shards "$SHARDS" \
    --flush-interval "$FLUSH_INTERVAL" \
    --rotation-interval "$ROTATION_INTERVAL" \
    --log-dir "$LOG_DIR" \
    --use-events \
    --event "$EVENT1_NAME" \
    > "$EVENT1_LOG" 2>&1 &
EVENT1_PID=$!
echo -e "${GREEN}✓ $EVENT1_NAME started (PID: $EVENT1_PID)${NC}"

# Start event2 test
echo -e "${CYAN}Starting $EVENT2_NAME test...${NC}"
EVENT2_LOG="$RESULTS_DIR/${EVENT2_NAME}_test.log"
./bin/direct_logger_test \
    --threads "$EVENT2_THREADS" \
    --log-size-kb "$LOG_SIZE_KB" \
    --rps "$EVENT2_RPS" \
    --duration "$DURATION" \
    --buffer-mb "$BUFFER_MB" \
    --shards "$SHARDS" \
    --flush-interval "$FLUSH_INTERVAL" \
    --rotation-interval "$ROTATION_INTERVAL" \
    --log-dir "$LOG_DIR" \
    --use-events \
    --event "$EVENT2_NAME" \
    > "$EVENT2_LOG" 2>&1 &
EVENT2_PID=$!
echo -e "${GREEN}✓ $EVENT2_NAME started (PID: $EVENT2_PID)${NC}"

# Start event3 test
echo -e "${CYAN}Starting $EVENT3_NAME test...${NC}"
EVENT3_LOG="$RESULTS_DIR/${EVENT3_NAME}_test.log"
./bin/direct_logger_test \
    --threads "$EVENT3_THREADS" \
    --log-size-kb "$LOG_SIZE_KB" \
    --rps "$EVENT3_RPS" \
    --duration "$DURATION" \
    --buffer-mb "$BUFFER_MB" \
    --shards "$SHARDS" \
    --flush-interval "$FLUSH_INTERVAL" \
    --rotation-interval "$ROTATION_INTERVAL" \
    --log-dir "$LOG_DIR" \
    --use-events \
    --event "$EVENT3_NAME" \
    > "$EVENT3_LOG" 2>&1 &
EVENT3_PID=$!
echo -e "${GREEN}✓ $EVENT3_NAME started (PID: $EVENT3_PID)${NC}"

echo ""
echo -e "${CYAN}All tests started. Waiting for completion...${NC}"
echo "  Monitor logs: tail -f $RESULTS_DIR/*_test.log"
echo ""

# Wait for all tests to complete
wait $EVENT1_PID
EVENT1_EXIT=$?
wait $EVENT2_PID
EVENT2_EXIT=$?
wait $EVENT3_PID
EVENT3_EXIT=$?

# Stop resource monitoring
kill $MONITOR_PID 2>/dev/null || true
wait $MONITOR_PID 2>/dev/null || true

# Check exit codes
if [ $EVENT1_EXIT -ne 0 ] || [ $EVENT2_EXIT -ne 0 ] || [ $EVENT3_EXIT -ne 0 ]; then
    echo -e "${RED}One or more tests failed!${NC}"
    [ $EVENT1_EXIT -ne 0 ] && echo "  $EVENT1_NAME: Exit code $EVENT1_EXIT"
    [ $EVENT2_EXIT -ne 0 ] && echo "  $EVENT2_NAME: Exit code $EVENT2_EXIT"
    [ $EVENT3_EXIT -ne 0 ] && echo "  $EVENT3_NAME: Exit code $EVENT3_EXIT"
fi

# Extract final metrics from logs
extract_metrics() {
    local log_file=$1
    local event_name=$2
    
    if [ ! -f "$log_file" ]; then
        echo "N/A,N/A,N/A,N/A,N/A,N/A,N/A"
        return
    fi
    
    # Extract final metrics line
    FINAL_LINE=$(grep "Final Metrics:" "$log_file" -A 1 | tail -1)
    
    if [ -z "$FINAL_LINE" ]; then
        echo "N/A,N/A,N/A,N/A,N/A,N/A,N/A"
        return
    fi
    
    # Parse metrics: Logs, Dropped, DropRate%, Bytes, Flushes, Errors, Swaps, AvgFlush, MaxFlush, AvgWrite, MaxWrite, WritePct, GC, Mem
    echo "$FINAL_LINE" | awk -F'|' '{
        # Extract values
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", $1)  # Logs
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2)  # Dropped
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", $3)  # Bytes
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", $4)  # Flushes
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", $5)  # Errors
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", $6)  # Swaps
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", $7)  # AvgFlush
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", $8)  # MaxFlush
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", $9)  # AvgWrite
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", $10) # MaxWrite
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", $11) # WritePct
        
        # Extract numeric values
        logs = $1; gsub(/[^0-9]/, "", logs)
        dropped = $2; gsub(/[^0-9.]/, "", dropped)
        bytes = $3; gsub(/[^0-9]/, "", bytes)
        flushes = $4; gsub(/[^0-9]/, "", flushes)
        errors = $5; gsub(/[^0-9]/, "", errors)
        avg_flush = $7; gsub(/[^0-9.]/, "", avg_flush)
        max_flush = $8; gsub(/[^0-9.]/, "", max_flush)
        
        print logs "," dropped "," bytes "," flushes "," errors "," avg_flush "," max_flush
    }'
}

# Generate summary report
SUMMARY_FILE="$RESULTS_DIR/summary.txt"
echo -e "${CYAN}Generating summary report...${NC}"

cat > "$SUMMARY_FILE" << EOF
════════════════════════════════════════════════════════════
    Multi-Event Logger Test Results
    Timestamp: $(date)
════════════════════════════════════════════════════════════

Configuration:
  Duration: $DURATION
  Total Threads: $THREADS
  Total RPS: $TOTAL_RPS
  Buffer: ${BUFFER_MB}MB, Shards: $SHARDS
  Log Size: ${LOG_SIZE_KB}KB

Event Distribution:
  $EVENT1_NAME: ${EVENT1_RPS} RPS, $EVENT1_THREADS threads
  $EVENT2_NAME: ${EVENT2_RPS} RPS, $EVENT2_THREADS threads
  $EVENT3_NAME: ${EVENT3_RPS} RPS, $EVENT3_THREADS threads

════════════════════════════════════════════════════════════
Results:
════════════════════════════════════════════════════════════

EOF

# Extract and add metrics for each event
for EVENT in "$EVENT1_NAME" "$EVENT2_NAME" "$EVENT3_NAME"; do
    LOG_FILE="$RESULTS_DIR/${EVENT}_test.log"
    echo "Event: $EVENT" >> "$SUMMARY_FILE"
    echo "----------------------------------------" >> "$SUMMARY_FILE"
    
    if [ -f "$LOG_FILE" ]; then
        # Extract key metrics
        FINAL_METRICS=$(grep "Final Metrics:" "$LOG_FILE" -A 1 | tail -1)
        if [ -n "$FINAL_METRICS" ]; then
            echo "$FINAL_METRICS" >> "$SUMMARY_FILE"
        else
            echo "No final metrics found" >> "$SUMMARY_FILE"
        fi
        
        # Extract flush metrics if available
        FLUSH_METRICS=$(grep "AvgFlush\|MaxFlush\|AvgWrite\|MaxWrite\|WritePct" "$LOG_FILE" | tail -1)
        if [ -n "$FLUSH_METRICS" ]; then
            echo "$FLUSH_METRICS" >> "$SUMMARY_FILE"
        fi
    else
        echo "Log file not found: $LOG_FILE" >> "$SUMMARY_FILE"
    fi
    echo "" >> "$SUMMARY_FILE"
done

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

# Display summary
cat "$SUMMARY_FILE"

echo ""
echo -e "${GREEN}✓ Test completed${NC}"
echo -e "${CYAN}Results saved to: $RESULTS_DIR${NC}"
echo "  Summary: $SUMMARY_FILE"
echo "  Event logs: $RESULTS_DIR/*_test.log"
echo "  Resource monitor: $RESULTS_DIR/resource_monitor.log"
echo "  Log files: $LOG_DIR/event*.log"


