#!/bin/bash

set -e

echo "=================================="
echo "Logger Profiling Suite (Docker)"
echo "=================================="
echo ""

# Configuration
RESULTS_DIR="/app/profiling_results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
RESULTS_FILE="$RESULTS_DIR/profiling_results_$TIMESTAMP.txt"

# Create results directory
mkdir -p "$RESULTS_DIR"

# Display system info
echo "Docker Container Information:"
echo "=============================="
echo "CPU Limit: 4 cores"
echo "Memory Limit: 4GB"
echo "Go Version: $(go version)"
echo ""

# Run the profiling
echo "Starting profiling..."
go run /app/scripts/run_all_scenarios.go | tee "$RESULTS_FILE"

echo ""
echo "Profiling complete!"
echo "Results saved to: $RESULTS_FILE"

