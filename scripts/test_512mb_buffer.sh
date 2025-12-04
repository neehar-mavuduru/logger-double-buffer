#!/bin/bash
# test_512mb_buffer.sh
# Test larger buffer (512MB) to reduce flush frequency
# Goal: Reduce flushes from 0.4s to 1.5s to improve disk I/O stability

set -e

# Configuration
CONTAINER_NAME="test-512mb-buffer"
RESULTS_DIR="results/buffer_512mb"
TEST_DURATION="10m"
RPS=1000
THREADS=100
BUFFER_SIZE="512MB"
SHARDS=8

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# Helper functions
print_header() {
    echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${CYAN}       $1${NC}"
    echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"
    echo ""
}

print_step() {
    echo -e "${BLUE}$1${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}📊 $1${NC}"
}

# Start
print_header "512MB Buffer Test - Reduced Flush Frequency"
echo -e "Run started at: $(date)"
echo -e "Results directory: ${RESULTS_DIR}"
echo -e "Test configuration:"
echo -e "  - RPS: ${RPS}"
echo -e "  - Duration: ${TEST_DURATION}"
echo -e "  - Threads: ${THREADS}"
echo -e "  - Buffer: ${BUFFER_SIZE} (4× larger than baseline!), Shards: ${SHARDS}"
echo -e "  - Expected flush frequency: ~1.5s (vs 0.4s with 128MB)"
echo ""

# Create results directory
mkdir -p "${RESULTS_DIR}"

# Cleanup previous test
print_step "Cleaning up previous test resources..."
docker rm -f ${CONTAINER_NAME} 2>/dev/null || true
docker system prune -f > /dev/null 2>&1
print_success "Docker cleanup complete"
echo ""

# Build Docker image
print_step "Building Docker image..."
if docker build -f docker/Dockerfile.server -t logger-server:latest . > /dev/null 2>&1; then
    print_success "Docker image built"
else
    print_error "Docker build failed!"
    exit 1
fi
echo ""

# Start server container
print_step "Starting server container with 512MB buffer..."
CONTAINER_ID=$(docker run -d \
    --name ${CONTAINER_NAME} \
    --cpus=4 \
    --memory=4g \
    -p 8585:8585 \
    -p 6060:6060 \
    -e BUFFER_SIZE=$((512*1024*1024)) \
    -e NUM_SHARDS=8 \
    -e FLUSH_INTERVAL=10s \
    -e LOG_FILE=/tmp/server.log \
    logger-server:latest)

if [ -z "$CONTAINER_ID" ]; then
    print_error "Failed to start container!"
    exit 1
fi

print_success "Container started: ${CONTAINER_ID}"
echo ""

# Wait for server readiness
print_step "Waiting for server to be ready..."
sleep 3

if ! docker ps | grep -q ${CONTAINER_NAME}; then
    print_error "Container exited early!"
    docker logs ${CONTAINER_NAME}
    exit 1
fi

sleep 2

if ! nc -z localhost 8585 2>/dev/null; then
    print_warning "Port not responding yet, waiting 5 more seconds..."
    sleep 5
fi

print_success "Server ready (container running, ports exposed)"
echo ""

# Start resource monitoring
print_step "Starting resource monitoring..."
(
    echo "timestamp,cpu_percent,mem_usage_mb,mem_limit_mb,net_in_mb,net_out_mb,block_in_mb,block_out_mb"
    while docker ps | grep -q ${CONTAINER_NAME}; do
        timestamp=$(date +%s)
        stats=$(docker stats ${CONTAINER_NAME} --no-stream --format "{{.CPUPerc}},{{.MemUsage}},{{.NetIO}},{{.BlockIO}}")
        
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

print_success "Resource monitoring started"
echo ""

# Start load test
print_step "Starting load test: ${RPS} RPS for ${TEST_DURATION} with ${THREADS} concurrent threads..."

ghz --insecure \
    --proto proto/random_numbers.proto \
    --call randomnumbers.RandomNumberService.GetRandomNumbers \
    --rps ${RPS} \
    --duration ${TEST_DURATION} \
    --connections ${THREADS} \
    --concurrency ${THREADS} \
    --format json \
    localhost:8585 > "$RESULTS_DIR/ghz_report.json" 2>&1 &

LOAD_PID=$!
print_success "Load test started (PID: ${LOAD_PID})"
echo ""

# Profile collection at key moments
print_header "Collecting Profiles at Key Moments"

# Collect profiles at 2, 5, 8, and 10 minutes
for minute in 2 5 8 10; do
    seconds=$((minute * 60))
    
    print_step "Waiting for ${minute}-minute mark..."
    
    # Wait until that time
    while [ $(ps -p ${LOAD_PID} -o etimes= 2>/dev/null | tr -d ' ') -lt $((seconds - 5)) ] 2>/dev/null; do
        sleep 5
    done
    
    # Small adjustment to hit exact time
    current_time=$(ps -p ${LOAD_PID} -o etimes= 2>/dev/null | tr -d ' ')
    if [ ! -z "$current_time" ] && [ $current_time -lt $seconds ]; then
        sleep_duration=$((seconds - current_time))
        sleep $sleep_duration
    fi
    
    print_info "Collecting profiles at ${minute} minutes..."
    
    # Heap profile
    if curl -s http://localhost:6060/debug/pprof/heap > "$RESULTS_DIR/heap_${minute}min.prof" 2>/dev/null; then
        print_success "heap profile saved: heap_${minute}min.prof"
    fi
    
    # Goroutine profile
    if curl -s http://localhost:6060/debug/pprof/goroutine > "$RESULTS_DIR/goroutine_${minute}min.prof" 2>/dev/null; then
        print_success "goroutine profile saved: goroutine_${minute}min.prof"
    fi
    
    echo ""
done

# Wait for load test completion
print_step "Waiting for load test to complete..."
wait $LOAD_PID 2>/dev/null || true
print_success "Load test completed!"
echo ""

# Stop resource monitoring
kill $MONITOR_PID 2>/dev/null || true

# Collect server logs
print_step "Collecting server logs..."
docker logs ${CONTAINER_NAME} > "$RESULTS_DIR/server.log" 2>&1
print_success "Server logs saved"
echo ""

# Stop server
print_step "Stopping server..."
docker stop ${CONTAINER_NAME} > /dev/null 2>&1
docker rm ${CONTAINER_NAME} > /dev/null 2>&1
print_success "Server stopped"
echo ""

# Final summary
print_header "✓✓✓ 512MB BUFFER TEST COMPLETE! ✓✓✓"
echo ""
echo -e "${GREEN}Run completed at: $(date)${NC}"
echo -e "${GREEN}Results directory: ${RESULTS_DIR}${NC}"
echo ""
echo -e "${YELLOW}Next step: Analyze the results${NC}"
echo -e "${BLUE}  grep -E \"(METRICS:|IO_BREAKDOWN:)\" ${RESULTS_DIR}/server.log | tail -100${NC}"
echo ""

