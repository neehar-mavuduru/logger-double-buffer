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
echo -e "${CYAN}       Sustained Performance Test (5 minutes)               ${NC}"
echo -e "${CYAN}       Analysis Window: First 4.5 minutes (270 seconds)     ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/sustained_tests"
CONTAINER_NAME="grpc-server-sustained-test"
SERVER_PORT=8585
RPS=1000
DURATION="5m"
FLUSH_INTERVAL="10s"

# Test scenarios
declare -a SCENARIOS=(
    "baseline:100:128:8"
    "winner_50t:50:64:4"
)

# Create results directory
mkdir -p "$RESULTS_DIR"

echo "Run started at: $(date)"
echo "Results directory: $RESULTS_DIR"
echo "Test configuration:"
echo "  - RPS: $RPS"
echo "  - Duration: $DURATION (analyze first 4.5 min)"
echo "  - Scenarios: ${#SCENARIOS[@]}"
echo ""

# Build Docker image
echo -e "${BLUE}Building Docker image...${NC}"
docker build -t grpc-server-test:latest -f docker/Dockerfile.server . > /dev/null 2>&1
echo -e "${GREEN}✓ Docker image built${NC}"
echo ""

# Run each scenario
SCENARIO_NUM=0
for SCENARIO_DEF in "${SCENARIOS[@]}"; do
    SCENARIO_NUM=$((SCENARIO_NUM + 1))
    
    IFS=':' read -r SCENARIO_NAME THREADS BUFFER_MB SHARDS <<< "$SCENARIO_DEF"
    
    echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}Scenario ${SCENARIO_NUM}/${#SCENARIOS[@]}: ${SCENARIO_NAME}${NC}"
    echo -e "${YELLOW}  Threads: $THREADS | Buffer: ${BUFFER_MB}MB | Shards: $SHARDS${NC}"
    echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
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
        echo "timestamp,cpu_percent,mem_usage_mb,mem_percent"
        while docker ps | grep -q $CONTAINER_NAME; do
            STATS=$(docker stats --no-stream --format "{{.CPUPerc}},{{.MemUsage}}" $CONTAINER_NAME 2>/dev/null || echo "0%,0B / 0B")
            CPU=$(echo $STATS | cut -d',' -f1 | sed 's/%//')
            MEM_RAW=$(echo $STATS | cut -d',' -f2)
            MEM_USAGE=$(echo $MEM_RAW | awk '{print $1}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "0")
            MEM_LIMIT=$(echo $MEM_RAW | awk '{print $3}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "1")
            MEM_PCT=$(awk "BEGIN {printf \"%.2f\", ($MEM_USAGE/$MEM_LIMIT)*100}")
            
            TIMESTAMP_NOW=$(date +%s)
            echo "$TIMESTAMP_NOW,$CPU,$MEM_USAGE,$MEM_PCT"
            sleep 1
        done
    } > "$RESULTS_DIR/${SCENARIO_NAME}_resource_${TIMESTAMP}.csv" 2>/dev/null &
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
        --output "$RESULTS_DIR/${SCENARIO_NAME}_ghz_${TIMESTAMP}.json" \
        localhost:$SERVER_PORT
    
    echo -e "${GREEN}✓ Load test completed!${NC}"
    
    # Wait a moment for final metrics
    sleep 2
    
    # Collect server logs
    echo -e "${BLUE}Collecting server logs...${NC}"
    docker logs $CONTAINER_NAME > "$RESULTS_DIR/${SCENARIO_NAME}_server_${TIMESTAMP}.log" 2>&1
    
    # Stop monitoring
    kill $MONITOR_PID 2>/dev/null || true
    wait $MONITOR_PID 2>/dev/null || true
    
    # Stop server
    echo -e "${YELLOW}Stopping server...${NC}"
    docker stop $CONTAINER_NAME > /dev/null 2>&1
    docker rm $CONTAINER_NAME > /dev/null 2>&1
    
    echo -e "${GREEN}✓ Scenario ${SCENARIO_NAME} complete!${NC}"
    echo ""
    
    # Extract key metrics (first 4.5 minutes only)
    echo -e "${CYAN}Quick Results (First 4.5 minutes):${NC}"
    
    # Get metrics at 270 second mark (or closest before)
    METRICS_270=$(grep "METRICS:" "$RESULTS_DIR/${SCENARIO_NAME}_server_${TIMESTAMP}.log" | \
        awk 'NR<=28' | tail -1)  # First ~27-28 metrics = ~270 seconds
    
    if [ -n "$METRICS_270" ]; then
        echo "$METRICS_270" | sed 's/.*METRICS: /  /'
    else
        echo "  No metrics found at 270s mark"
    fi
    
    echo ""
    sleep 2
done

echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}       ✓✓✓ ALL SUSTAINED TESTS COMPLETE! ✓✓✓            ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${GREEN}Total runtime: $(date)${NC}"
echo -e "${GREEN}Results directory: $RESULTS_DIR${NC}"
echo ""
echo -e "${YELLOW}Next step: Analyze results${NC}"
echo -e "${BLUE}  go run scripts/process_sustained_tests.go${NC}"
echo ""

