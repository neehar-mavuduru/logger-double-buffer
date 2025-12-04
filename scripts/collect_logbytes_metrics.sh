#!/bin/bash

# LogBytes Performance Evaluation Script
# Runs 7 profiling scenarios and collects CPU, memory, and latency metrics

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}      LogBytes Performance Evaluation (7 Scenarios)         ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
RESULTS_DIR="results/logbytes"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
CONSOLIDATED_CSV="${RESULTS_DIR}/results_${TIMESTAMP}.csv"

# Create results directory
mkdir -p "$RESULTS_DIR"

# Test scenarios
# Format: threads,shards,buffer_kb
declare -a SCENARIOS=(
    "1,1,2048"   # Scenario 1: 1T, 1S, 2MB
    "2,1,2048"   # Scenario 2: 2T, 1S, 2MB
    "4,2,2048"   # Scenario 3: 4T, 2S, 2MB (1MB per shard)
    "4,1,2048"   # Scenario 4: 4T, 1S, 2MB
    "4,4,2048"   # Scenario 5: 4T, 4S, 2MB (512KB per shard)
    "16,4,2048"  # Scenario 6: 16T, 4S, 2MB (512KB per shard)
    "16,8,2048"  # Scenario 7: 16T, 8S, 2MB (256KB per shard)
)

# Write CSV header
echo "scenario,threads,shards,shard_size_kb,throughput_logs_sec,drop_rate_percent,log_latency_p50_ns,log_latency_p95_ns,log_latency_p99_ns,log_latency_mean_ns,alloc_mb,total_alloc_mb,sys_mb,num_gc,gc_pause_ms,cpu_percent,mem_usage_mb,mem_percent" > "$CONSOLIDATED_CSV"

echo -e "${GREEN}Results will be saved to: ${CONSOLIDATED_CSV}${NC}"
echo ""

# Change to project root
cd "$(dirname "$0")/.."

echo -e "${BLUE}Running all 7 scenarios...${NC}"
echo -e "${YELLOW}Estimated time: ~14 minutes${NC}"
echo ""

# Run the comprehensive profiling test
# The test will output CSV lines that we can capture
go test -v -timeout 20m ./asynclogger \
    -run TestRunAllScenarios \
    2>&1 | tee >(grep "^RESULT_CSV:" | sed 's/^RESULT_CSV://' >> "$CONSOLIDATED_CSV")

echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}✓ All scenarios completed!${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${GREEN}Results saved to: ${CONSOLIDATED_CSV}${NC}"
echo ""

# Generate report automatically
echo -e "${BLUE}Generating performance report...${NC}"
go run scripts/logbytes_processor/process_logbytes_results.go "${CONSOLIDATED_CSV}"

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Report generated: LOGBYTES_PERFORMANCE_REPORT.md${NC}"
    echo ""
    echo -e "${YELLOW}View the report:${NC}"
    echo "  cat LOGBYTES_PERFORMANCE_REPORT.md"
    echo "  or open it in your editor"
else
    echo -e "${RED}✗ Report generation failed${NC}"
    echo -e "${YELLOW}Manual command:${NC}"
    echo "  go run scripts/logbytes_processor/process_logbytes_results.go ${CONSOLIDATED_CSV}"
fi
echo ""

