#!/bin/bash

# Safe Baseline Test Script - Handles Docker hanging issues
# Tests: 100 concurrent threads, 8 shards, 64MB buffer, 300KB logs, 5 minutes

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}    Baseline Confirmation Test (Safe Mode)                 ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/retry_test"
CONTAINER_NAME="grpc-server-retry-test"
SERVER_PORT=8585
GHZ_REPORT="${RESULTS_DIR}/ghz_report_${TIMESTAMP}.json"

# Test parameters
CONCURRENT_THREADS=100
RPS=1000
DURATION="10m"
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
echo ""

# Create results directory
mkdir -p "$RESULTS_DIR"

# Function to check Docker with timeout
check_docker() {
    local timeout=3
    if command -v gtimeout &> /dev/null; then
        gtimeout $timeout docker info > /dev/null 2>&1
    elif command -v timeout &> /dev/null; then
        timeout $timeout docker info > /dev/null 2>&1
    else
        # Use background process with kill
        docker info > /dev/null 2>&1 &
        local pid=$!
        sleep $timeout
        if kill -0 $pid 2>/dev/null; then
            kill -9 $pid 2>/dev/null
            return 1
        fi
        wait $pid
        return $?
    fi
}

# Check Docker with timeout
echo -e "${BLUE}Checking Docker...${NC}"
if ! check_docker; then
    echo -e "${RED}Error: Docker is not responding (may be stuck)${NC}"
    echo -e "${YELLOW}Attempting to clean up stuck processes...${NC}"
    pkill -9 -f "docker.*grpc-server-retry-test" 2>/dev/null || true
    pkill -9 -f "docker stats" 2>/dev/null || true
    sleep 2
    
    if ! check_docker; then
        echo -e "${RED}Error: Docker is still not responding${NC}"
        echo -e "${YELLOW}Please restart Docker Desktop and try again${NC}"
        exit 1
    fi
fi
echo -e "${GREEN}✓ Docker is accessible${NC}"

# Clean up any existing container (with timeout protection)
echo -e "${BLUE}Cleaning up existing containers...${NC}"
docker stop "$CONTAINER_NAME" 2>/dev/null || true
docker rm "$CONTAINER_NAME" 2>/dev/null || true
sleep 1

# Build Docker image
echo -e "${BLUE}Building Docker image...${NC}"
cd "$(dirname "$0")/.."
docker build -f docker/Dockerfile.server -t grpc-server:test . || {
    echo -e "${RED}Error: Docker build failed${NC}"
    exit 1
}

# Start server container
echo -e "${BLUE}Starting server container...${NC}"
docker run -d \
    --name "$CONTAINER_NAME" \
    --cpus="4" \
    --memory="4g" \
    -p "${SERVER_PORT}:${SERVER_PORT}" \
    -e BUFFER_SIZE=$((BUFFER_MB * 1024 * 1024)) \
    -e NUM_SHARDS=$SHARDS \
    -e FLUSH_INTERVAL=$FLUSH_INTERVAL \
    -e LOG_FILE=/app/logs/server.log \
    grpc-server:test || {
    echo -e "${RED}Error: Failed to start container${NC}"
    exit 1
}

# Wait for server to start
echo -e "${BLUE}Waiting for server to start...${NC}"
sleep 5

# Check if server is running
if ! docker ps | grep -q "$CONTAINER_NAME"; then
    echo -e "${RED}Error: Server container failed to start${NC}"
    docker logs "$CONTAINER_NAME" 2>&1 | tail -20
    exit 1
fi

# Run ghz load test
echo -e "${BLUE}Running ghz load test...${NC}"
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
    localhost:${SERVER_PORT} || {
    echo -e "${RED}Error: ghz test failed${NC}"
    docker stop "$CONTAINER_NAME" 2>/dev/null || true
    exit 1
}

# Wait a bit for final metrics
sleep 2

# Extract final metrics from server logs
echo -e "${BLUE}Extracting metrics...${NC}"
docker logs "$CONTAINER_NAME" 2>&1 | grep -E "Logger Stats|METRICS|FLUSH_METRICS|IO_BREAKDOWN" | tail -15

# Stop container
echo -e "${BLUE}Stopping server container...${NC}"
docker stop "$CONTAINER_NAME" 2>/dev/null || true

# Clean up log file to prevent storage clogging
echo -e "${BLUE}Cleaning up log files...${NC}"
docker exec "$CONTAINER_NAME" rm -f /app/logs/server.log 2>/dev/null || true

docker rm "$CONTAINER_NAME" 2>/dev/null || true

# Parse results
echo ""
echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}Test Results Summary${NC}"
echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Parse ghz results
if [ -f "$GHZ_REPORT" ]; then
    echo -e "${BLUE}gRPC Performance:${NC}"
    python3 << EOF
import json
try:
    with open("$GHZ_REPORT", 'r') as f:
        d = json.load(f)
    print(f"  Total Requests: {d['total']:,}")
    print(f"  Successful: {d['statusCodeDistribution'].get('OK', 0):,}")
    print(f"  Failed: {d['statusCodeDistribution'].get('Unavailable', 0):,}")
    print(f"  RPS: {d['rps']:.2f}")
    lat = d['latencyDistribution']
    for p in lat:
        if p['percentage'] in [50, 95, 99]:
            print(f"  P{p['percentage']}: {p['latency']/1e6:.3f}ms")
except Exception as e:
    print(f"  Error parsing results: {e}")
EOF
fi

echo ""
echo -e "${GREEN}Test completed!${NC}"
echo -e "${BLUE}Results saved to: $GHZ_REPORT${NC}"

