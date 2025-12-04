#!/bin/bash

# Production Load Test Script
# Uses ghz to generate realistic gRPC load (1000 RPS, 300KB logs per request)
# Monitors CPU, memory, GC, and application metrics

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}    Production Load Test - gRPC Server + AsyncLogger       ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/production_load"
CONTAINER_NAME="grpc-server-loadtest"
SERVER_PORT=8585
METRICS_FILE="${RESULTS_DIR}/metrics_${TIMESTAMP}.json"
RESOURCE_FILE="${RESULTS_DIR}/resources_${TIMESTAMP}.csv"
GHZ_REPORT="${RESULTS_DIR}/ghz_report_${TIMESTAMP}.json"

# Parse arguments
SCENARIO=${1:-baseline}

# Parse scenario config based on name
case "$SCENARIO" in
    baseline)
        RPS=1000
        DURATION="2m"
        BUFFER_MB=256
        SHARDS=8
        FLUSH_INTERVAL="5s"
        ;;
    high_buffer)
        RPS=1000
        DURATION="2m"
        BUFFER_MB=512
        SHARDS=8
        FLUSH_INTERVAL="5s"
        ;;
    more_shards)
        RPS=1000
        DURATION="2m"
        BUFFER_MB=256
        SHARDS=16
        FLUSH_INTERVAL="5s"
        ;;
    peak_load)
        RPS=1500
        DURATION="2m"
        BUFFER_MB=512
        SHARDS=16
        FLUSH_INTERVAL="5s"
        ;;
    low_load)
        RPS=500
        DURATION="2m"
        BUFFER_MB=128
        SHARDS=4
        FLUSH_INTERVAL="5s"
        ;;
    *)
        echo -e "${RED}Error: Unknown scenario '$SCENARIO'${NC}"
        echo "Available scenarios: baseline, high_buffer, more_shards, peak_load, low_load"
        exit 1
        ;;
esac

echo -e "${GREEN}Test Configuration:${NC}"
echo "  Scenario: $SCENARIO"
echo "  RPS: $RPS requests/second"
echo "  Duration: $DURATION"
echo "  Buffer Size: ${BUFFER_MB}MB"
echo "  Shards: $SHARDS"
echo "  Flush Interval: $FLUSH_INTERVAL"
echo "  Expected Data Rate: $((RPS * 300 / 1024)) MB/sec"
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

# Stop any existing server container
if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo -e "${YELLOW}Stopping existing container...${NC}"
    docker stop "$CONTAINER_NAME" > /dev/null 2>&1 || true
    docker rm "$CONTAINER_NAME" > /dev/null 2>&1 || true
fi

echo -e "${BLUE}Building server container...${NC}"
docker build -t grpc-server-loadtest \
    --build-arg BUFFER_SIZE=$((BUFFER_MB * 1024 * 1024)) \
    --build-arg NUM_SHARDS=$SHARDS \
    --build-arg FLUSH_INTERVAL=$FLUSH_INTERVAL \
    -f docker/Dockerfile.server .

echo -e "${BLUE}Starting gRPC server...${NC}"
docker run -d \
    --name "$CONTAINER_NAME" \
    --cpus="4" \
    --memory="4g" \
    -p ${SERVER_PORT}:8585 \
    -e BUFFER_SIZE=$((BUFFER_MB * 1024 * 1024)) \
    -e NUM_SHARDS=$SHARDS \
    -e FLUSH_INTERVAL=$FLUSH_INTERVAL \
    grpc-server-loadtest

# Wait for server to be ready
echo -e "${YELLOW}Waiting for server to be ready...${NC}"
sleep 5

# Verify server is running
if ! docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo -e "${RED}Error: Container failed to start${NC}"
    docker logs "$CONTAINER_NAME"
    exit 1
fi

echo -e "${GREEN}✓ Server is ready${NC}"
echo ""

# Start resource monitoring in background
echo -e "${BLUE}Starting resource monitoring...${NC}"
(
    echo "timestamp,cpu_percent,memory_mb,memory_percent,net_rx_mb,net_tx_mb,block_read_mb,block_write_mb"
    START_TIME=$(date +%s)
    while docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; do
        STATS=$(docker stats "$CONTAINER_NAME" --no-stream --format "{{.CPUPerc}},{{.MemUsage}},{{.MemPerc}},{{.NetIO}},{{.BlockIO}}")
        
        # Parse stats
        CPU=$(echo "$STATS" | cut -d',' -f1 | sed 's/%//')
        MEM_USAGE=$(echo "$STATS" | cut -d',' -f2 | awk '{print $1}' | sed 's/MiB//' | sed 's/GiB/*1024/' | bc 2>/dev/null || echo "0")
        MEM_PERCENT=$(echo "$STATS" | cut -d',' -f3 | sed 's/%//')
        NET=$(echo "$STATS" | cut -d',' -f4)
        NET_RX=$(echo "$NET" | awk '{print $1}' | sed 's/MB//' | sed 's/kB/*0.001/' | sed 's/GB/*1000/' | bc 2>/dev/null || echo "0")
        NET_TX=$(echo "$NET" | awk '{print $4}' | sed 's/MB//' | sed 's/kB/*0.001/' | sed 's/GB/*1000/' | bc 2>/dev/null || echo "0")
        BLOCK=$(echo "$STATS" | cut -d',' -f5)
        BLOCK_READ=$(echo "$BLOCK" | awk '{print $1}' | sed 's/MB//' | sed 's/kB/*0.001/' | sed 's/GB/*1000/' | bc 2>/dev/null || echo "0")
        BLOCK_WRITE=$(echo "$BLOCK" | awk '{print $4}' | sed 's/MB//' | sed 's/kB/*0.001/' | sed 's/GB/*1000/' | bc 2>/dev/null || echo "0")
        
        CURRENT_TIME=$(date +%s)
        ELAPSED=$((CURRENT_TIME - START_TIME))
        
        echo "${ELAPSED},${CPU},${MEM_USAGE},${MEM_PERCENT},${NET_RX},${NET_TX},${BLOCK_READ},${BLOCK_WRITE}"
        
        sleep 1
    done
) > "$RESOURCE_FILE" &
MONITOR_PID=$!

echo -e "${GREEN}✓ Resource monitoring started (PID: $MONITOR_PID)${NC}"
echo ""

# Run load test with ghz
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  Running Load Test ($RPS RPS for $DURATION)                ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

ghz --insecure \
    --proto proto/random_numbers.proto \
    --call randomnumbers.RandomNumberService.GetRandomNumbers \
    --rps $RPS \
    --duration $DURATION \
    --connections 50 \
    --concurrency 100 \
    --format json \
    --output "$GHZ_REPORT" \
    localhost:${SERVER_PORT}

echo ""
echo -e "${GREEN}✓ Load test completed!${NC}"
echo ""

# Collect server logs and metrics
echo -e "${BLUE}Collecting server metrics...${NC}"
docker logs "$CONTAINER_NAME" > "${RESULTS_DIR}/server_logs_${TIMESTAMP}.log" 2>&1

# Stop resource monitoring
kill $MONITOR_PID 2>/dev/null || true
wait $MONITOR_PID 2>/dev/null || true

# Stop server
echo -e "${YELLOW}Stopping server...${NC}"
docker stop "$CONTAINER_NAME" > /dev/null 2>&1

# Collect final stats
docker stats "$CONTAINER_NAME" --no-stream > "${RESULTS_DIR}/final_stats_${TIMESTAMP}.txt" 2>&1 || true

# Remove container
docker rm "$CONTAINER_NAME" > /dev/null 2>&1

echo -e "${GREEN}✓ Server stopped${NC}"
echo ""

# Process results
echo -e "${BLUE}Processing results...${NC}"
go run scripts/process_production_results.go \
    --ghz-report "$GHZ_REPORT" \
    --resource-file "$RESOURCE_FILE" \
    --server-logs "${RESULTS_DIR}/server_logs_${TIMESTAMP}.log" \
    --output "${RESULTS_DIR}/analysis_${TIMESTAMP}.md" \
    --scenario "$SCENARIO"

echo ""
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}✓ Production Load Test Complete!${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${GREEN}Results:${NC}"
echo "  GHZ Report:      $GHZ_REPORT"
echo "  Resource Data:   $RESOURCE_FILE"
echo "  Server Logs:     ${RESULTS_DIR}/server_logs_${TIMESTAMP}.log"
echo "  Analysis:        ${RESULTS_DIR}/analysis_${TIMESTAMP}.md"
echo ""
echo -e "${YELLOW}View analysis:${NC}"
echo "  cat ${RESULTS_DIR}/analysis_${TIMESTAMP}.md"
echo ""

