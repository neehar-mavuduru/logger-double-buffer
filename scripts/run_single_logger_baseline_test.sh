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
echo -e "${CYAN}    Single Logger Baseline Test - 100T, 64MB, 8S, 1000RPS  ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/single_logger_baseline_test"
SERVER_PORT=8585
DURATION="10m"
THREADS=100
BUFFER_MB=64
SHARDS=8
FLUSH_INTERVAL="10s"
RPS=1000
LOG_DIR="logs"

# Create results and logs directories
mkdir -p "$RESULTS_DIR"
mkdir -p "$LOG_DIR"

# Server process tracking
SERVER_PID=""
SERVER_LOG="$RESULTS_DIR/server_${TIMESTAMP}.log"

# Function to clean up old test logs
cleanup_old_logs() {
    echo -e "${BLUE}Cleaning up old test logs...${NC}"
    
    # Clean up old log files
    if [ -d "$LOG_DIR" ]; then
        find "$LOG_DIR" -name "*.log" -type f -mtime +1 -delete 2>/dev/null || true
    fi
    
    # Clean up old result files (keep last 3 runs)
    if [ -d "$RESULTS_DIR" ]; then
        find "$RESULTS_DIR" -name "*.log" -o -name "*.json" -o -name "*.csv" | sort -r | tail -n +4 | xargs rm -f 2>/dev/null || true
    fi
    
    # Kill any existing server processes
    pkill -f "./bin/server" 2>/dev/null || true
    sleep 1
}

# Function to clean up test logs
cleanup_test_logs() {
    echo -e "${BLUE}Cleaning up test logs...${NC}"
    if [ -d "$LOG_DIR" ]; then
        echo "  Removing log files from $LOG_DIR..."
        rm -f "$LOG_DIR"/*.log 2>/dev/null || true
        echo -e "${GREEN}✓ Test logs cleaned up${NC}"
    fi
}

# Function to check if port is available
check_port() {
    if lsof -Pi :$SERVER_PORT -sTCP:LISTEN -t >/dev/null 2>&1 ; then
        echo -e "${RED}✗ Port $SERVER_PORT is already in use${NC}"
        echo "  Please stop the process using this port or change SERVER_PORT"
        exit 1
    fi
}

# Cleanup function
cleanup() {
    echo ""
    echo -e "${YELLOW}Cleaning up...${NC}"
    
    # Stop monitoring
    if [ -n "${MONITOR_PID:-}" ]; then
        kill $MONITOR_PID 2>/dev/null || true
        wait $MONITOR_PID 2>/dev/null || true
    fi
    
    # Stop server
    if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
        echo -e "${YELLOW}Stopping server...${NC}"
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    
    # Clean up logs
    cleanup_test_logs
}

# Set trap for cleanup on exit
trap cleanup EXIT

# Clean up old logs
cleanup_old_logs

echo "Run started at: $(date)"
echo "Results directory: $RESULTS_DIR"
echo "Test configuration:"
echo "  - Threads: $THREADS"
echo "  - Buffer: ${BUFFER_MB}MB"
echo "  - Shards: $SHARDS"
echo "  - RPS: $RPS"
echo "  - Duration: $DURATION"
echo "  - Flush Interval: $FLUSH_INTERVAL"
echo ""

# Check if server binary exists
if [ ! -f "./bin/server" ]; then
    echo -e "${RED}✗ Server binary not found at ./bin/server${NC}"
    echo "  Please build the server first: go build -o bin/server ./server"
    exit 1
fi

# Check port availability
check_port

# Start server
echo -e "${BLUE}Starting server...${NC}"
BUFFER_SIZE=$((BUFFER_MB * 1024 * 1024))
NUM_SHARDS=$SHARDS
FLUSH_INTERVAL=$FLUSH_INTERVAL
LOG_FILE="$LOG_DIR/server.log"

# Start server in background
./bin/server \
    -log-buffer-size=$BUFFER_SIZE \
    -log-num-shards=$NUM_SHARDS \
    -log-flush-interval=$FLUSH_INTERVAL \
    -log-file=$LOG_FILE \
    > "$SERVER_LOG" 2>&1 &
SERVER_PID=$!

# Wait for server to be ready
echo -e "${BLUE}Waiting for server to be ready...${NC}"
for i in {1..30}; do
    if grep -q "Listening on" "$SERVER_LOG" 2>/dev/null; then
        echo -e "${GREEN}✓ Server ready!${NC}"
        break
    fi
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        echo -e "${RED}✗ Server failed to start${NC}"
        echo "Last 20 lines of server log:"
        tail -20 "$SERVER_LOG"
        exit 1
    fi
    sleep 1
done

# Start resource monitoring in background
echo -e "${BLUE}Starting resource monitoring...${NC}"
{
    echo "timestamp,cpu_percent,mem_usage_mb,mem_percent,disk_usage_mb,disk_available_mb,disk_usage_pct,io_read_mb,io_write_mb"
    while kill -0 "$SERVER_PID" 2>/dev/null; do
        # Get process stats using ps and /proc
        if [ -f "/proc/$SERVER_PID/stat" ]; then
            # CPU percentage (simplified - uses ps)
            CPU=$(ps -p "$SERVER_PID" -o %cpu --no-headers 2>/dev/null | tr -d ' ' || echo "0")
            
            # Memory stats from /proc
            MEM_INFO=$(cat /proc/$SERVER_PID/status 2>/dev/null | grep -E "VmRSS|VmSize" || echo "")
            MEM_RSS=$(echo "$MEM_INFO" | grep VmRSS | awk '{print $2}' || echo "0")
            MEM_USAGE=$((MEM_RSS / 1024))  # Convert KB to MB
            MEM_TOTAL=$(free -m | awk '/^Mem:/{print $2}')
            MEM_PCT=$(awk "BEGIN {printf \"%.2f\", ($MEM_USAGE/$MEM_TOTAL)*100}" 2>/dev/null || echo "0")
            
            # Disk I/O stats from /proc
            IO_STATS=$(cat /proc/$SERVER_PID/io 2>/dev/null || echo "")
            IO_READ=$(echo "$IO_STATS" | grep read_bytes | awk '{print $2}' || echo "0")
            IO_WRITE=$(echo "$IO_STATS" | grep write_bytes | awk '{print $2}' || echo "0")
            IO_READ_MB=$((IO_READ / 1024 / 1024))
            IO_WRITE_MB=$((IO_WRITE / 1024 / 1024))
            
            # Disk space monitoring
            DISK_INFO=$(df -m "$LOG_DIR" 2>/dev/null | tail -1 || echo "0 0 0 0")
            DISK_TOTAL=$(echo "$DISK_INFO" | awk '{print $2}')
            DISK_USED=$(echo "$DISK_INFO" | awk '{print $3}')
            DISK_AVAIL=$(echo "$DISK_INFO" | awk '{print $4}')
            DISK_PCT=$(echo "$DISK_INFO" | awk '{print $5}' | sed 's/%//' || echo "0")
            
            TIMESTAMP=$(date +%s)
            echo "$TIMESTAMP,$CPU,$MEM_USAGE,$MEM_PCT,$DISK_USED,$DISK_AVAIL,$DISK_PCT,$IO_READ_MB,$IO_WRITE_MB"
        fi
        sleep 1
    done
} > "$RESULTS_DIR/resource_timeline_${TIMESTAMP}.csv" 2>/dev/null &
MONITOR_PID=$!

# Run load test
echo -e "${BLUE}Running load test (${RPS} RPS, ${THREADS} threads, ${DURATION})...${NC}"
ghz --insecure \
    --proto proto/random_numbers.proto \
    --call randomnumbers.RandomNumberService.GetRandomNumbers \
    --rps $RPS \
    --duration $DURATION \
    --concurrency $THREADS \
    --format json \
    --output "$RESULTS_DIR/ghz_${TIMESTAMP}.json" \
    localhost:$SERVER_PORT > /dev/null 2>&1
GHZ_EXIT=$?

if [ $GHZ_EXIT -eq 0 ]; then
    echo -e "${GREEN}✓ Load test completed!${NC}"
else
    echo -e "${RED}⚠ Load test failed with exit code: $GHZ_EXIT${NC}"
fi

# Wait a moment for final metrics
sleep 2

# Copy server log to results
echo -e "${BLUE}Collecting server logs...${NC}"
if [ -f "$SERVER_LOG" ]; then
    cp "$SERVER_LOG" "$RESULTS_DIR/server_${TIMESTAMP}.log"
else
    echo "Server log not found at $SERVER_LOG"
fi

# Extract flush errors for analysis
echo -e "${BLUE}Analyzing flush errors...${NC}"
grep "FLUSH_ERROR" "$RESULTS_DIR/server_${TIMESTAMP}.log" > "$RESULTS_DIR/flush_errors_${TIMESTAMP}.log" 2>/dev/null || echo "No flush errors found" > "$RESULTS_DIR/flush_errors_${TIMESTAMP}.log"
FLUSH_ERROR_COUNT=$(grep -c "FLUSH_ERROR" "$RESULTS_DIR/server_${TIMESTAMP}.log" 2>/dev/null || echo "0")
if [ "$FLUSH_ERROR_COUNT" -gt 0 ]; then
    echo -e "${YELLOW}⚠ Found $FLUSH_ERROR_COUNT flush errors - see flush_errors_${TIMESTAMP}.log${NC}"
fi

# Stop monitoring
kill $MONITOR_PID 2>/dev/null || true
wait $MONITOR_PID 2>/dev/null || true

# Clean up test logs before stopping server
cleanup_test_logs
echo ""

# Stop server (cleanup function will handle this, but explicit stop for clarity)
if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    echo -e "${YELLOW}Stopping server...${NC}"
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
fi

echo -e "${GREEN}✓ Single logger baseline test complete!${NC}"
echo ""
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}                      Test Results                          ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${BLUE}Results Directory:${NC} $RESULTS_DIR"
echo ""
echo -e "${BLUE}Result Files:${NC}"
echo -e "  - Server log:        server_${TIMESTAMP}.log"
echo -e "  - ghz report:        ghz_${TIMESTAMP}.json"
echo -e "  - Resource timeline: resource_timeline_${TIMESTAMP}.csv"
if [ "$FLUSH_ERROR_COUNT" -gt 0 ]; then
    echo -e "  - Flush errors:      flush_errors_${TIMESTAMP}.log"
fi
echo ""

# Display final metrics
echo -e "${CYAN}                  Final Metrics Summary                   ${NC}"
LAST_METRICS=$(grep "METRICS:" "$RESULTS_DIR/server_${TIMESTAMP}.log" | tail -1)
if [ -n "$LAST_METRICS" ]; then
    echo "$LAST_METRICS" | sed 's/.*METRICS: //'
fi

# Display gRPC results if available
if [ -f "$RESULTS_DIR/ghz_${TIMESTAMP}.json" ]; then
    echo ""
    echo -e "${CYAN}                      gRPC Results                       ${NC}"
    if command -v jq >/dev/null 2>&1; then
        RPS_RESULT=$(jq -r '.rps' "$RESULTS_DIR/ghz_${TIMESTAMP}.json" 2>/dev/null || echo "N/A")
        LATENCY_AVG=$(jq -r '.latency.average' "$RESULTS_DIR/ghz_${TIMESTAMP}.json" 2>/dev/null || echo "N/A")
        LATENCY_P99=$(jq -r '.latency.p99' "$RESULTS_DIR/ghz_${TIMESTAMP}.json" 2>/dev/null || echo "N/A")
        TOTAL_REQUESTS=$(jq -r '.count' "$RESULTS_DIR/ghz_${TIMESTAMP}.json" 2>/dev/null || echo "N/A")
        ERRORS=$(jq -r '.errorCount' "$RESULTS_DIR/ghz_${TIMESTAMP}.json" 2>/dev/null || echo "N/A")
        
        echo "  RPS:        $RPS_RESULT"
        echo "  Avg Latency: $LATENCY_AVG"
        echo "  P99 Latency: $LATENCY_P99"
        echo "  Total Reqs:  $TOTAL_REQUESTS"
        echo "  Errors:      $ERRORS"
    else
        echo "  Install 'jq' to parse JSON results automatically"
        echo "  Results file: $RESULTS_DIR/ghz_${TIMESTAMP}.json"
    fi
fi

echo ""
echo -e "${GREEN}Test completed successfully!${NC}"
echo ""







