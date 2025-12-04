#!/bin/bash

# Baseline Load Test Script for AsyncLogger
# 
# This script runs a comprehensive load test of the AsyncLogger module using Docker.
# It tests the baseline configuration: 64MB buffer, 8 shards, 100 concurrent threads.
#
# Prerequisites:
#   - Docker installed and running
#   - ghz installed (go install github.com/bojand/ghz/cmd/ghz@latest)
#   - A gRPC server that uses AsyncLogger (see README.md for integration)
#   - Proto file for the gRPC service
#
# Usage:
#   ./run_baseline_load_test.sh [OPTIONS]
#
# Options:
#   --duration DURATION     Test duration (default: 10m)
#   --rps RPS               Target requests per second (default: 1000)
#   --threads THREADS       Concurrent threads (default: 100)
#   --buffer-mb MB          Buffer size in MB (default: 64)
#   --shards SHARDS         Number of shards (default: 8)
#   --flush-interval INTERVAL  Flush interval (default: 10s)
#   --server-image IMAGE    Docker image name (default: grpc-server-test:latest)
#   --proto-file PATH       Path to proto file (REQUIRED)
#   --grpc-call CALL        gRPC service call (REQUIRED, e.g., package.Service.Method)
#   --server-port PORT      Server port (default: 8585)
#   --help                  Show this help message

set -euo pipefail

# Colors for output
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly BLUE='\033[0;34m'
readonly YELLOW='\033[1;33m'
readonly CYAN='\033[0;36m'
readonly NC='\033[0m' # No Color

# Default configuration
DURATION="${DURATION:-10m}"
RPS="${RPS:-1000}"
THREADS="${THREADS:-100}"
BUFFER_MB="${BUFFER_MB:-64}"
SHARDS="${SHARDS:-8}"
FLUSH_INTERVAL="${FLUSH_INTERVAL:-10s}"
SERVER_IMAGE="${SERVER_IMAGE:-grpc-server-test:latest}"
PROTO_FILE="${PROTO_FILE:-}"
GRPC_CALL="${GRPC_CALL:-}"
SERVER_PORT="${SERVER_PORT:-8585}"
CONTAINER_NAME="asynclogger-baseline-test"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --duration)
            DURATION="$2"
            shift 2
            ;;
        --rps)
            RPS="$2"
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
        --flush-interval)
            FLUSH_INTERVAL="$2"
            shift 2
            ;;
        --server-image)
            SERVER_IMAGE="$2"
            shift 2
            ;;
        --proto-file)
            PROTO_FILE="$2"
            shift 2
            ;;
        --grpc-call)
            GRPC_CALL="$2"
            shift 2
            ;;
        --server-port)
            SERVER_PORT="$2"
            shift 2
            ;;
        --help)
            cat << EOF
Baseline Load Test Script for AsyncLogger

Usage: $0 [OPTIONS]

Options:
    --duration DURATION         Test duration (default: 10m)
    --rps RPS                   Target requests per second (default: 1000)
    --threads THREADS           Concurrent threads (default: 100)
    --buffer-mb MB              Buffer size in MB (default: 64)
    --shards SHARDS             Number of shards (default: 8)
    --flush-interval INTERVAL   Flush interval (default: 10s)
    --server-image IMAGE         Docker image name (default: grpc-server-test:latest)
    --proto-file PATH           Path to proto file (REQUIRED)
    --grpc-call CALL            gRPC service call (REQUIRED, e.g., package.Service.Method)
    --server-port PORT           Server port (default: 8585)
    --help                      Show this help message

Environment Variables:
    All options can also be set via environment variables (e.g., DURATION=5m)

Prerequisites:
    1. Docker installed and running
    2. ghz installed: go install github.com/bojand/ghz/cmd/ghz@latest
    3. A gRPC server Docker image that uses AsyncLogger
    4. Proto file for the gRPC service

Examples:
    # Basic usage with required parameters
    $0 --proto-file /path/to/service.proto --grpc-call package.Service.Method
    
    # Custom configuration
    $0 --proto-file service.proto --grpc-call myapp.UserService.GetUser --duration 5m --rps 1500
EOF
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Print header
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}    AsyncLogger Baseline Load Test                         ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Create results directory
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/baseline_${BUFFER_MB}mb_${SHARDS}s_${THREADS}t_${DURATION}"
mkdir -p "$RESULTS_DIR"

# Validate required parameters
if [ -z "$PROTO_FILE" ]; then
    echo -e "${RED}✗ Error: --proto-file is required${NC}"
    echo "  Example: --proto-file /path/to/service.proto"
    echo "  Use --help for more information"
    exit 1
fi

if [ -z "$GRPC_CALL" ]; then
    echo -e "${RED}✗ Error: --grpc-call is required${NC}"
    echo "  Example: --grpc-call package.Service.Method"
    echo "  Use --help for more information"
    exit 1
fi

# Display configuration
echo -e "${BLUE}Test Configuration:${NC}"
echo "  Buffer Size:        ${BUFFER_MB}MB"
echo "  Shards:             $SHARDS"
echo "  Concurrent Threads: $THREADS"
echo "  Target RPS:         $RPS"
echo "  Duration:           $DURATION"
echo "  Flush Interval:     $FLUSH_INTERVAL"
echo "  Server Port:        $SERVER_PORT"
echo "  Proto File:         $PROTO_FILE"
echo "  gRPC Call:          $GRPC_CALL"
echo "  Results Directory:  $RESULTS_DIR"
echo ""

# Check prerequisites
echo -e "${BLUE}Checking prerequisites...${NC}"

if ! command -v docker &> /dev/null; then
    echo -e "${RED}✗ Docker is not installed${NC}"
    echo "  Install Docker from https://docs.docker.com/get-docker/"
    exit 1
fi

if ! docker info &> /dev/null; then
    echo -e "${RED}✗ Docker daemon is not running${NC}"
    echo "  Start Docker Desktop or Docker daemon"
    exit 1
fi
echo -e "${GREEN}✓ Docker is available${NC}"

if ! command -v ghz &> /dev/null; then
    echo -e "${RED}✗ ghz is not installed${NC}"
    echo "  Install with: go install github.com/bojand/ghz/cmd/ghz@latest"
    exit 1
fi
echo -e "${GREEN}✓ ghz is available${NC}"

if [ ! -f "$PROTO_FILE" ]; then
    echo -e "${RED}✗ Proto file not found: $PROTO_FILE${NC}"
    echo "  Ensure the proto file exists and path is correct"
    exit 1
fi
echo -e "${GREEN}✓ Proto file found: $PROTO_FILE${NC}"
echo -e "${GREEN}✓ gRPC call: $GRPC_CALL${NC}"

# Check if server image exists
if ! docker images --format "{{.Repository}}:{{.Tag}}" | grep -q "^${SERVER_IMAGE}$"; then
    echo -e "${YELLOW}⚠ Server image not found: $SERVER_IMAGE${NC}"
    echo "  Build the server image first or update --server-image"
    echo "  Example: docker build -t $SERVER_IMAGE -f docker/Dockerfile.server ."
    exit 1
fi
echo -e "${GREEN}✓ Server image found: $SERVER_IMAGE${NC}"
echo ""

# Cleanup any existing container
echo -e "${BLUE}Cleaning up any existing containers...${NC}"
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true
echo -e "${GREEN}✓ Cleanup complete${NC}"
echo ""

# Start server container
echo -e "${BLUE}Starting server container...${NC}"
docker run -d \
    --name "$CONTAINER_NAME" \
    --cpus=4 \
    --memory=8g \
    -p $SERVER_PORT:$SERVER_PORT \
    -e BUFFER_SIZE=$((BUFFER_MB * 1024 * 1024)) \
    -e NUM_SHARDS=$SHARDS \
    -e FLUSH_INTERVAL=$FLUSH_INTERVAL \
    -e LOG_FILE=/app/logs/server.log \
    "$SERVER_IMAGE" \
    > /dev/null 2>&1

if [ $? -ne 0 ]; then
    echo -e "${RED}✗ Failed to start server container${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Server container started${NC}"

# Wait for server to be ready
echo -e "${BLUE}Waiting for server to be ready...${NC}"
MAX_WAIT=30
for i in $(seq 1 $MAX_WAIT); do
    if docker logs "$CONTAINER_NAME" 2>&1 | grep -q -E "(Server listening|Listening on|gRPC server started)"; then
        echo -e "${GREEN}✓ Server ready!${NC}"
        break
    fi
    if [ $i -eq $MAX_WAIT ]; then
        echo -e "${RED}✗ Server failed to start within ${MAX_WAIT}s${NC}"
        echo -e "${YELLOW}Server logs:${NC}"
        docker logs "$CONTAINER_NAME" 2>&1 | tail -20
        docker rm -f "$CONTAINER_NAME" 2>/dev/null || true
        exit 1
    fi
    sleep 1
done
echo ""

# Start resource monitoring in background
echo -e "${BLUE}Starting resource monitoring...${NC}"
RESOURCE_FILE="$RESULTS_DIR/resource_timeline_${TIMESTAMP}.csv"
{
    echo "timestamp,cpu_percent,mem_usage_mb,mem_percent,net_input_mb,net_output_mb,block_input_mb,block_output_mb"
    while docker ps --format "{{.Names}}" | grep -q "^${CONTAINER_NAME}$"; do
        STATS=$(docker stats --no-stream --format "{{.CPUPerc}},{{.MemUsage}},{{.NetIO}},{{.BlockIO}}" "$CONTAINER_NAME" 2>/dev/null || echo "0%,0B / 0B,0B / 0B,0B / 0B")
        
        CPU=$(echo "$STATS" | cut -d',' -f1 | sed 's/%//')
        MEM_RAW=$(echo "$STATS" | cut -d',' -f2)
        NET_RAW=$(echo "$STATS" | cut -d',' -f3)
        BLOCK_RAW=$(echo "$STATS" | cut -d',' -f4)
        
        # Parse memory
        MEM_USAGE=$(echo "$MEM_RAW" | awk '{print $1}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "0")
        MEM_LIMIT=$(echo "$MEM_RAW" | awk '{print $3}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "1")
        MEM_PCT=$(awk "BEGIN {printf \"%.2f\", ($MEM_USAGE/$MEM_LIMIT)*100}" 2>/dev/null || echo "0")
        
        # Parse network I/O
        NET_RX=$(echo "$NET_RAW" | awk '{print $1}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "0")
        NET_TX=$(echo "$NET_RAW" | awk '{print $3}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "0")
        
        # Parse block I/O
        BLOCK_READ=$(echo "$BLOCK_RAW" | awk '{print $1}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "0")
        BLOCK_WRITE=$(echo "$BLOCK_RAW" | awk '{print $3}' | numfmt --from=auto --to-unit=Mi 2>/dev/null || echo "0")
        
        TIMESTAMP_NOW=$(date +%s)
        echo "$TIMESTAMP_NOW,$CPU,$MEM_USAGE,$MEM_PCT,$NET_RX,$NET_TX,$BLOCK_READ,$BLOCK_WRITE"
        sleep 1
    done
} > "$RESOURCE_FILE" 2>/dev/null &
MONITOR_PID=$!
echo -e "${GREEN}✓ Resource monitoring started (PID: $MONITOR_PID)${NC}"
echo ""

# Run load test
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}    Running Load Test                                    ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}Configuration:${NC}"
echo "  RPS:       $RPS"
echo "  Duration:  $DURATION"
echo "  Threads:   $THREADS"
echo ""

GHZ_REPORT="$RESULTS_DIR/ghz_report_${TIMESTAMP}.json"
START_TIME=$(date +%s)

ghz --insecure \
    --proto "$PROTO_FILE" \
    --call "$GRPC_CALL" \
    --rps $RPS \
    --duration $DURATION \
    --concurrency $THREADS \
    --connections $THREADS \
    --format json \
    --output "$GHZ_REPORT" \
    localhost:$SERVER_PORT

END_TIME=$(date +%s)
DURATION_SEC=$((END_TIME - START_TIME))

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Load test completed successfully${NC}"
else
    echo -e "${RED}✗ Load test failed${NC}"
fi
echo ""

# Wait a moment for final metrics
sleep 3

# Collect server logs
echo -e "${BLUE}Collecting server logs...${NC}"
SERVER_LOG="$RESULTS_DIR/server_logs_${TIMESTAMP}.log"
docker logs "$CONTAINER_NAME" > "$SERVER_LOG" 2>&1
echo -e "${GREEN}✓ Server logs collected${NC}"

# Stop monitoring
echo -e "${BLUE}Stopping resource monitoring...${NC}"
kill $MONITOR_PID 2>/dev/null || true
wait $MONITOR_PID 2>/dev/null || true
echo -e "${GREEN}✓ Resource monitoring stopped${NC}"

# Stop and remove server container
echo -e "${BLUE}Stopping server container...${NC}"
docker stop "$CONTAINER_NAME" > /dev/null 2>&1
docker rm "$CONTAINER_NAME" > /dev/null 2>&1
echo -e "${GREEN}✓ Server container stopped and removed${NC}"

# Clean up log files
echo -e "${BLUE}Cleaning up log files...${NC}"
rm -f /tmp/*.log /tmp/logbytes_*.log /tmp/server_*.log 2>/dev/null || true
echo -e "${GREEN}✓ Log files cleaned${NC}"
echo ""

# Process and display results
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}                     Test Results                          ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Extract AsyncLogger metrics from server logs
if [ -f "$SERVER_LOG" ]; then
    LAST_METRICS=$(grep "METRICS:" "$SERVER_LOG" | tail -1 || echo "")
    LAST_FLUSH=$(grep "FLUSH_METRICS:" "$SERVER_LOG" | tail -1 || echo "")
    LAST_IO=$(grep "IO_BREAKDOWN:" "$SERVER_LOG" | tail -1 || echo "")
    FINAL_STATS=$(grep "Logger Stats" "$SERVER_LOG" | tail -1 || echo "")
    
    if [ -n "$LAST_METRICS" ]; then
        echo -e "${GREEN}AsyncLogger Metrics:${NC}"
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
    
    if [ -n "$FINAL_STATS" ]; then
        echo -e "${GREEN}Final Logger Stats:${NC}"
        echo "$FINAL_STATS" | sed 's/.*Logger Stats - //'
        echo ""
    fi
fi

# Process ghz results
if [ -f "$GHZ_REPORT" ]; then
    echo -e "${GREEN}gRPC Performance (ghz):${NC}"
    TOTAL=$(jq -r '.count' "$GHZ_REPORT" 2>/dev/null || echo "N/A")
    RPS_ACTUAL=$(jq -r '.rps' "$GHZ_REPORT" 2>/dev/null || echo "N/A")
    # ghz reports latency in nanoseconds, convert to milliseconds
    AVG_LATENCY_NS=$(jq -r '.average' "$GHZ_REPORT" 2>/dev/null || echo "0")
    P50_NS=$(jq -r '.latencyDistribution[] | select(.percentage == 50) | .latency' "$GHZ_REPORT" 2>/dev/null || echo "0")
    P95_NS=$(jq -r '.latencyDistribution[] | select(.percentage == 95) | .latency' "$GHZ_REPORT" 2>/dev/null || echo "0")
    P99_NS=$(jq -r '.latencyDistribution[] | select(.percentage == 99) | .latency' "$GHZ_REPORT" 2>/dev/null || echo "0")
    
    # Convert nanoseconds to milliseconds (divide by 1e6)
    AVG_LATENCY=$(echo "scale=2; $AVG_LATENCY_NS / 1000000" | bc 2>/dev/null || echo "N/A")
    P50=$(echo "scale=2; $P50_NS / 1000000" | bc 2>/dev/null || echo "N/A")
    P95=$(echo "scale=2; $P95_NS / 1000000" | bc 2>/dev/null || echo "N/A")
    P99=$(echo "scale=2; $P99_NS / 1000000" | bc 2>/dev/null || echo "N/A")
    
    echo "  Total Requests:    $TOTAL"
    echo "  Actual RPS:        $RPS_ACTUAL"
    echo "  Average Latency:   ${AVG_LATENCY}ms"
    echo "  P50 Latency:       ${P50}ms"
    echo "  P95 Latency:       ${P95}ms"
    echo "  P99 Latency:       ${P99}ms"
    echo ""
fi

# Process resource data
if [ -f "$RESOURCE_FILE" ] && [ -s "$RESOURCE_FILE" ]; then
    echo -e "${GREEN}Resource Utilization:${NC}"
    AVG_CPU=$(tail -n +2 "$RESOURCE_FILE" | awk -F',' '{sum+=$2; count++} END {if(count>0) printf "%.2f", sum/count; else print "0"}')
    MAX_CPU=$(tail -n +2 "$RESOURCE_FILE" | awk -F',' '{if($2>max) max=$2} END {printf "%.2f", max}')
    AVG_MEM=$(tail -n +2 "$RESOURCE_FILE" | awk -F',' '{sum+=$3; count++} END {if(count>0) printf "%.2f", sum/count; else print "0"}')
    MAX_MEM=$(tail -n +2 "$RESOURCE_FILE" | awk -F',' '{if($3>max) max=$3} END {printf "%.2f", max}')
    
    echo "  Average CPU:       ${AVG_CPU}%"
    echo "  Peak CPU:          ${MAX_CPU}%"
    echo "  Average Memory:    ${AVG_MEM}MB"
    echo "  Peak Memory:       ${MAX_MEM}MB"
    echo ""
fi

# Summary
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}Test Complete!${NC}"
echo ""
echo -e "${BLUE}Results saved to: $RESULTS_DIR/${NC}"
echo "  - Server logs:     server_logs_${TIMESTAMP}.log"
echo "  - ghz report:      ghz_report_${TIMESTAMP}.json"
echo "  - Resource data:   resource_timeline_${TIMESTAMP}.csv"
echo ""
echo -e "${BLUE}Test Duration: ${DURATION_SEC}s${NC}"
echo -e "${BLUE}Completed at: $(date)${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"

