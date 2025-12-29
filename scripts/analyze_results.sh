#!/bin/bash
# Script to analyze baseline test results

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

RESULTS_DIR="results/event_baseline_test"

if [ ! -d "$RESULTS_DIR" ]; then
    echo -e "${RED}✗ Results directory not found: $RESULTS_DIR${NC}"
    exit 1
fi

# Find latest test run
LATEST_SERVER_LOG=$(ls -t "$RESULTS_DIR"/server_*.log 2>/dev/null | head -1)
LATEST_TIMESTAMP=$(basename "$LATEST_SERVER_LOG" 2>/dev/null | grep -o '[0-9]\{8\}_[0-9]\{6\}' || echo "")

if [ -z "$LATEST_TIMESTAMP" ]; then
    echo -e "${RED}✗ No test results found in $RESULTS_DIR${NC}"
    exit 1
fi

echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}              Baseline Test Results Analysis               ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "Test run timestamp: ${YELLOW}$LATEST_TIMESTAMP${NC}"
echo ""

# Check if files exist
SERVER_LOG="$RESULTS_DIR/server_${LATEST_TIMESTAMP}.log"
GHZ_EVENT1="$RESULTS_DIR/ghz_event1_${LATEST_TIMESTAMP}.json"
GHZ_EVENT2="$RESULTS_DIR/ghz_event2_${LATEST_TIMESTAMP}.json"
GHZ_EVENT3="$RESULTS_DIR/ghz_event3_${LATEST_TIMESTAMP}.json"
RESOURCE_CSV="$RESULTS_DIR/resource_timeline_${LATEST_TIMESTAMP}.csv"
FLUSH_ERRORS="$RESULTS_DIR/flush_errors_${LATEST_TIMESTAMP}.log"

# 1. Server Metrics
echo -e "${BLUE}=== Server Metrics ===${NC}"
if [ -f "$SERVER_LOG" ]; then
    LAST_METRICS=$(grep "METRICS:" "$SERVER_LOG" | tail -1)
    if [ -n "$LAST_METRICS" ]; then
        echo "$LAST_METRICS" | sed 's/.*METRICS: //'
    else
        echo -e "${YELLOW}⚠ No metrics found in server log${NC}"
    fi
    
    echo ""
    LAST_EVENT_STATS=$(grep "EVENT_STATS:" "$SERVER_LOG" | tail -1)
    if [ -n "$LAST_EVENT_STATS" ]; then
        echo "Per-Event Stats:"
        echo "$LAST_EVENT_STATS" | sed 's/.*EVENT_STATS: //'
    fi
else
    echo -e "${RED}✗ Server log not found: $SERVER_LOG${NC}"
fi

echo ""

# 2. Flush Errors
echo -e "${BLUE}=== Flush Errors ===${NC}"
if [ -f "$FLUSH_ERRORS" ]; then
    ERROR_COUNT=$(grep -c "FLUSH_ERROR" "$SERVER_LOG" 2>/dev/null || echo "0")
    if [ "$ERROR_COUNT" -eq 0 ]; then
        echo -e "${GREEN}✓ No flush errors${NC}"
    else
        echo -e "${RED}✗ Found $ERROR_COUNT flush errors${NC}"
        echo "First 5 errors:"
        head -5 "$FLUSH_ERRORS"
    fi
else
    echo -e "${YELLOW}⚠ Flush errors log not found${NC}"
fi

echo ""

# 3. gRPC Load Test Results
echo -e "${BLUE}=== gRPC Load Test Results ===${NC}"
if command -v jq &> /dev/null; then
    for event_num in 1 2 3; do
        report_file="$RESULTS_DIR/ghz_event${event_num}_${LATEST_TIMESTAMP}.json"
        if [ -f "$report_file" ]; then
            echo ""
            echo -e "${CYAN}Event${event_num}:${NC}"
            RPS=$(jq -r '.rps' "$report_file" 2>/dev/null || echo "N/A")
            COUNT=$(jq -r '.count' "$report_file" 2>/dev/null || echo "N/A")
            AVG_LATENCY=$(jq -r '.average / 1000000' "$report_file" 2>/dev/null || echo "N/A")
            P99_LATENCY=$(jq -r '.details[0].latencyDistribution[] | select(.percentile == 99) | .latency / 1000000' "$report_file" 2>/dev/null || echo "N/A")
            ERRORS=$(jq -r '.errorCount' "$report_file" 2>/dev/null || echo "N/A")
            
            echo "  RPS: $RPS"
            echo "  Total Requests: $COUNT"
            echo "  Avg Latency: ${AVG_LATENCY}ms"
            echo "  P99 Latency: ${P99_LATENCY}ms"
            echo "  Errors: $ERRORS"
        else
            echo -e "${YELLOW}⚠ Event${event_num} report not found${NC}"
        fi
    done
else
    echo -e "${YELLOW}⚠ jq not installed - cannot parse JSON results${NC}"
    echo "Install with: sudo apt-get install -y jq"
fi

echo ""

# 4. Resource Usage Summary
echo -e "${BLUE}=== Resource Usage Summary ===${NC}"
if [ -f "$RESOURCE_CSV" ]; then
    # Skip header line
    DATA_LINES=$(tail -n +2 "$RESOURCE_CSV" | wc -l)
    if [ "$DATA_LINES" -gt 0 ]; then
        echo "Data points collected: $DATA_LINES"
        
        # Calculate averages using awk
        AVG_CPU=$(tail -n +2 "$RESOURCE_CSV" | awk -F',' '{sum+=$2; count++} END {if(count>0) printf "%.2f", sum/count; else print "0"}')
        AVG_MEM=$(tail -n +2 "$RESOURCE_CSV" | awk -F',' '{sum+=$3; count++} END {if(count>0) printf "%.2f", sum/count; else print "0"}')
        MAX_MEM=$(tail -n +2 "$RESOURCE_CSV" | awk -F',' '{if($3>max) max=$3} END {printf "%.2f", max}')
        AVG_DISK=$(tail -n +2 "$RESOURCE_CSV" | awk -F',' '{sum+=$5; count++} END {if(count>0) printf "%.2f", sum/count; else print "0"}')
        
        echo "  Avg CPU: ${AVG_CPU}%"
        echo "  Avg Memory: ${AVG_MEM}MB"
        echo "  Peak Memory: ${MAX_MEM}MB"
        echo "  Avg Disk Used: ${AVG_DISK}MB"
        echo ""
        echo "Full timeline: $RESOURCE_CSV"
    else
        echo -e "${YELLOW}⚠ No resource data collected${NC}"
    fi
else
    echo -e "${YELLOW}⚠ Resource timeline not found${NC}"
fi

echo ""

# 5. File Locations
echo -e "${BLUE}=== Result Files ===${NC}"
echo "Server log:        $SERVER_LOG"
echo "Event1 report:     $GHZ_EVENT1"
echo "Event2 report:     $GHZ_EVENT2"
echo "Event3 report:     $GHZ_EVENT3"
echo "Resource timeline: $RESOURCE_CSV"
echo "Flush errors:      $FLUSH_ERRORS"

echo ""
echo -e "${GREEN}✓ Analysis complete${NC}"







