#!/bin/bash

# Monitor retry test and report results when complete

RESULTS_DIR="results/retry_test"
CONTAINER_NAME="grpc-server-retry-test"
MAX_WAIT=600  # 10 minutes max wait
CHECK_INTERVAL=30  # Check every 30 seconds

echo "Monitoring test completion..."
echo "Test will run for 5 minutes once started"
echo ""

start_time=$(date +%s)
last_status=""

while true; do
    current_time=$(date +%s)
    elapsed=$((current_time - start_time))
    
    # Check if container is running
    container_status=$(docker ps --filter "name=$CONTAINER_NAME" --format "{{.Status}}" 2>/dev/null)
    
    # Check if ghz is running
    ghz_running=$(ps aux | grep -E "ghz.*5m" | grep -v grep | wc -l)
    
    # Check for test completion files
    latest_ghz=$(ls -t ${RESULTS_DIR}/ghz_report_*.json 2>/dev/null | head -1)
    latest_logs=$(ls -t ${RESULTS_DIR}/server_logs_*.txt 2>/dev/null | head -1)
    
    status=""
    if [ -n "$container_status" ]; then
        status="Container: Running ($container_status)"
    elif [ "$ghz_running" -gt 0 ]; then
        status="ghz: Running"
    elif [ -n "$latest_ghz" ] && [ -n "$latest_logs" ]; then
        # Check if files are recent (within last 2 minutes)
        file_age=$(($(date +%s) - $(stat -f %m "$latest_ghz" 2>/dev/null || echo 0)))
        if [ "$file_age" -lt 120 ]; then
            status="Test: COMPLETE"
        else
            status="Test: Old files found"
        fi
    else
        status="Build: In progress"
    fi
    
    # Only print if status changed
    if [ "$status" != "$last_status" ]; then
        echo "[$(date +%H:%M:%S)] $status (Elapsed: ${elapsed}s)"
        last_status="$status"
    fi
    
    # Check if test completed
    if [ "$status" = "Test: COMPLETE" ]; then
        echo ""
        echo "════════════════════════════════════════════════════════════"
        echo "Test Completed! Analyzing results..."
        echo "════════════════════════════════════════════════════════════"
        break
    fi
    
    # Timeout check
    if [ $elapsed -gt $MAX_WAIT ]; then
        echo ""
        echo "Timeout: Test taking longer than expected"
        break
    fi
    
    sleep $CHECK_INTERVAL
done

# Extract and display results
echo ""
echo "Final Results:"
echo ""

if [ -n "$latest_ghz" ]; then
    echo "gRPC Performance:"
    cat "$latest_ghz" | jq -r '
        "  Total Requests: \(.total),
  Successful: \(.statusCodeDistribution.OK // 0),
  Failed: \(.statusCodeDistribution."Unavailable" // 0),
  P50 Latency: \(.latencyDistribution.P50)ms,
  P95 Latency: \(.latencyDistribution.P95)ms,
  P99 Latency: \(.latencyDistribution.P99)ms,
  RPS: \(.rps),
  Duration: \(.duration)"
    ' 2>/dev/null || echo "  (Parsing ghz results...)"
    echo ""
fi

if [ -n "$latest_logs" ]; then
    echo "Logger Metrics:"
    grep "Logger Stats" "$latest_logs" | tail -1 | sed 's/.*Logger Stats - //' || echo "  (Extracting metrics...)"
    echo ""
    
    echo "Drop Rate Trend:"
    grep "METRICS:" "$latest_logs" | tail -5 | sed 's/.*Dropped: \([0-9]*\) (\([0-9.]*\)%).*/  \1 drops (\2%)/' || echo "  (Extracting drop rates...)"
    echo ""
fi

echo "Files:"
echo "  gRPC Report: $latest_ghz"
echo "  Server Logs: $latest_logs"

