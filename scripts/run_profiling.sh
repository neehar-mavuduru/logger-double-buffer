#!/bin/bash

set -e

echo "=================================="
echo "Logger Profiling Suite"
echo "=================================="
echo ""

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
RESULTS_DIR="./profiling_results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_FILE="$RESULTS_DIR/profiling_results_$TIMESTAMP.txt"
CSV_FILE="$RESULTS_DIR/profiling_results_$TIMESTAMP.csv"
REPORT_FILE="PROFILING_STUDY_RESULTS.md"

# Create results directory
mkdir -p "$RESULTS_DIR"

# Check if running in Docker
if [ -f /.dockerenv ]; then
    echo -e "${BLUE}Running in Docker container${NC}"
    RUN_IN_DOCKER=true
else
    echo -e "${YELLOW}Running on host machine${NC}"
    RUN_IN_DOCKER=false
fi

# Display system info
echo ""
echo "System Information:"
echo "==================="
if [ "$RUN_IN_DOCKER" = true ]; then
    echo "Platform: Docker Container (Linux)"
    echo "CPU Limit: 4 cores"
    echo "Memory Limit: 4GB"
else
    echo "Platform: $(uname -s) $(uname -m)"
    echo "CPU Cores: $(getconf _NPROCESSORS_ONLN 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 'unknown')"
    echo "Memory: $(free -h 2>/dev/null | awk '/^Mem:/ {print $2}' || sysctl -n hw.memsize 2>/dev/null | awk '{print $1/1024/1024/1024 " GB"}' || echo 'unknown')"
fi
echo "Go Version: $(go version)"
echo ""

# Build the profiling test
echo -e "${BLUE}Building profiling test...${NC}"
cd logger
go test -c -o ../profiling_test -bench=. -run=^$
cd ..
echo -e "${GREEN}✓ Build complete${NC}"
echo ""

# Initialize CSV file
echo "ScenarioName,Threads,Shards,BufferMB,Duration,TotalLogs,DroppedLogs,DropRate,Throughput,Flushes,BytesWritten,MemAllocMB,NumGC" > "$CSV_FILE"

# Function to run a single test scenario
run_scenario() {
    local name=$1
    local threads=$2
    local shards=$3
    local buffer_mb=$4
    
    echo -e "${YELLOW}Running: $name${NC}"
    echo "  Config: Threads=$threads, Shards=$shards, Buffer=${buffer_mb}MB"
    echo "  Duration: 2 minutes"
    
    # Run the test and capture output
    ./profiling_test -test.bench=. -test.run=^$ -test.benchtime=120s \
        -threads=$threads -shards=$shards -buffer=$((buffer_mb * 1024 * 1024)) \
        2>&1 | tee -a "$RESULTS_FILE"
    
    echo -e "${GREEN}✓ Complete${NC}"
    echo ""
}

# Get total scenario count
echo -e "${BLUE}Calculating test scenarios...${NC}"
SCENARIO_COUNT=$(go run scripts/count_scenarios.go)
echo "Total scenarios: $SCENARIO_COUNT"
ESTIMATED_MINUTES=$((SCENARIO_COUNT * 2))
echo "Estimated runtime: ~$ESTIMATED_MINUTES minutes"
echo ""

# Ask for confirmation
if [ "$RUN_IN_DOCKER" = false ]; then
    read -p "Continue with profiling? (y/n) " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Profiling cancelled."
        exit 0
    fi
fi

# Start profiling
START_TIME=$(date +%s)
echo -e "${GREEN}Starting profiling suite...${NC}"
echo "Results will be saved to: $RESULTS_FILE"
echo ""

# Run all scenarios using the Go test runner
echo "Running all profiling scenarios..."
./profiling_test -test.bench=BenchmarkProfilingScenario -test.run=^$ -test.benchtime=1x 2>&1 | tee -a "$RESULTS_FILE"

END_TIME=$(date +%s)
ELAPSED=$((END_TIME - START_TIME))
ELAPSED_MIN=$((ELAPSED / 60))
ELAPSED_SEC=$((ELAPSED % 60))

echo ""
echo -e "${GREEN}=================================="
echo "Profiling Complete!"
echo "==================================${NC}"
echo "Total time: ${ELAPSED_MIN}m ${ELAPSED_SEC}s"
echo "Results saved to: $RESULTS_FILE"
echo "CSV saved to: $CSV_FILE"
echo ""

# Generate report
echo -e "${BLUE}Generating analysis report...${NC}"
if command -v go &> /dev/null; then
    go run scripts/process_results.go "$RESULTS_FILE" "$CSV_FILE" "$REPORT_FILE"
    echo -e "${GREEN}✓ Report generated: $REPORT_FILE${NC}"
else
    echo -e "${YELLOW}⚠ Go not found. Skipping report generation.${NC}"
fi

echo ""
echo "Done!"

