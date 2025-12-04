#!/bin/bash

# Buffer Optimization Study
# Tests 27 targeted scenarios varying buffer size, shard count, and thread count
# Goal: Find optimal configuration for 1000 RPS with minimal CPU and 0% drops

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
MAGENTA='\033[0;35m'
NC='\033[0m' # No Color

echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}       Buffer Optimization Study - 27 Scenarios             ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/buffer_optimization"
CONTAINER_NAME="grpc-server-buffer-opt"
SERVER_PORT=8585
RPS=1000
DURATION="2m"
FLUSH_INTERVAL="5s"

# Create results directory
mkdir -p "$RESULTS_DIR"

# Log file for this run
RUN_LOG="${RESULTS_DIR}/run_${TIMESTAMP}.log"
touch "$RUN_LOG"

echo "Run started at: $(date)" | tee -a "$RUN_LOG"
echo "Results directory: $RESULTS_DIR" | tee -a "$RUN_LOG"
echo "" | tee -a "$RUN_LOG"

# Function to run a single scenario
run_scenario() {
    local scenario_id=$1
    local phase=$2
    local buffer_mb=$3
    local shards=$4
    local threads=$5
    local description=$6
    
    echo -e "${MAGENTA}────────────────────────────────────────────────────────────${NC}"
    echo -e "${GREEN}Scenario $scenario_id: $description${NC}"
    echo -e "${YELLOW}  Buffer: ${buffer_mb}MB | Shards: $shards | Threads: $threads${NC}"
    echo -e "${MAGENTA}────────────────────────────────────────────────────────────${NC}"
    
    # Log scenario details
    echo "=== Scenario $scenario_id: $description ===" >> "$RUN_LOG"
    echo "Phase: $phase" >> "$RUN_LOG"
    echo "Buffer: ${buffer_mb}MB, Shards: $shards, Threads: $threads" >> "$RUN_LOG"
    echo "Timestamp: $(date)" >> "$RUN_LOG"
    
    # File paths for this scenario
    local resource_file="${RESULTS_DIR}/scenario_${scenario_id}_resources.csv"
    local ghz_report="${RESULTS_DIR}/scenario_${scenario_id}_ghz.json"
    local server_log="${RESULTS_DIR}/scenario_${scenario_id}_server.log"
    local metadata_file="${RESULTS_DIR}/scenario_${scenario_id}_metadata.json"
    
    # Write metadata
    cat > "$metadata_file" <<EOF
{
  "scenario_id": "$scenario_id",
  "phase": "$phase",
  "description": "$description",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "config": {
    "buffer_mb": $buffer_mb,
    "shards": $shards,
    "threads": $threads,
    "rps": $RPS,
    "duration": "$DURATION",
    "flush_interval": "$FLUSH_INTERVAL"
  }
}
EOF
    
    # Clean up any existing container
    docker rm -f "$CONTAINER_NAME" > /dev/null 2>&1 || true
    sleep 1
    
    # Start server container
    echo -e "${BLUE}Starting server...${NC}"
    docker run -d \
        --name "$CONTAINER_NAME" \
        --cpus=4 \
        --memory=4g \
        -p ${SERVER_PORT}:8585 \
        -e BUFFER_SIZE=$((buffer_mb * 1024 * 1024)) \
        -e NUM_SHARDS=$shards \
        -e FLUSH_INTERVAL=$FLUSH_INTERVAL \
        grpc-server:latest \
        > /dev/null
    
    # Wait for server to be ready
    echo -e "${BLUE}Waiting for server to be ready...${NC}"
    for i in {1..30}; do
        if docker logs "$CONTAINER_NAME" 2>&1 | grep -q "Server listening"; then
            echo -e "${GREEN}✓ Server ready!${NC}"
            break
        fi
        sleep 1
    done
    
    # Start resource monitoring in background
    echo -e "${BLUE}Starting resource monitoring...${NC}"
    ./scripts/monitor_docker_resources.sh "$CONTAINER_NAME" "$resource_file" 150 > /dev/null 2>&1 &
    MONITOR_PID=$!
    
    sleep 2
    
    # Run ghz load test
    echo -e "${BLUE}Running load test: $RPS RPS for $DURATION with $threads concurrent threads...${NC}"
    ghz --insecure \
        --proto proto/random_numbers.proto \
        --call randomnumbers.RandomNumberService.GetRandomNumbers \
        --rps $RPS \
        --duration $DURATION \
        --connections 50 \
        --concurrency $threads \
        --format json \
        --output "$ghz_report" \
        localhost:${SERVER_PORT} 2>&1 | tee -a "$RUN_LOG"
    
    echo -e "${GREEN}✓ Load test completed!${NC}"
    
    # Collect server logs
    echo -e "${BLUE}Collecting server logs...${NC}"
    docker logs "$CONTAINER_NAME" > "$server_log" 2>&1
    
    # Stop resource monitoring
    kill $MONITOR_PID 2>/dev/null || true
    wait $MONITOR_PID 2>/dev/null || true
    
    # Stop and remove container
    echo -e "${YELLOW}Stopping server...${NC}"
    docker stop "$CONTAINER_NAME" > /dev/null 2>&1 || true
    docker rm "$CONTAINER_NAME" > /dev/null 2>&1 || true
    
    echo -e "${GREEN}✓ Scenario $scenario_id complete!${NC}"
    echo "" | tee -a "$RUN_LOG"
    
    # Brief pause between scenarios
    sleep 2
}

# Build Docker image once
echo -e "${BLUE}Building Docker image...${NC}"
docker build -f docker/Dockerfile.server -t grpc-server:latest . > /dev/null 2>&1
echo -e "${GREEN}✓ Docker image built${NC}"
echo ""

# Track progress
TOTAL_SCENARIOS=27
CURRENT_SCENARIO=0
START_TIME=$(date +%s)

# Phase 1: Minimum Viable Buffer (6 tests)
# Fixed: 8 shards, 100 threads
# Vary: Buffer [8MB, 16MB, 32MB, 64MB, 128MB, 256MB]
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  PHASE 1: Minimum Viable Buffer (6 scenarios)              ${NC}"
echo -e "${BLUE}  Goal: Find smallest buffer with 0% drops                  ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

PHASE="Phase1_MinViableBuffer"
SHARDS=8
THREADS=100

for BUFFER in 8 16 32 64 128 256; do
    CURRENT_SCENARIO=$((CURRENT_SCENARIO + 1))
    ELAPSED=$(($(date +%s) - START_TIME))
    AVG_TIME=$((ELAPSED / CURRENT_SCENARIO))
    REMAINING=$((TOTAL_SCENARIOS - CURRENT_SCENARIO))
    ETA=$((AVG_TIME * REMAINING))
    
    echo -e "${YELLOW}Progress: ${CURRENT_SCENARIO}/${TOTAL_SCENARIOS} | Elapsed: ${ELAPSED}s | ETA: ${ETA}s${NC}"
    
    run_scenario "p1_${BUFFER}mb" "$PHASE" $BUFFER $SHARDS $THREADS \
        "Phase 1: ${BUFFER}MB buffer, $SHARDS shards, $THREADS threads"
done

echo -e "${GREEN}✓✓✓ Phase 1 Complete! ✓✓✓${NC}"
echo ""

# Analyze Phase 1 to find best buffer
echo -e "${BLUE}Analyzing Phase 1 results...${NC}"
# For now, we'll manually determine the winner or use all results in final analysis
# In production, we'd parse results here to auto-select
echo -e "${YELLOW}Manual review needed - using 64MB as baseline for Phase 2${NC}"
BEST_BUFFER=64
echo ""

# Phase 2: Shard Optimization (6 tests)
# Fixed: Best buffer from Phase 1 (assume 64MB), 100 threads
# Vary: Shards [1, 2, 4, 8, 16, 32]
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  PHASE 2: Shard Optimization (6 scenarios)                 ${NC}"
echo -e "${BLUE}  Goal: Find optimal shard count for lock contention        ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

PHASE="Phase2_ShardOptimization"
BUFFER=$BEST_BUFFER
THREADS=100

for SHARDS in 1 2 4 8 16 32; do
    CURRENT_SCENARIO=$((CURRENT_SCENARIO + 1))
    ELAPSED=$(($(date +%s) - START_TIME))
    AVG_TIME=$((ELAPSED / CURRENT_SCENARIO))
    REMAINING=$((TOTAL_SCENARIOS - CURRENT_SCENARIO))
    ETA=$((AVG_TIME * REMAINING))
    
    echo -e "${YELLOW}Progress: ${CURRENT_SCENARIO}/${TOTAL_SCENARIOS} | Elapsed: ${ELAPSED}s | ETA: ${ETA}s${NC}"
    
    run_scenario "p2_${SHARDS}shards" "$PHASE" $BUFFER $SHARDS $THREADS \
        "Phase 2: ${BUFFER}MB buffer, $SHARDS shards, $THREADS threads"
done

echo -e "${GREEN}✓✓✓ Phase 2 Complete! ✓✓✓${NC}"
echo ""

# Assume best shard count (manual review or auto-detect)
echo -e "${YELLOW}Manual review needed - using 8 shards as baseline for Phase 3${NC}"
BEST_SHARDS=8
echo ""

# Phase 3: Thread Scaling (3 tests)
# Fixed: Best buffer + shards from previous phases
# Vary: Threads [10, 50, 100]
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  PHASE 3: Thread Scaling (3 scenarios)                     ${NC}"
echo -e "${BLUE}  Goal: Verify scalability across concurrency levels        ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

PHASE="Phase3_ThreadScaling"
BUFFER=$BEST_BUFFER
SHARDS=$BEST_SHARDS

for THREADS in 10 50 100; do
    CURRENT_SCENARIO=$((CURRENT_SCENARIO + 1))
    ELAPSED=$(($(date +%s) - START_TIME))
    AVG_TIME=$((ELAPSED / CURRENT_SCENARIO))
    REMAINING=$((TOTAL_SCENARIOS - CURRENT_SCENARIO))
    ETA=$((AVG_TIME * REMAINING))
    
    echo -e "${YELLOW}Progress: ${CURRENT_SCENARIO}/${TOTAL_SCENARIOS} | Elapsed: ${ELAPSED}s | ETA: ${ETA}s${NC}"
    
    run_scenario "p3_${THREADS}threads" "$PHASE" $BUFFER $SHARDS $THREADS \
        "Phase 3: ${BUFFER}MB buffer, $SHARDS shards, $THREADS threads"
done

echo -e "${GREEN}✓✓✓ Phase 3 Complete! ✓✓✓${NC}"
echo ""

# Phase 4: Cross-Validation (12 tests)
# Test top 4 configs across all thread counts
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  PHASE 4: Cross-Validation (12 scenarios)                  ${NC}"
echo -e "${BLUE}  Goal: Validate top configs across thread counts           ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

PHASE="Phase4_CrossValidation"

# Define top 4 configs (these are examples - adjust based on Phase 1-3 results)
declare -a TOP_CONFIGS=(
    "32:4"    # 32MB, 4 shards
    "64:8"    # 64MB, 8 shards
    "128:8"   # 128MB, 8 shards
    "64:16"   # 64MB, 16 shards
)

for CONFIG in "${TOP_CONFIGS[@]}"; do
    BUFFER=$(echo $CONFIG | cut -d':' -f1)
    SHARDS=$(echo $CONFIG | cut -d':' -f2)
    
    for THREADS in 10 50 100; do
        CURRENT_SCENARIO=$((CURRENT_SCENARIO + 1))
        ELAPSED=$(($(date +%s) - START_TIME))
        AVG_TIME=$((ELAPSED / CURRENT_SCENARIO))
        REMAINING=$((TOTAL_SCENARIOS - CURRENT_SCENARIO))
        ETA=$((AVG_TIME * REMAINING))
        
        echo -e "${YELLOW}Progress: ${CURRENT_SCENARIO}/${TOTAL_SCENARIOS} | Elapsed: ${ELAPSED}s | ETA: ${ETA}s${NC}"
        
        run_scenario "p4_${BUFFER}mb_${SHARDS}s_${THREADS}t" "$PHASE" $BUFFER $SHARDS $THREADS \
            "Phase 4: ${BUFFER}MB buffer, $SHARDS shards, $THREADS threads"
    done
done

echo -e "${GREEN}✓✓✓ Phase 4 Complete! ✓✓✓${NC}"
echo ""

# Final summary
TOTAL_TIME=$(($(date +%s) - START_TIME))
MINUTES=$((TOTAL_TIME / 60))
SECONDS=$((TOTAL_TIME % 60))

echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}       ✓✓✓ ALL 27 SCENARIOS COMPLETE! ✓✓✓                  ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${GREEN}Total runtime: ${MINUTES}m ${SECONDS}s${NC}"
echo -e "${GREEN}Results directory: $RESULTS_DIR${NC}"
echo ""
echo -e "${YELLOW}Next step: Process results${NC}"
echo -e "${BLUE}  go run scripts/process_buffer_optimization.go${NC}"
echo ""

# Log completion
echo "Run completed at: $(date)" >> "$RUN_LOG"
echo "Total scenarios: $TOTAL_SCENARIOS" >> "$RUN_LOG"
echo "Total runtime: ${MINUTES}m ${SECONDS}s" >> "$RUN_LOG"

echo -e "${GREEN}Log file: $RUN_LOG${NC}"
echo ""

