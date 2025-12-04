#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

RESULTS_DIR="results/cliff_investigation"
PROFILE_DIR="$RESULTS_DIR/profiles"
DURATION="5m"
RPS=1000
THREADS=100

echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}       210s Cliff Investigation - Detailed Profiling         ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""
echo "Run started at: $(date)"
echo "Results directory: $RESULTS_DIR"
echo "Test configuration:"
echo "  - RPS: $RPS"
echo "  - Duration: $DURATION"
echo "  - Threads: $THREADS (baseline config)"
echo "  - Profiles: 200s, 210s, 220s, 240s"
echo ""

# Cleanup any previous test containers
echo -e "${BLUE}Cleaning up previous test resources...${NC}"
docker rm -f cliff-investigation 2>/dev/null || true

# Check Docker storage
echo -e "${BLUE}Checking Docker storage...${NC}"
docker system df

# Cleanup if needed (remove dangling images, stopped containers, unused volumes)
echo -e "${BLUE}Cleaning up Docker resources...${NC}"
docker system prune -f > /dev/null 2>&1
echo -e "${GREEN}✓ Docker cleanup complete${NC}"
echo ""

# Create results directories
mkdir -p "$RESULTS_DIR"
mkdir -p "$PROFILE_DIR"

# Build Docker image
echo -e "${BLUE}Building Docker image...${NC}"
docker build -f docker/Dockerfile.server -t logger-server:latest . > /dev/null 2>&1
if [ $? -ne 0 ]; then
  echo -e "${RED}✗ Docker build failed!${NC}"
  echo -e "${YELLOW}Trying verbose build...${NC}"
  docker build -f docker/Dockerfile.server -t logger-server:latest .
  exit 1
fi
echo -e "${GREEN}✓ Docker image built${NC}"
echo ""

# Start container
echo -e "${BLUE}Starting server container...${NC}"
CONTAINER_ID=$(docker run -d --rm \
  --name cliff-investigation \
  --cpus="4" \
  --memory="4g" \
  -p 8585:8585 \
  -p 6060:6060 \
  -v "$(pwd)/results:/app/results" \
  -e "BUFFER_SIZE=134217728" \
  -e "NUM_SHARDS=8" \
  logger-server:latest)

echo -e "${GREEN}✓ Container started: $CONTAINER_ID${NC}"

# Wait for server to be ready
echo -e "${BLUE}Waiting for server to be ready...${NC}"
sleep 3

# Check if container is still running
if ! docker ps | grep -q cliff-investigation; then
  echo -e "${RED}✗ Container exited!${NC}"
  docker logs cliff-investigation 2>&1 | tail -20
  exit 1
fi

# Wait a bit more for server to fully start
sleep 2

# Verify ports are accessible
if ! nc -z localhost 8585 2>/dev/null && ! lsof -i :8585 > /dev/null 2>&1; then
  echo -e "${YELLOW}⚠️  Port 8585 not accessible yet, waiting...${NC}"
  sleep 5
fi

echo -e "${GREEN}✓ Server ready (container running, ports exposed)${NC}"
echo ""

# Start resource monitoring in background
echo -e "${BLUE}Starting resource monitoring...${NC}"
docker stats cliff-investigation --no-stream --format "{{.CPUPerc}},{{.MemUsage}},{{.NetIO}},{{.BlockIO}}" > "$RESULTS_DIR/docker_stats_start.txt"

(
  echo "timestamp,cpu_percent,mem_usage_mb,mem_limit_mb,net_in_mb,net_out_mb,block_in_mb,block_out_mb"
  while docker ps | grep -q cliff-investigation; do
    timestamp=$(date +%s)
    stats=$(docker stats cliff-investigation --no-stream --format "{{.CPUPerc}},{{.MemUsage}},{{.NetIO}},{{.BlockIO}}")
    
    # Parse stats
    cpu=$(echo "$stats" | cut -d',' -f1 | tr -d '%')
    mem_raw=$(echo "$stats" | cut -d',' -f2)
    mem_usage=$(echo "$mem_raw" | awk '{print $1}' | sed 's/GiB/*1024/;s/MiB//;s/KiB\/1024/' | bc 2>/dev/null || echo "0")
    mem_limit=$(echo "$mem_raw" | awk '{print $3}' | sed 's/GiB/*1024/;s/MiB//;s/KiB\/1024/' | bc 2>/dev/null || echo "0")
    
    net_raw=$(echo "$stats" | cut -d',' -f3)
    net_in=$(echo "$net_raw" | awk '{print $1}' | sed 's/GB/*1024/;s/MB//;s/kB\/1024/' | bc 2>/dev/null || echo "0")
    net_out=$(echo "$net_raw" | awk '{print $3}' | sed 's/GB/*1024/;s/MB//;s/kB\/1024/' | bc 2>/dev/null || echo "0")
    
    block_raw=$(echo "$stats" | cut -d',' -f4)
    block_in=$(echo "$block_raw" | awk '{print $1}' | sed 's/GB/*1024/;s/MB//;s/kB\/1024/' | bc 2>/dev/null || echo "0")
    block_out=$(echo "$block_raw" | awk '{print $3}' | sed 's/GB/*1024/;s/MB//;s/kB\/1024/' | bc 2>/dev/null || echo "0")
    
    echo "$timestamp,$cpu,$mem_usage,$mem_limit,$net_in,$net_out,$block_in,$block_out"
    sleep 2
  done
) > "$RESULTS_DIR/resource_timeline.csv" 2>/dev/null &
MONITOR_PID=$!

echo -e "${GREEN}✓ Resource monitoring started${NC}"
echo ""

# Start load test in background
echo -e "${BLUE}Starting load test: $RPS RPS for $DURATION with $THREADS concurrent threads...${NC}"
ghz --insecure \
  --proto proto/random_numbers.proto \
  --call randomnumbers.RandomNumberService.GetRandomNumbers \
  -d '{}' \
  -c "$THREADS" \
  -z "$DURATION" \
  --rps "$RPS" \
  --connections 10 \
  --format json \
  --output "$RESULTS_DIR/ghz_report.json" \
  localhost:8585 > "$RESULTS_DIR/ghz_output.txt" 2>&1 &
LOAD_PID=$!

echo -e "${GREEN}✓ Load test started (PID: $LOAD_PID)${NC}"
echo ""

# Profile collection function
collect_profile() {
  local time_mark=$1
  local profile_type=$2
  local output_file="$PROFILE_DIR/${profile_type}_${time_mark}s.prof"
  
  echo -e "${YELLOW}📊 Collecting $profile_type profile at $time_mark seconds...${NC}"
  curl -s "http://localhost:6060/debug/pprof/$profile_type" > "$output_file"
  echo -e "${GREEN}✓ $profile_type profile saved: $(basename $output_file)${NC}"
}

# Collect profiles at critical moments
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}       Collecting Profiles at Critical Moments              ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# 200s (before cliff)
echo -e "${BLUE}Waiting for 200s mark (before cliff)...${NC}"
sleep 200
collect_profile "200" "heap"
collect_profile "200" "goroutine"
collect_profile "200" "allocs"
echo ""

# 210s (at cliff)
echo -e "${RED}Waiting for 210s mark (at the cliff!)...${NC}"
sleep 10
collect_profile "210" "heap"
collect_profile "210" "goroutine"
collect_profile "210" "allocs"
collect_profile "210" "block"
echo ""

# 220s (after cliff)
echo -e "${YELLOW}Waiting for 220s mark (after cliff)...${NC}"
sleep 10
collect_profile "220" "heap"
collect_profile "220" "goroutine"
collect_profile "220" "allocs"
echo ""

# 240s (degraded state)
echo -e "${YELLOW}Waiting for 240s mark (degraded state)...${NC}"
sleep 20
collect_profile "240" "heap"
collect_profile "240" "goroutine"
echo ""

# Wait for load test to complete
echo -e "${BLUE}Waiting for load test to complete...${NC}"
wait $LOAD_PID
echo -e "${GREEN}✓ Load test completed!${NC}"
echo ""

# Stop resource monitoring
kill $MONITOR_PID 2>/dev/null || true

# Collect server logs
echo -e "${BLUE}Collecting server logs...${NC}"
docker logs cliff-investigation > "$RESULTS_DIR/server.log" 2>&1
echo -e "${GREEN}✓ Server logs saved${NC}"
echo ""

# Stop container
echo -e "${YELLOW}Stopping server...${NC}"
docker stop cliff-investigation > /dev/null 2>&1
echo -e "${GREEN}✓ Server stopped${NC}"
echo ""

echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}       ✓✓✓ INVESTIGATION COMPLETE! ✓✓✓                   ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${GREEN}Run completed at: $(date)${NC}"
echo -e "${GREEN}Results directory: $RESULTS_DIR${NC}"
echo ""
echo -e "${YELLOW}Next step: Analyze the results${NC}"
echo -e "${BLUE}  go run scripts/analyze_cliff.go${NC}"
echo ""

