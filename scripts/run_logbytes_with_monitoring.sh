#!/bin/bash

# Run LogBytes profiling with real-time resource monitoring
# This script orchestrates Docker container execution and resource monitoring

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  LogBytes Performance Evaluation with Resource Monitoring  ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

# Configuration
CONTAINER_NAME="logbytes-profiler"
RESULTS_DIR="results/logbytes"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESOURCE_TIMELINE="${RESULTS_DIR}/resource_timeline_${TIMESTAMP}.csv"
COMPOSE_FILE="docker/docker-compose-logbytes.yml"

# Create results directory
mkdir -p "$RESULTS_DIR"

echo -e "${GREEN}Configuration:${NC}"
echo "  - Container: $CONTAINER_NAME"
echo "  - Results directory: $RESULTS_DIR"
echo "  - Resource timeline: $RESOURCE_TIMELINE"
echo "  - Test duration: ~14 minutes (7 scenarios × 2 min each)"
echo ""

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}Error: Docker is not running${NC}"
    exit 1
fi

# Clean up any existing container
if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo -e "${YELLOW}Cleaning up existing container...${NC}"
    docker rm -f "$CONTAINER_NAME" > /dev/null 2>&1 || true
fi

echo -e "${BLUE}Starting Docker container...${NC}"

# Start container in detached mode
docker-compose -f "$COMPOSE_FILE" up -d

# Wait for container to be fully up
echo -e "${YELLOW}Waiting for container to start...${NC}"
sleep 5

# Verify container is running
if ! docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo -e "${RED}Error: Container failed to start${NC}"
    echo -e "${YELLOW}Check logs with: docker-compose -f $COMPOSE_FILE logs${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Container started${NC}"
echo ""

# Start resource monitoring in background
echo -e "${BLUE}Starting resource monitoring...${NC}"
bash scripts/monitor_docker_resources.sh "$CONTAINER_NAME" "$RESOURCE_TIMELINE" 900 &
MONITOR_PID=$!

echo -e "${GREEN}✓ Resource monitoring started (PID: $MONITOR_PID)${NC}"
echo ""

# Show container logs in real-time
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  Container Logs (Real-time)                                ${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

# Follow logs until container stops
docker logs -f "$CONTAINER_NAME" 2>&1 &
LOGS_PID=$!

# Wait for container to finish
docker wait "$CONTAINER_NAME" > /dev/null 2>&1

# Stop following logs
kill $LOGS_PID 2>/dev/null || true

echo ""
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}✓ Tests completed!${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

# Wait for monitoring to finish (give it a few seconds to write final data)
sleep 3
kill $MONITOR_PID 2>/dev/null || true
wait $MONITOR_PID 2>/dev/null || true

# Clean up container
echo -e "${YELLOW}Cleaning up container...${NC}"
docker-compose -f "$COMPOSE_FILE" down > /dev/null 2>&1

echo -e "${GREEN}✓ Container cleaned up${NC}"
echo ""

# Copy results from container (if any were created inside)
echo -e "${BLUE}Collecting results...${NC}"

# Check if results exist
if [ -f "$RESULTS_DIR/consolidated_results.csv" ] || ls "$RESULTS_DIR"/results_*.csv 1> /dev/null 2>&1; then
    echo -e "${GREEN}✓ Test results found${NC}"
    
    # Generate resource usage report
    if [ -f "$RESOURCE_TIMELINE" ]; then
        echo -e "${BLUE}Analyzing resource usage over time...${NC}"
        go run scripts/logbytes_processor/analyze_resource_timeline.go "$RESOURCE_TIMELINE"
    fi
else
    echo -e "${YELLOW}Warning: No results files found${NC}"
    echo -e "${YELLOW}Results directory contents:${NC}"
    ls -la "$RESULTS_DIR" || echo "  (directory is empty)"
fi

echo ""
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}✓ LogBytes Profiling Complete!${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

echo -e "${GREEN}Output Files:${NC}"
echo "  1. Test Results: $RESULTS_DIR/results_*.csv"
echo "  2. Resource Timeline: $RESOURCE_TIMELINE"
echo "  3. Performance Report: LOGBYTES_PERFORMANCE_REPORT.md"
echo ""

echo -e "${YELLOW}Next Steps:${NC}"
echo "  1. Review: cat LOGBYTES_PERFORMANCE_REPORT.md"
echo "  2. Analyze resource usage: $RESOURCE_TIMELINE"
echo "  3. Compare with baseline metrics"
echo ""

