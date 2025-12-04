#!/bin/bash

# Thread Scaling Study
# Tests 18 scenarios for 50 and 200 concurrent threads to find optimal buffer and shard configurations
# Goal: Understand how to tune params as thread count increases

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}       Thread Scaling Study - 18 Scenarios (5 min each)     ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_DIR="results/thread_scaling"
CONTAINER_NAME="grpc-server-thread-scaling"
SERVER_PORT=8585
RPS=1000
DURATION="2m"  # 2 minutes per test (matches proven baseline)
FLUSH_INTERVAL="5s"

# Create results directories
mkdir -p "$RESULTS_DIR/50_threads"
mkdir -p "$RESULTS_DIR/200_threads"

# Log file for this run
RUN_LOG="${RESULTS_DIR}/run_${TIMESTAMP}.log"
touch "$RUN_LOG"

echo "Run started at: $(date)" | tee -a "$RUN_LOG"
echo "Results directory: $RESULTS_DIR" | tee -a "$RUN_LOG"
echo "Test duration per scenario: $DURATION (5 minutes)" | tee -a "$RUN_LOG"
echo "" | tee -a "$RUN_LOG"

# Function to run a single scenario
run_scenario() {
    local scenario_id=$1
    local phase=$2
    local buffer_mb=$3
    local shards=$4
    local threads=$5
    local description=$6
    local subdir=$7  # 50_threads or 200_threads
    
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
    local resource_file="${RESULTS_DIR}/${subdir}/scenario_${scenario_id}_resources.csv"
    local ghz_report="${RESULTS_DIR}/${subdir}/scenario_${scenario_id}_ghz.json"
    local server_log="${RESULTS_DIR}/${subdir}/scenario_${scenario_id}_server.log"
    local metadata_file="${RESULTS_DIR}/${subdir}/scenario_${scenario_id}_metadata.json"
    
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
    ./scripts/monitor_docker_resources.sh "$CONTAINER_NAME" "$resource_file" 350 > /dev/null 2>&1 &
    MONITOR_PID=$!
    
    sleep 2
    
    # Adjust connections for thread count
    local connections=50
    if [ $threads -lt 50 ]; then
        connections=$threads
    fi
    
    # Run ghz load test
    echo -e "${BLUE}Running load test: $RPS RPS for $DURATION with $threads concurrent threads...${NC}"
    ghz --insecure \
        --proto proto/random_numbers.proto \
        --call randomnumbers.RandomNumberService.GetRandomNumbers \
        --rps $RPS \
        --duration $DURATION \
        --connections $connections \
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
    sleep 3
}

# Build Docker image once
echo -e "${BLUE}Building Docker image...${NC}"
docker build -f docker/Dockerfile.server -t grpc-server:latest . > /dev/null 2>&1
echo -e "${GREEN}✓ Docker image built${NC}"
echo ""

# Track progress
TOTAL_SCENARIOS=18
CURRENT_SCENARIO=0
START_TIME=$(date +%s)

# ═══════════════════════════════════════════════════════════════════════
# PART A: 50 THREAD OPTIMIZATION (8 scenarios)
# ═══════════════════════════════════════════════════════════════════════

echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}  PART A: 50 Thread Optimization (8 scenarios)              ${NC}"
echo -e "${CYAN}  Hypothesis: Less contention, smaller buffer sufficient    ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Phase A1: Buffer Sizing (3 tests)
echo -e "${BLUE}═══ Phase A1: Buffer Sizing (3 scenarios) ═══${NC}"
echo -e "${YELLOW}Fixed: 8 shards, 50 threads | Vary: Buffer size${NC}"
echo ""

PHASE="PhaseA1_BufferSizing_50T"
SHARDS=8
THREADS=50

for BUFFER in 32 64 128; do
    CURRENT_SCENARIO=$((CURRENT_SCENARIO + 1))
    ELAPSED=$(($(date +%s) - START_TIME))
    AVG_TIME=$((ELAPSED / CURRENT_SCENARIO))
    REMAINING=$((TOTAL_SCENARIOS - CURRENT_SCENARIO))
    ETA=$((AVG_TIME * REMAINING))
    
    echo -e "${YELLOW}Progress: ${CURRENT_SCENARIO}/${TOTAL_SCENARIOS} | Elapsed: ${ELAPSED}s | ETA: ${ETA}s${NC}"
    
    run_scenario "a1_${BUFFER}mb" "$PHASE" $BUFFER $SHARDS $THREADS \
        "50T: ${BUFFER}MB buffer, $SHARDS shards" "50_threads"
done

# Phase A2: Shard Optimization (4 tests)
echo -e "${BLUE}═══ Phase A2: Shard Optimization (4 scenarios) ═══${NC}"
echo -e "${YELLOW}Fixed: 64MB buffer, 50 threads | Vary: Shard count${NC}"
echo ""

PHASE="PhaseA2_ShardOpt_50T"
BUFFER=64
THREADS=50

for SHARDS in 2 4 8 16; do
    CURRENT_SCENARIO=$((CURRENT_SCENARIO + 1))
    ELAPSED=$(($(date +%s) - START_TIME))
    AVG_TIME=$((ELAPSED / CURRENT_SCENARIO))
    REMAINING=$((TOTAL_SCENARIOS - CURRENT_SCENARIO))
    ETA=$((AVG_TIME * REMAINING))
    
    echo -e "${YELLOW}Progress: ${CURRENT_SCENARIO}/${TOTAL_SCENARIOS} | Elapsed: ${ELAPSED}s | ETA: ${ETA}s${NC}"
    
    run_scenario "a2_${SHARDS}s" "$PHASE" $BUFFER $SHARDS $THREADS \
        "50T: ${BUFFER}MB buffer, $SHARDS shards" "50_threads"
done

# Phase A3: Validation (1 test)
echo -e "${BLUE}═══ Phase A3: Validation (1 scenario) ═══${NC}"
echo ""

PHASE="PhaseA3_Validation_50T"
BUFFER=64
SHARDS=4
THREADS=50

CURRENT_SCENARIO=$((CURRENT_SCENARIO + 1))
ELAPSED=$(($(date +%s) - START_TIME))
AVG_TIME=$((ELAPSED / CURRENT_SCENARIO))
REMAINING=$((TOTAL_SCENARIOS - CURRENT_SCENARIO))
ETA=$((AVG_TIME * REMAINING))

echo -e "${YELLOW}Progress: ${CURRENT_SCENARIO}/${TOTAL_SCENARIOS} | Elapsed: ${ELAPSED}s | ETA: ${ETA}s${NC}"

run_scenario "a3_val" "$PHASE" $BUFFER $SHARDS $THREADS \
    "50T: ${BUFFER}MB buffer, $SHARDS shards (validation)" "50_threads"

echo -e "${GREEN}✓✓✓ Part A Complete (50 threads)! ✓✓✓${NC}"
echo ""

# ═══════════════════════════════════════════════════════════════════════
# PART B: 200 THREAD OPTIMIZATION (10 scenarios)
# ═══════════════════════════════════════════════════════════════════════

echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}  PART B: 200 Thread Optimization (10 scenarios)            ${NC}"
echo -e "${CYAN}  Hypothesis: High contention, larger buffer needed         ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Phase B1: Buffer Sizing (4 tests)
echo -e "${BLUE}═══ Phase B1: Buffer Sizing (4 scenarios) ═══${NC}"
echo -e "${YELLOW}Fixed: 8 shards, 200 threads | Vary: Buffer size${NC}"
echo ""

PHASE="PhaseB1_BufferSizing_200T"
SHARDS=8
THREADS=200

for BUFFER in 64 128 256 512; do
    CURRENT_SCENARIO=$((CURRENT_SCENARIO + 1))
    ELAPSED=$(($(date +%s) - START_TIME))
    AVG_TIME=$((ELAPSED / CURRENT_SCENARIO))
    REMAINING=$((TOTAL_SCENARIOS - CURRENT_SCENARIO))
    ETA=$((AVG_TIME * REMAINING))
    
    echo -e "${YELLOW}Progress: ${CURRENT_SCENARIO}/${TOTAL_SCENARIOS} | Elapsed: ${ELAPSED}s | ETA: ${ETA}s${NC}"
    
    run_scenario "b1_${BUFFER}mb" "$PHASE" $BUFFER $SHARDS $THREADS \
        "200T: ${BUFFER}MB buffer, $SHARDS shards" "200_threads"
done

# Phase B2: Shard Optimization (4 tests)
echo -e "${BLUE}═══ Phase B2: Shard Optimization (4 scenarios) ═══${NC}"
echo -e "${YELLOW}Fixed: 256MB buffer, 200 threads | Vary: Shard count${NC}"
echo ""

PHASE="PhaseB2_ShardOpt_200T"
BUFFER=256
THREADS=200

for SHARDS in 4 8 16 32; do
    CURRENT_SCENARIO=$((CURRENT_SCENARIO + 1))
    ELAPSED=$(($(date +%s) - START_TIME))
    AVG_TIME=$((ELAPSED / CURRENT_SCENARIO))
    REMAINING=$((TOTAL_SCENARIOS - CURRENT_SCENARIO))
    ETA=$((AVG_TIME * REMAINING))
    
    echo -e "${YELLOW}Progress: ${CURRENT_SCENARIO}/${TOTAL_SCENARIOS} | Elapsed: ${ELAPSED}s | ETA: ${ETA}s${NC}"
    
    run_scenario "b2_${SHARDS}s" "$PHASE" $BUFFER $SHARDS $THREADS \
        "200T: ${BUFFER}MB buffer, $SHARDS shards" "200_threads"
done

# Phase B3: Cross-Validation (2 tests)
echo -e "${BLUE}═══ Phase B3: Cross-Validation (2 scenarios) ═══${NC}"
echo ""

PHASE="PhaseB3_Validation_200T"

# Test 1: 256MB, 16 shards
BUFFER=256
SHARDS=16
THREADS=200

CURRENT_SCENARIO=$((CURRENT_SCENARIO + 1))
ELAPSED=$(($(date +%s) - START_TIME))
AVG_TIME=$((ELAPSED / CURRENT_SCENARIO))
REMAINING=$((TOTAL_SCENARIOS - CURRENT_SCENARIO))
ETA=$((AVG_TIME * REMAINING))

echo -e "${YELLOW}Progress: ${CURRENT_SCENARIO}/${TOTAL_SCENARIOS} | Elapsed: ${ELAPSED}s | ETA: ${ETA}s${NC}"

run_scenario "b3_val1" "$PHASE" $BUFFER $SHARDS $THREADS \
    "200T: ${BUFFER}MB buffer, $SHARDS shards (validation 1)" "200_threads"

# Test 2: 512MB, 16 shards
BUFFER=512
SHARDS=16
THREADS=200

CURRENT_SCENARIO=$((CURRENT_SCENARIO + 1))
ELAPSED=$(($(date +%s) - START_TIME))

echo -e "${YELLOW}Progress: ${CURRENT_SCENARIO}/${TOTAL_SCENARIOS} | Elapsed: 0s | ETA: 0s${NC}"

run_scenario "b3_val2" "$PHASE" $BUFFER $SHARDS $THREADS \
    "200T: ${BUFFER}MB buffer, $SHARDS shards (validation 2)" "200_threads"

echo -e "${GREEN}✓✓✓ Part B Complete (200 threads)! ✓✓✓${NC}"
echo ""

# Final summary
TOTAL_TIME=$(($(date +%s) - START_TIME))
MINUTES=$((TOTAL_TIME / 60))
SECONDS=$((TOTAL_TIME % 60))

echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}       ✓✓✓ ALL 18 SCENARIOS COMPLETE! ✓✓✓                  ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${GREEN}Total runtime: ${MINUTES}m ${SECONDS}s${NC}"
echo -e "${GREEN}Results directory: $RESULTS_DIR${NC}"
echo ""
echo -e "${YELLOW}Next step: Process results${NC}"
echo -e "${BLUE}  go run scripts/process_thread_scaling.go${NC}"
echo ""

# Log completion
echo "Run completed at: $(date)" >> "$RUN_LOG"
echo "Total scenarios: $TOTAL_SCENARIOS" >> "$RUN_LOG"
echo "Total runtime: ${MINUTES}m ${SECONDS}s" >> "$RUN_LOG"

echo -e "${GREEN}Log file: $RUN_LOG${NC}"
echo ""

