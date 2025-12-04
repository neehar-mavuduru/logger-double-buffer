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
echo -e "${CYAN}       Baseline Configuration Test - 100T, 64MB, 8S        ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/baseline_retest"
CONTAINER_NAME="grpc-server-baseline-test"
SERVER_PORT=8585
RPS=1000
DURATION="10m"
THREADS=100
BUFFER_MB=64
SHARDS=8
FLUSH_INTERVAL="10s"

# Create results directory
mkdir -p "$RESULTS_DIR"

echo "Run started at: $(date)"
echo "Results directory: $RESULTS_DIR"
echo "Test configuration:"
echo "  - Threads: $THREADS"
echo "  - Buffer: ${BUFFER_MB}MB"
echo "  - Shards: $SHARDS"
echo "  - RPS: $RPS"
echo "  - Duration: $DURATION"
echo ""

# Build Docker image
echo -e "${BLUE}Building Docker image...${NC}"
docker build -t grpc-server-test:latest -f docker/Dockerfile.server . > /dev/null 2>&1
echo -e "${GREEN}✓ Docker image built${NC}"
echo ""

# Cleanup any existing container
docker rm -f $CONTAINER_NAME 2>/dev/null || true

# Start server
echo -e "${BLUE}Starting server...${NC}"
docker run -d \
    --name $CONTAINER_NAME \
    --cpus=4 \
    --memory=4g \
    -p $SERVER_PORT:$SERVER_PORT \
    -e BUFFER_SIZE=$((BUFFER_MB * 1024 * 1024)) \
    -e NUM_SHARDS=$SHARDS \
    -e FLUSH_INTERVAL=$FLUSH_INTERVAL \
    grpc-server-test:latest \
    > /dev/null 2>&1

# Wait for server to be ready
echo -e "${BLUE}Waiting for server to be ready...${NC}"
for i in {1..30}; do
    if docker logs $CONTAINER_NAME 2>&1 | grep -q "Listening on"; then
        echo -e "${GREEN}✓ Server ready!${NC}"
        break
    fi
    sleep 1
done

# Start resource monitoring in background
echo -e "${BLUE}Starting resource monitoring...${NC}"
{
    echo "timestamp,cpu_percent,mem_usage_mb,mem_percent,net_input_mb,net_output_mb,block_input_mb,block_output_mb"
    while docker ps | grep -q $CONTAINER_NAME; do
        STATS=$(docker stats --no-stream --format "{{.CPUPerc}},{{.MemUsage}}" $CONTAINER_NAME 2>/dev/null || echo "0%,0B / 0B")
        CPU=$(echo $STATS | cut -d',' -f1 | sed 's/%//')
        MEM_RAW=$(echo $STATS | cut -d',' -f2)
        MEM_USAGE=$(echo $MEM_RAW | awk '{print $1}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "0")
        MEM_LIMIT=$(echo $MEM_RAW | awk '{print $3}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "1")
        MEM_PCT=$(awk "BEGIN {printf \"%.2f\", ($MEM_USAGE/$MEM_LIMIT)*100}")
        
        TIMESTAMP=$(date +%s)
        echo "$TIMESTAMP,$CPU,$MEM_USAGE,$MEM_PCT,0,0,0,0"
        sleep 1
    done
} > "$RESULTS_DIR/resource_timeline_${TIMESTAMP}.csv" 2>/dev/null &
MONITOR_PID=$!

# Run load test
echo -e "${BLUE}Running load test: $RPS RPS for $DURATION with $THREADS concurrent threads...${NC}"
ghz --insecure \
    --proto proto/random_numbers.proto \
    --call randomnumbers.RandomNumberService.GetRandomNumbers \
    --rps $RPS \
    --duration $DURATION \
    --concurrency $THREADS \
    --format json \
    --output "$RESULTS_DIR/ghz_report_${TIMESTAMP}.json" \
    localhost:$SERVER_PORT

echo -e "${GREEN}✓ Load test completed!${NC}"

# Wait a moment for final metrics
sleep 2

# Collect server logs
echo -e "${BLUE}Collecting server logs...${NC}"
docker logs $CONTAINER_NAME > "$RESULTS_DIR/server_${TIMESTAMP}.log" 2>&1

# Stop monitoring
kill $MONITOR_PID 2>/dev/null || true
wait $MONITOR_PID 2>/dev/null || true

# Stop server
echo -e "${YELLOW}Stopping server...${NC}"
docker stop $CONTAINER_NAME > /dev/null 2>&1
docker rm $CONTAINER_NAME > /dev/null 2>&1

echo -e "${GREEN}✓ Baseline test complete!${NC}"
echo ""

# Extract key metrics from server logs
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}                     Quick Results                          ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"

LAST_METRICS=$(grep "METRICS:" "$RESULTS_DIR/server_${TIMESTAMP}.log" | tail -1)
if [ -n "$LAST_METRICS" ]; then
    echo "$LAST_METRICS" | sed 's/.*METRICS: //'
else
    echo "No metrics found in server logs"
fi

echo ""
echo -e "${BLUE}Results saved to: $RESULTS_DIR/${NC}"
echo -e "${BLUE}  - Server logs:     server_${TIMESTAMP}.log${NC}"
echo -e "${BLUE}  - ghz report:      ghz_report_${TIMESTAMP}.json${NC}"
echo -e "${BLUE}  - Resource data:   resource_timeline_${TIMESTAMP}.csv${NC}"
echo ""
echo -e "${GREEN}Run completed at: $(date)${NC}"

