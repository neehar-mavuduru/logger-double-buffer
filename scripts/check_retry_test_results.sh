#!/bin/bash

# Quick script to check retry test results

RESULTS_DIR="results/retry_test"
LATEST_GHZ=$(ls -t ${RESULTS_DIR}/ghz_report_*.json 2>/dev/null | head -1)
LATEST_RESOURCES=$(ls -t ${RESULTS_DIR}/resources_*.csv 2>/dev/null | head -1)
LATEST_LOGS=$(ls -t ${RESULTS_DIR}/server_logs_*.txt 2>/dev/null | head -1)

echo "════════════════════════════════════════════════════════════"
echo "Retry Test Results Summary"
echo "════════════════════════════════════════════════════════════"
echo ""

if [ -f "$LATEST_GHZ" ]; then
    echo "gRPC Performance (ghz):"
    cat "$LATEST_GHZ" | jq -r '
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

if [ -f "$LATEST_RESOURCES" ]; then
    echo "Resource Usage:"
    awk -F',' 'NR>1 {
        cpu+=$2; mem+=$3; count++
        if (NR==2) {min_cpu=$2; max_cpu=$2; min_mem=$3; max_mem=$3}
        if ($2<min_cpu) min_cpu=$2
        if ($2>max_cpu) max_cpu=$2
        if ($3<min_mem) min_mem=$3
        if ($3>max_mem) max_mem=$3
    } END {
        if (count>0) {
            print "  Samples: " count
            print "  Avg CPU: " cpu/count "%"
            print "  Min CPU: " min_cpu "%"
            print "  Max CPU: " max_cpu "%"
            print "  Avg Memory: " mem/count " MB"
            print "  Min Memory: " min_mem " MB"
            print "  Max Memory: " max_mem " MB"
        }
    }' "$LATEST_RESOURCES" 2>/dev/null || echo "  (Processing resource data...)"
    echo ""
fi

if [ -f "$LATEST_LOGS" ]; then
    echo "Logger Metrics (from server logs):"
    grep "METRICS:" "$LATEST_LOGS" | tail -1 | sed 's/.*METRICS: //' || echo "  (Extracting metrics...)"
    echo ""
    echo "Drop Rate Trend:"
    grep "METRICS:" "$LATEST_LOGS" | tail -5 | sed 's/.*Dropped: \([0-9]*\) (\([0-9.]*\)%).*/  \1 drops (\2%)/' || echo "  (Extracting drop rates...)"
    echo ""
fi

echo "Files:"
echo "  gRPC Report: $LATEST_GHZ"
echo "  Resources: $LATEST_RESOURCES"
echo "  Server Logs: $LATEST_LOGS"

