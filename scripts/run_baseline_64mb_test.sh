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
echo -e "${CYAN}    Baseline Performance Test - 64MB Buffer, 8 Shards      ${NC}"
echo -e "${CYAN}    100 Concurrent Threads, 1000 RPS, 300KB Logs, 10min  ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/baseline_64mb_8s_100t_10min"
CONTAINER_NAME="grpc-server-baseline-64mb"
SERVER_PORT=8585
RPS=1000
DURATION="10m"
THREADS=100
BUFFER_MB=64
SHARDS=8
FLUSH_INTERVAL="10s"
LOG_SIZE_KB=300

# Create results directory
mkdir -p "$RESULTS_DIR"

echo "Run started at: $(date)"
echo "Results directory: $RESULTS_DIR"
echo "Test configuration:"
echo "  - Buffer Size: ${BUFFER_MB}MB"
echo "  - Shards: $SHARDS"
echo "  - Concurrent Threads: $THREADS"
echo "  - Target RPS: $RPS"
echo "  - Duration: $DURATION"
echo "  - Log Size per Request: ${LOG_SIZE_KB}KB"
echo "  - Expected Data Rate: $((RPS * LOG_SIZE_KB / 1024)) MB/sec"
echo ""

# Check if ghz is installed
if ! command -v ghz &> /dev/null; then
    echo -e "${RED}Error: ghz is not installed${NC}"
    echo "Install with: go install github.com/bojand/ghz/cmd/ghz@latest"
    exit 1
fi

# Build Docker image
echo -e "${BLUE}Building Docker image...${NC}"
docker build -t grpc-server-test:latest -f docker/Dockerfile.server . > /dev/null 2>&1
echo -e "${GREEN}✓ Docker image built${NC}"
echo ""

# Cleanup any existing container
docker rm -f $CONTAINER_NAME 2>/dev/null || true

# Start server
echo -e "${BLUE}Starting server container...${NC}"
docker run -d \
    --name $CONTAINER_NAME \
    --cpus=4 \
    --memory=8g \
    -p $SERVER_PORT:$SERVER_PORT \
    -e BUFFER_SIZE=$((BUFFER_MB * 1024 * 1024)) \
    -e NUM_SHARDS=$SHARDS \
    -e FLUSH_INTERVAL=$FLUSH_INTERVAL \
    -e LOG_FILE=/app/logs/server.log \
    grpc-server-test:latest \
    > /dev/null 2>&1

# Wait for server to be ready
echo -e "${BLUE}Waiting for server to be ready...${NC}"
for i in {1..30}; do
    if docker logs $CONTAINER_NAME 2>&1 | grep -q "Server listening"; then
        echo -e "${GREEN}✓ Server ready!${NC}"
        break
    fi
    if [ $i -eq 30 ]; then
        echo -e "${RED}✗ Server failed to start${NC}"
        docker logs $CONTAINER_NAME
        exit 1
    fi
    sleep 1
done

# Start resource monitoring in background
echo -e "${BLUE}Starting resource monitoring...${NC}"
RESOURCE_FILE="$RESULTS_DIR/resource_timeline_${TIMESTAMP}.csv"
{
    echo "timestamp,cpu_percent,mem_usage_mb,mem_percent,net_input_mb,net_output_mb,block_input_mb,block_output_mb"
    while docker ps | grep -q $CONTAINER_NAME; do
        STATS=$(docker stats --no-stream --format "{{.CPUPerc}},{{.MemUsage}},{{.NetIO}},{{.BlockIO}}" $CONTAINER_NAME 2>/dev/null || echo "0%,0B / 0B,0B / 0B,0B / 0B")
        
        CPU=$(echo $STATS | cut -d',' -f1 | sed 's/%//')
        MEM_RAW=$(echo $STATS | cut -d',' -f2)
        NET_RAW=$(echo $STATS | cut -d',' -f3)
        BLOCK_RAW=$(echo $STATS | cut -d',' -f4)
        
        # Parse memory
        MEM_USAGE=$(echo $MEM_RAW | awk '{print $1}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "0")
        MEM_LIMIT=$(echo $MEM_RAW | awk '{print $3}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "1")
        MEM_PCT=$(awk "BEGIN {printf \"%.2f\", ($MEM_USAGE/$MEM_LIMIT)*100}")
        
        # Parse network I/O
        NET_RX=$(echo $NET_RAW | awk '{print $1}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "0")
        NET_TX=$(echo $NET_RAW | awk '{print $3}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "0")
        
        # Parse block I/O
        BLOCK_READ=$(echo $BLOCK_RAW | awk '{print $1}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "0")
        BLOCK_WRITE=$(echo $BLOCK_RAW | awk '{print $3}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "0")
        
        TIMESTAMP_NOW=$(date +%s)
        echo "$TIMESTAMP_NOW,$CPU,$MEM_USAGE,$MEM_PCT,$NET_RX,$NET_TX,$BLOCK_READ,$BLOCK_WRITE"
        sleep 1
    done
} > "$RESOURCE_FILE" 2>/dev/null &
MONITOR_PID=$!

# Run load test
echo -e "${BLUE}Running load test: $RPS RPS for $DURATION with $THREADS concurrent threads...${NC}"
GHZ_REPORT="$RESULTS_DIR/ghz_report_${TIMESTAMP}.json"
ghz --insecure \
    --proto proto/random_numbers.proto \
    --call randomnumbers.RandomNumberService.GetRandomNumbers \
    --rps $RPS \
    --duration $DURATION \
    --concurrency $THREADS \
    --connections $THREADS \
    --format json \
    --output "$GHZ_REPORT" \
    localhost:$SERVER_PORT

echo -e "${GREEN}✓ Load test completed!${NC}"

# Wait a moment for final metrics
sleep 3

# Collect server logs
echo -e "${BLUE}Collecting server logs...${NC}"
SERVER_LOG="$RESULTS_DIR/server_logs_${TIMESTAMP}.log"
docker logs $CONTAINER_NAME > "$SERVER_LOG" 2>&1

# Stop monitoring
kill $MONITOR_PID 2>/dev/null || true
wait $MONITOR_PID 2>/dev/null || true

# Extract final metrics from server logs
echo -e "${BLUE}Extracting final metrics...${NC}"
FINAL_METRICS=$(grep "Logger Stats" "$SERVER_LOG" | tail -1 || echo "")
LAST_METRICS=$(grep "METRICS:" "$SERVER_LOG" | tail -1 || echo "")
LAST_FLUSH=$(grep "FLUSH_METRICS:" "$SERVER_LOG" | tail -1 || echo "")
LAST_IO=$(grep "IO_BREAKDOWN:" "$SERVER_LOG" | tail -1 || echo "")

# Stop server
echo -e "${YELLOW}Stopping server...${NC}"
docker stop $CONTAINER_NAME > /dev/null 2>&1

# Clean up logs from container to save storage
echo -e "${BLUE}Cleaning up log files...${NC}"
docker exec $CONTAINER_NAME rm -f /app/logs/server.log 2>/dev/null || true
docker rm $CONTAINER_NAME > /dev/null 2>&1

# Clean up /tmp log files
echo -e "${BLUE}Cleaning up /tmp log files...${NC}"
rm -f /tmp/*.log /tmp/logbytes_*.log /tmp/server_*.log 2>/dev/null || true
echo -e "${GREEN}✓ /tmp log files cleaned${NC}"

echo -e "${GREEN}✓ Baseline test complete!${NC}"
echo ""

# Process results
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}                     Test Results                          ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

if [ -n "$LAST_METRICS" ]; then
    echo -e "${GREEN}Logger Metrics:${NC}"
    echo "$LAST_METRICS" | sed 's/.*METRICS: //'
    echo ""
fi

if [ -n "$LAST_FLUSH" ]; then
    echo -e "${GREEN}Flush Metrics:${NC}"
    echo "$LAST_FLUSH" | sed 's/.*FLUSH_METRICS: //'
    echo ""
fi

if [ -n "$LAST_IO" ]; then
    echo -e "${GREEN}I/O Breakdown:${NC}"
    echo "$LAST_IO" | sed 's/.*IO_BREAKDOWN: //'
    echo ""
fi

if [ -n "$FINAL_METRICS" ]; then
    echo -e "${GREEN}Final Logger Stats:${NC}"
    echo "$FINAL_METRICS" | sed 's/.*Logger Stats - //'
    echo ""
fi

# Process ghz results
if [ -f "$GHZ_REPORT" ]; then
    echo -e "${GREEN}gRPC Performance (ghz):${NC}"
    TOTAL=$(jq -r '.count' "$GHZ_REPORT" 2>/dev/null || echo "N/A")
    RPS_ACTUAL=$(jq -r '.rps' "$GHZ_REPORT" 2>/dev/null || echo "N/A")
    AVG_LATENCY=$(jq -r '.average' "$GHZ_REPORT" 2>/dev/null | sed 's/ms//' || echo "N/A")
    P50=$(jq -r '.latencyDistribution[] | select(.percentage == 50) | .latency' "$GHZ_REPORT" 2>/dev/null || echo "N/A")
    P95=$(jq -r '.latencyDistribution[] | select(.percentage == 95) | .latency' "$GHZ_REPORT" 2>/dev/null || echo "N/A")
    P99=$(jq -r '.latencyDistribution[] | select(.percentage == 99) | .latency' "$GHZ_REPORT" 2>/dev/null || echo "N/A")
    
    echo "  Total Requests: $TOTAL"
    echo "  Actual RPS: $RPS_ACTUAL"
    echo "  Average Latency: ${AVG_LATENCY}ms"
    echo "  P50 Latency: ${P50}ms"
    echo "  P95 Latency: ${P95}ms"
    echo "  P99 Latency: ${P99}ms"
    echo ""
fi

# Process resource data
if [ -f "$RESOURCE_FILE" ]; then
    echo -e "${GREEN}Resource Utilization:${NC}"
    AVG_CPU=$(tail -n +2 "$RESOURCE_FILE" | awk -F',' '{sum+=$2; count++} END {if(count>0) printf "%.2f", sum/count; else print "0"}')
    MAX_CPU=$(tail -n +2 "$RESOURCE_FILE" | awk -F',' '{if($2>max) max=$2} END {printf "%.2f", max}')
    AVG_MEM=$(tail -n +2 "$RESOURCE_FILE" | awk -F',' '{sum+=$3; count++} END {if(count>0) printf "%.2f", sum/count; else print "0"}')
    MAX_MEM=$(tail -n +2 "$RESOURCE_FILE" | awk -F',' '{if($3>max) max=$3} END {printf "%.2f", max}')
    
    echo "  Average CPU: ${AVG_CPU}%"
    echo "  Peak CPU: ${MAX_CPU}%"
    echo "  Average Memory: ${AVG_MEM}MB"
    echo "  Peak Memory: ${MAX_MEM}MB"
    echo ""
fi

echo -e "${BLUE}Results saved to: $RESULTS_DIR/${NC}"
echo -e "${BLUE}  - Server logs:     server_logs_${TIMESTAMP}.log${NC}"
echo -e "${BLUE}  - ghz report:      ghz_report_${TIMESTAMP}.json${NC}"
echo -e "${BLUE}  - Resource data:   resource_timeline_${TIMESTAMP}.csv${NC}"
echo ""
echo -e "${GREEN}Run completed at: $(date)${NC}"

# Generate comprehensive analysis report
echo -e "${BLUE}Generating analysis report...${NC}"
if command -v go &> /dev/null && [ -f "scripts/process_production_results.go" ]; then
    go run scripts/process_production_results.go \
        -ghz-report="$GHZ_REPORT" \
        -resource-file="$RESOURCE_FILE" \
        -server-logs="$SERVER_LOG" \
        -output="$RESULTS_DIR/analysis_${TIMESTAMP}.md" \
        -scenario="baseline_64mb_8s_100t" \
        2>/dev/null || echo "Note: Could not generate detailed analysis report"
fi

echo ""
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}Test complete! All metrics captured and logs cleaned up.${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"

