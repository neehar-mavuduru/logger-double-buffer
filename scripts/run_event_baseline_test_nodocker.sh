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
echo -e "${CYAN}    Event-Based Baseline Test - 3 Events, 100T, 64MB, 8S   ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/event_baseline_test"
SERVER_PORT=8585
DURATION="10m"
THREADS=100
BUFFER_MB=64
SHARDS=8
FLUSH_INTERVAL="10s"
LOG_DIR="logs"

# Event configuration
EVENT1_RPS=350
EVENT2_RPS=350
EVENT3_RPS=300
TOTAL_RPS=$((EVENT1_RPS + EVENT2_RPS + EVENT3_RPS))

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
        OLD_RESULTS=$(ls -t "$RESULTS_DIR"/*.log 2>/dev/null | tail -n +4 2>/dev/null || true)
        if [ -n "$OLD_RESULTS" ]; then
            echo "  Removing old result files..."
            echo "$OLD_RESULTS" | xargs rm -f 2>/dev/null || true
        fi
    fi
    
    echo -e "${GREEN}✓ Cleanup complete${NC}"
}

# Function to clean up event logs after test completion
cleanup_test_logs() {
    echo -e "${BLUE}Cleaning up event logs created during test...${NC}"
    
    if [ -d "$LOG_DIR" ]; then
        echo "  Removing event log files from $LOG_DIR..."
        rm -f "$LOG_DIR"/event*.log "$LOG_DIR"/*.log 2>/dev/null || true
        echo -e "${GREEN}✓ Event logs cleaned up${NC}"
    else
        echo -e "${YELLOW}⚠ Log directory not found${NC}"
    fi
}

# Function to check if port is in use
check_port() {
    if lsof -Pi :$SERVER_PORT -sTCP:LISTEN -t >/dev/null 2>&1 ; then
        echo -e "${RED}✗ Port $SERVER_PORT is already in use${NC}"
        echo "  Please stop the process using this port or change SERVER_PORT"
        exit 1
    fi
}

# Function to cleanup on exit
cleanup() {
    echo ""
    echo -e "${YELLOW}Cleaning up...${NC}"
    
    # Stop server if running
    if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
        echo "  Stopping server (PID: $SERVER_PID)..."
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    
    # Kill any remaining server processes
    pkill -f "./bin/server" 2>/dev/null || true
    
    cleanup_test_logs
}

trap cleanup EXIT INT TERM

echo "Run started at: $(date)"
echo "Results directory: $RESULTS_DIR"
echo "Test configuration:"
echo "  - Total Threads: $THREADS (distributed across 3 events)"
echo "  - Buffer: ${BUFFER_MB}MB per event"
echo "  - Shards: $SHARDS per event"
echo "  - Total RPS: $TOTAL_RPS (Event1: $EVENT1_RPS, Event2: $EVENT2_RPS, Event3: $EVENT3_RPS)"
echo "  - Duration: $DURATION"
echo "  - Log size: 300KB per request"
echo ""

# Cleanup old logs before starting
cleanup_old_logs
echo ""

# Check if server binary exists
if [ ! -f "./bin/server" ]; then
    echo -e "${BLUE}Building server...${NC}"
    go build -o bin/server ./server
    echo -e "${GREEN}✓ Server built${NC}"
else
    echo -e "${GREEN}✓ Server binary found${NC}"
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

# Run 3 parallel ghz instances for different events
echo -e "${BLUE}Running load test with 3 events:${NC}"
echo -e "  ${CYAN}Event1:${NC} $EVENT1_RPS RPS"
echo -e "  ${CYAN}Event2:${NC} $EVENT2_RPS RPS"
echo -e "  ${CYAN}Event3:${NC} $EVENT3_RPS RPS"
echo ""

# Calculate concurrency per event (distribute threads proportionally)
EVENT1_THREADS=$((THREADS * EVENT1_RPS / TOTAL_RPS))
EVENT2_THREADS=$((THREADS * EVENT2_RPS / TOTAL_RPS))
EVENT3_THREADS=$((THREADS - EVENT1_THREADS - EVENT2_THREADS))

# Run Event1 load test in background
echo -e "${BLUE}Starting Event1 load test ($EVENT1_RPS RPS, $EVENT1_THREADS threads)...${NC}"
ghz --insecure \
    --proto proto/random_numbers.proto \
    --call randomnumbers.RandomNumberService.GetRandomNumbers \
    --data-file test_data/event1.json \
    --rps $EVENT1_RPS \
    --duration $DURATION \
    --concurrency $EVENT1_THREADS \
    --format json \
    --output "$RESULTS_DIR/ghz_event1_${TIMESTAMP}.json" \
    localhost:$SERVER_PORT > /dev/null 2>&1 &
GHZ_EVENT1_PID=$!

# Run Event2 load test in background
echo -e "${BLUE}Starting Event2 load test ($EVENT2_RPS RPS, $EVENT2_THREADS threads)...${NC}"
ghz --insecure \
    --proto proto/random_numbers.proto \
    --call randomnumbers.RandomNumberService.GetRandomNumbers \
    --data-file test_data/event2.json \
    --rps $EVENT2_RPS \
    --duration $DURATION \
    --concurrency $EVENT2_THREADS \
    --format json \
    --output "$RESULTS_DIR/ghz_event2_${TIMESTAMP}.json" \
    localhost:$SERVER_PORT > /dev/null 2>&1 &
GHZ_EVENT2_PID=$!

# Run Event3 load test in background
echo -e "${BLUE}Starting Event3 load test ($EVENT3_RPS RPS, $EVENT3_THREADS threads)...${NC}"
ghz --insecure \
    --proto proto/random_numbers.proto \
    --call randomnumbers.RandomNumberService.GetRandomNumbers \
    --data-file test_data/event3.json \
    --rps $EVENT3_RPS \
    --duration $DURATION \
    --concurrency $EVENT3_THREADS \
    --format json \
    --output "$RESULTS_DIR/ghz_event3_${TIMESTAMP}.json" \
    localhost:$SERVER_PORT > /dev/null 2>&1 &
GHZ_EVENT3_PID=$!

echo -e "${GREEN}✓ All load tests started${NC}"
echo -e "${YELLOW}Waiting for tests to complete ($DURATION)...${NC}"

# Wait for all ghz instances to complete
wait $GHZ_EVENT1_PID
EVENT1_EXIT=$?
wait $GHZ_EVENT2_PID
EVENT2_EXIT=$?
wait $GHZ_EVENT3_PID
EVENT3_EXIT=$?

if [ $EVENT1_EXIT -eq 0 ] && [ $EVENT2_EXIT -eq 0 ] && [ $EVENT3_EXIT -eq 0 ]; then
    echo -e "${GREEN}✓ All load tests completed!${NC}"
else
    echo -e "${RED}⚠ Some load tests failed (Event1: $EVENT1_EXIT, Event2: $EVENT2_EXIT, Event3: $EVENT3_EXIT)${NC}"
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

# Clean up event logs before stopping server
cleanup_test_logs
echo ""

# Stop server (cleanup function will handle this, but explicit stop for clarity)
if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    echo -e "${YELLOW}Stopping server...${NC}"
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
fi

echo -e "${GREEN}✓ Event-based baseline test complete!${NC}"
echo ""

# Extract key metrics from server logs
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}                     Quick Results                          ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"

LAST_METRICS=$(grep "METRICS:" "$RESULTS_DIR/server_${TIMESTAMP}.log" | tail -1)
if [ -n "$LAST_METRICS" ]; then
    echo "$LAST_METRICS" | sed 's/.*METRICS: //'
fi

LAST_EVENT_STATS=$(grep "EVENT_STATS:" "$RESULTS_DIR/server_${TIMESTAMP}.log" | tail -1)
if [ -n "$LAST_EVENT_STATS" ]; then
    echo "$LAST_EVENT_STATS" | sed 's/.*EVENT_STATS: //'
fi

echo ""
echo -e "${BLUE}Results saved to: $RESULTS_DIR/${NC}"
echo -e "${BLUE}  - Server logs:        server_${TIMESTAMP}.log${NC}"
echo -e "${BLUE}  - Event1 ghz report:  ghz_event1_${TIMESTAMP}.json${NC}"
echo -e "${BLUE}  - Event2 ghz report:  ghz_event2_${TIMESTAMP}.json${NC}"
echo -e "${BLUE}  - Event3 ghz report:  ghz_event3_${TIMESTAMP}.json${NC}"
echo -e "${BLUE}  - Resource data:      resource_timeline_${TIMESTAMP}.csv${NC}"
echo ""

# Aggregate ghz results if jq is available
if command -v jq &> /dev/null; then
    echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
    echo -e "${CYAN}                  Per-Event gRPC Results                   ${NC}"
    echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
    
    for event_num in 1 2 3; do
        report_file="$RESULTS_DIR/ghz_event${event_num}_${TIMESTAMP}.json"
        if [ -f "$report_file" ]; then
            echo ""
            echo -e "${CYAN}Event${event_num}:${NC}"
            jq -r '
                "  RPS: " + (.rps | tostring) + 
                " | Total Requests: " + (.count | tostring) + 
                " | Avg Latency: " + ((.average / 1000000) | tostring) + "ms" +
                " | P99 Latency: " + ((.details[0].latencyDistribution[] | select(.percentile == 99) | .latency / 1000000) | tostring) + "ms"
            ' "$report_file" 2>/dev/null || echo "  (Unable to parse results)"
        fi
    done
fi

echo ""
echo -e "${GREEN}Run completed at: $(date)${NC}"







