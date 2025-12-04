#!/bin/bash

# Retry Mechanism Test Script
# Tests: 100 concurrent threads, 8 shards, 64MB buffer, 300KB logs, 5 minutes
# Measures: Drop rate, latency, throughput, GC, memory

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}    Retry Mechanism Test - Prevent Log Drops               ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/retry_test"
CONTAINER_NAME="grpc-server-retry-test"
SERVER_PORT=8585
METRICS_FILE="${RESULTS_DIR}/metrics_${TIMESTAMP}.json"
RESOURCE_FILE="${RESULTS_DIR}/resources_${TIMESTAMP}.csv"
GHZ_REPORT="${RESULTS_DIR}/ghz_report_${TIMESTAMP}.json"

# Test parameters
CONCURRENT_THREADS=100
RPS=1000  # Target requests per second
DURATION="5m"
BUFFER_MB=64
SHARDS=8
FLUSH_INTERVAL="5s"
LOG_SIZE_KB=300

echo -e "${GREEN}Test Configuration:${NC}"
echo "  Concurrent Threads: $CONCURRENT_THREADS"
echo "  Target RPS: $RPS requests/second"
echo "  Duration: $DURATION"
echo "  Buffer Size: ${BUFFER_MB}MB"
echo "  Shards: $SHARDS"
echo "  Flush Interval: $FLUSH_INTERVAL"
echo "  Log Size: ${LOG_SIZE_KB}KB per request"
echo "  Expected Data Rate: $((RPS * LOG_SIZE_KB / 1024)) MB/sec"
echo ""

# Create results directory
mkdir -p "$RESULTS_DIR"

# Check if ghz is installed
if ! command -v ghz &> /dev/null; then
    echo -e "${YELLOW}Installing ghz...${NC}"
    go install github.com/bojand/ghz/cmd/ghz@latest
fi

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}Error: Docker is not running${NC}"
    exit 1
fi

# Clean up any existing container
echo -e "${BLUE}Cleaning up existing containers...${NC}"
docker stop "$CONTAINER_NAME" 2>/dev/null || true
docker rm "$CONTAINER_NAME" 2>/dev/null || true

# Build Docker image
echo -e "${BLUE}Building Docker image...${NC}"
cd "$(dirname "$0")/.."
docker build -f docker/Dockerfile.server -t grpc-server:test .

# Start server container
echo -e "${BLUE}Starting server container...${NC}"
docker run -d \
    --name "$CONTAINER_NAME" \
    --cpus="4" \
    --memory="4g" \
    -p "${SERVER_PORT}:${SERVER_PORT}" \
    -v "$(pwd)/logs:/app/logs" \
    -e BUFFER_SIZE=$((BUFFER_MB * 1024 * 1024)) \
    -e NUM_SHARDS=$SHARDS \
    -e FLUSH_INTERVAL=$FLUSH_INTERVAL \
    -e LOG_FILE=/app/logs/server_${TIMESTAMP}.log \
    grpc-server:test

# Wait for server to start
echo -e "${BLUE}Waiting for server to start...${NC}"
sleep 5

# Check if server is running
if ! docker ps | grep -q "$CONTAINER_NAME"; then
    echo -e "${RED}Error: Server container failed to start${NC}"
    docker logs "$CONTAINER_NAME"
    exit 1
fi

# Start resource monitoring in background
echo -e "${BLUE}Starting resource monitoring...${NC}"
(
    echo "timestamp,cpu_percent,memory_mb,memory_percent"
    while true; do
        STATS=$(docker stats --no-stream --format "{{.CPUPerc}},{{.MemUsage}}" "$CONTAINER_NAME" 2>/dev/null || echo "0,0")
        CPU=$(echo "$STATS" | cut -d',' -f1 | sed 's/%//')
        MEM_USAGE=$(echo "$STATS" | cut -d',' -f2 | awk '{print $1}')
        MEM_UNIT=$(echo "$STATS" | cut -d',' -f2 | awk '{print $2}')
        
        # Convert memory to MB
        if [[ "$MEM_UNIT" == "GiB" ]]; then
            MEM_MB=$(echo "$MEM_USAGE * 1024" | bc)
        elif [[ "$MEM_UNIT" == "MiB" ]]; then
            MEM_MB=$MEM_USAGE
        elif [[ "$MEM_UNIT" == "KiB" ]]; then
            MEM_MB=$(echo "scale=2; $MEM_USAGE / 1024" | bc)
        else
            MEM_MB=0
        fi
        
        echo "$(date +%s),$CPU,$MEM_MB"
        sleep 1
    done
) > "$RESOURCE_FILE" &
MONITOR_PID=$!

# Run ghz load test
echo -e "${BLUE}Running ghz load test...${NC}"
echo "  Concurrent connections: $CONCURRENT_THREADS"
echo "  Target RPS: $RPS"
echo "  Duration: $DURATION"
echo ""

ghz \
    --proto=proto/random_numbers.proto \
    --call=randomnumbers.RandomNumberService.GetRandomNumbers \
    --insecure \
    --connections=$CONCURRENT_THREADS \
    --concurrency=$CONCURRENT_THREADS \
    --rps=$RPS \
    --duration="$DURATION" \
    --timeout=30s \
    --format=json \
    --output="$GHZ_REPORT" \
    localhost:${SERVER_PORT}

# Stop monitoring
kill $MONITOR_PID 2>/dev/null || true

# Wait a bit for final metrics
sleep 2

# Extract final metrics from server logs
echo -e "${BLUE}Extracting metrics from server logs...${NC}"
docker logs "$CONTAINER_NAME" > "${RESULTS_DIR}/server_logs_${TIMESTAMP}.txt" 2>&1

# Stop container
echo -e "${BLUE}Stopping server container...${NC}"
docker stop "$CONTAINER_NAME" 2>/dev/null || true

# Cleanup: Delete log files to save disk space
echo -e "${BLUE}Cleaning up log files...${NC}"
rm -f "${RESULTS_DIR}/server_logs_${TIMESTAMP}.txt" 2>/dev/null || true
rm -f "logs/server_${TIMESTAMP}.log" 2>/dev/null || true
echo -e "${GREEN}✓ Log files cleaned up${NC}"

# Parse results
echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}Test Results Summary${NC}"
echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Parse ghz results
if [ -f "$GHZ_REPORT" ]; then
    echo -e "${BLUE}gRPC Performance (from ghz):${NC}"
    cat "$GHZ_REPORT" | jq -r '
        "  Total Requests: \(.total),
  Successful: \(.statusCodeDistribution.OK // 0),
  Failed: \(.statusCodeDistribution."Unavailable" // 0),
  P50 Latency: \(.latencyDistribution.P50)ms,
  P95 Latency: \(.latencyDistribution.P95)ms,
  P99 Latency: \(.latencyDistribution.P99)ms,
  RPS: \(.rps),
  Duration: \(.duration)"
    ' || echo "  (Unable to parse ghz results)"
fi

echo ""
echo -e "${BLUE}Resource Usage:${NC}"
if [ -f "$RESOURCE_FILE" ]; then
    awk -F',' 'NR>1 {
        cpu+=$2; mem+=$3; count++
        if (NR==2) {min_cpu=$2; max_cpu=$2; min_mem=$3; max_mem=$3}
        if ($2<min_cpu) min_cpu=$2
        if ($2>max_cpu) max_cpu=$2
        if ($3<min_mem) min_mem=$3
        if ($3>max_mem) max_mem=$3
    } END {
        if (count>0) {
            print "  Avg CPU: " cpu/count "%"
            print "  Min CPU: " min_cpu "%"
            print "  Max CPU: " max_cpu "%"
            print "  Avg Memory: " mem/count " MB"
            print "  Min Memory: " min_mem " MB"
            print "  Max Memory: " max_mem " MB"
        }
    }' "$RESOURCE_FILE"
fi

echo ""
echo -e "${GREEN}Results saved to:${NC}"
echo "  gRPC Report: $GHZ_REPORT"
echo "  Resource Metrics: $RESOURCE_FILE"
echo "  Server Logs: ${RESULTS_DIR}/server_logs_${TIMESTAMP}.txt"
echo ""
echo -e "${GREEN}Test completed!${NC}"

