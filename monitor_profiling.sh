#!/bin/bash

# Monitor the profiling run in real-time

LOG_FILE="profiling_results/run_20251113_225522.log"

echo "=================================="
echo "Profiling Monitor"
echo "=================================="
echo ""
echo "Log file: $LOG_FILE"
echo "Press Ctrl+C to stop monitoring (profiling will continue)"
echo ""

# Check if profiling is still running
if pgrep -f "run_all_scenarios.go" > /dev/null; then
    echo "✓ Profiling is running"
else
    echo "⚠ Profiling process not found (may have completed or failed)"
fi

echo ""
echo "Latest results:"
echo "-----------------------------------"

# Follow the log file
tail -f "$LOG_FILE"

