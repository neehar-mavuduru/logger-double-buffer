#!/bin/bash

set -euo pipefail

# Colors
CYAN='\033[0;36m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

RESULTS_DIR="results/direct_logger_test"

if [ ! -d "$RESULTS_DIR" ]; then
    echo "Error: Results directory not found: $RESULTS_DIR"
    exit 1
fi

# Find latest test log
LATEST_LOG=$(ls -t "$RESULTS_DIR"/test_*.log 2>/dev/null | head -1)

if [ -z "$LATEST_LOG" ]; then
    echo "Error: No test logs found in $RESULTS_DIR"
    exit 1
fi

echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}        Direct Logger Test Results Analysis                ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""
echo "Analyzing: $(basename "$LATEST_LOG")"
echo ""

# Extract final metrics
FINAL_METRICS=$(grep "METRICS:" "$LATEST_LOG" | tail -1)

if [ -z "$FINAL_METRICS" ]; then
    echo "Error: No metrics found in log file"
    exit 1
fi

# Parse metrics
LOGS=$(echo "$FINAL_METRICS" | sed -n 's/.*Logs: \([0-9]*\).*/\1/p')
DROPPED=$(echo "$FINAL_METRICS" | sed -n 's/.*Dropped: \([0-9]*\).*/\1/p')
DROP_RATE=$(echo "$FINAL_METRICS" | sed -n 's/.*Dropped: [0-9]* (\([0-9.]*\)%).*/\1/p')
BYTES=$(echo "$FINAL_METRICS" | sed -n 's/.*Bytes: \([0-9]*\).*/\1/p')
FLUSHES=$(echo "$FINAL_METRICS" | sed -n 's/.*Flushes: \([0-9]*\).*/\1/p')
ERRORS=$(echo "$FINAL_METRICS" | sed -n 's/.*Errors: \([0-9]*\).*/\1/p')
SWAPS=$(echo "$FINAL_METRICS" | sed -n 's/.*Swaps: \([0-9]*\).*/\1/p')
AVG_FLUSH=$(echo "$FINAL_METRICS" | sed -n 's/.*AvgFlush: \([0-9.]*\)ms.*/\1/p')
MAX_FLUSH=$(echo "$FINAL_METRICS" | sed -n 's/.*MaxFlush: \([0-9.]*\)ms.*/\1/p')
GC_CYCLES=$(echo "$FINAL_METRICS" | sed -n 's/.*GC: \([0-9]*\) cycles.*/\1/p')
GC_PAUSE=$(echo "$FINAL_METRICS" | sed -n 's/.*GC: [0-9]* cycles \([0-9.]*\)ms.*/\1/p')
MEM=$(echo "$FINAL_METRICS" | sed -n 's/.*Mem: \([0-9.]*\)MB.*/\1/p')

# Display results
echo -e "${CYAN}📊 Overall Metrics:${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
printf "  Logs Written:     %'d\n" "$LOGS"
printf "  Logs Dropped:      %'d (%.4f%%)\n" "$DROPPED" "$DROP_RATE"
printf "  Bytes Written:     %'d (%.2f GB)\n" "$BYTES" "$(echo "scale=2; $BYTES/1024/1024/1024" | bc)"
printf "  Flushes:           %'d\n" "$FLUSHES"
printf "  Flush Errors:      %'d\n" "$ERRORS"
printf "  Set Swaps:         %'d\n" "$SWAPS"
echo ""
echo -e "${CYAN}⏱️  Flush Performance:${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
printf "  Avg Flush:         %.2f ms\n" "$AVG_FLUSH"
printf "  Max Flush:         %.2f ms\n" "$MAX_FLUSH"
echo ""
echo -e "${CYAN}💾 Resource Usage:${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
printf "  GC Cycles:         %d\n" "$GC_CYCLES"
printf "  GC Pause:         %.2f ms\n" "$GC_PAUSE"
printf "  Memory:            %.2f MB\n" "$MEM"
echo ""

# Check for flush errors
if [ "$ERRORS" -gt 0 ]; then
    echo -e "${YELLOW}⚠️  Flush Errors Detected:${NC}"
    grep "FLUSH_ERROR" "$LATEST_LOG" | tail -5
    echo ""
fi

# Analyze resource timeline if available
RESOURCE_FILE=$(ls -t "$RESULTS_DIR"/resource_timeline_*.csv 2>/dev/null | head -1)
if [ -n "$RESOURCE_FILE" ] && [ -f "$RESOURCE_FILE" ]; then
    echo -e "${CYAN}📈 Resource Timeline Summary:${NC}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    
    # Calculate averages
    AVG_CPU=$(awk -F',' 'NR>1 {sum+=$2; count++} END {if(count>0) printf "%.2f", sum/count; else print "0"}' "$RESOURCE_FILE")
    PEAK_CPU=$(awk -F',' 'NR>1 {if($2>max) max=$2} END {printf "%.2f", max+0}' "$RESOURCE_FILE")
    AVG_MEM=$(awk -F',' 'NR>1 {sum+=$3; count++} END {if(count>0) printf "%.2f", sum/count; else print "0"}' "$RESOURCE_FILE")
    PEAK_MEM=$(awk -F',' 'NR>1 {if($3>max) max=$3} END {printf "%.2f", max+0}' "$RESOURCE_FILE")
    
    printf "  Avg CPU:          %.2f%%\n" "$AVG_CPU"
    printf "  Peak CPU:          %.2f%%\n" "$PEAK_CPU"
    printf "  Avg Memory:        %.2f MB\n" "$AVG_MEM"
    printf "  Peak Memory:       %.2f MB\n" "$PEAK_MEM"
    echo ""
fi

# Performance assessment
echo -e "${CYAN}✅ Performance Assessment:${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if (( $(echo "$DROP_RATE < 0.1" | bc -l) )); then
    echo -e "  Drop Rate:         ${GREEN}✓ Excellent (<0.1%)${NC}"
elif (( $(echo "$DROP_RATE < 1.0" | bc -l) )); then
    echo -e "  Drop Rate:         ${GREEN}✓ Good (<1%)${NC}"
else
    echo -e "  Drop Rate:         ${YELLOW}⚠ High (>=1%)${NC}"
fi

if (( $(echo "$AVG_FLUSH < 50" | bc -l) )); then
    echo -e "  Avg Flush:         ${GREEN}✓ Excellent (<50ms)${NC}"
elif (( $(echo "$AVG_FLUSH < 100" | bc -l) )); then
    echo -e "  Avg Flush:         ${GREEN}✓ Good (<100ms)${NC}"
else
    echo -e "  Avg Flush:         ${YELLOW}⚠ Slow (>=100ms)${NC}"
fi

if [ "$ERRORS" -eq 0 ]; then
    echo -e "  Flush Errors:      ${GREEN}✓ None${NC}"
else
    echo -e "  Flush Errors:      ${YELLOW}⚠ $ERRORS errors${NC}"
fi

echo ""
echo -e "${CYAN}📁 Files:${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Test log:          $(basename "$LATEST_LOG")"
if [ -n "$RESOURCE_FILE" ]; then
    echo "  Resource timeline: $(basename "$RESOURCE_FILE")"
fi
echo ""

