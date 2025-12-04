#!/bin/bash

# Monitor Docker container resource usage over time
# Usage: ./monitor_docker_resources.sh <container_name> <output_file> <duration_seconds>

CONTAINER_NAME=${1:-logbytes-profiler}
OUTPUT_FILE=${2:-results/logbytes/resource_timeline.csv}
DURATION=${3:-900}  # Default 15 minutes (covers all 7 scenarios)

echo "Monitoring container: $CONTAINER_NAME"
echo "Output file: $OUTPUT_FILE"
echo "Duration: ${DURATION}s"
echo ""

# Create output directory
mkdir -p "$(dirname "$OUTPUT_FILE")"

# Write CSV header
echo "timestamp,elapsed_seconds,cpu_percent,mem_usage_mb,mem_percent,mem_limit_mb,net_rx_mb,net_tx_mb,block_read_mb,block_write_mb" > "$OUTPUT_FILE"

# Start time
START_TIME=$(date +%s)
ELAPSED=0

echo "Starting resource monitoring..."

# Monitor loop
while [ $ELAPSED -lt $DURATION ]; do
    # Check if container is still running
    if ! docker ps --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
        echo "Container stopped, ending monitoring"
        break
    fi
    
    # Get current timestamp
    TIMESTAMP=$(date +"%Y-%m-%d %H:%M:%S")
    
    # Get Docker stats (single sample)
    STATS=$(docker stats --no-stream --format "{{.CPUPerc}},{{.MemUsage}},{{.MemPerc}},{{.NetIO}},{{.BlockIO}}" "$CONTAINER_NAME" 2>/dev/null)
    
    if [ -n "$STATS" ]; then
        # Parse stats
        CPU=$(echo "$STATS" | cut -d',' -f1 | sed 's/%//g')
        MEM_USAGE=$(echo "$STATS" | cut -d',' -f2)
        MEM_PERCENT=$(echo "$STATS" | cut -d',' -f3 | sed 's/%//g')
        NET_IO=$(echo "$STATS" | cut -d',' -f4)
        BLOCK_IO=$(echo "$STATS" | cut -d',' -f5)
        
        # Parse memory usage (e.g., "123.4MiB / 4GiB")
        MEM_USED=$(echo "$MEM_USAGE" | awk '{print $1}' | sed 's/MiB//g; s/GiB//g')
        MEM_LIMIT=$(echo "$MEM_USAGE" | awk '{print $3}' | sed 's/MiB//g; s/GiB//g')
        
        # Convert GiB to MiB if needed
        if echo "$MEM_USAGE" | grep -q "GiB"; then
            if echo "$MEM_USAGE" | grep -q "/ .*GiB"; then
                MEM_LIMIT=$(echo "$MEM_LIMIT * 1024" | bc)
            fi
            if echo "$MEM_USAGE" | awk '{print $1}' | grep -q "GiB"; then
                MEM_USED=$(echo "$MEM_USED * 1024" | bc)
            fi
        fi
        
        # Parse network I/O (e.g., "1.23MB / 4.56MB")
        NET_RX=$(echo "$NET_IO" | awk '{print $1}' | sed 's/MB//g; s/kB//g; s/GB//g')
        NET_TX=$(echo "$NET_IO" | awk '{print $3}' | sed 's/MB//g; s/kB//g; s/GB//g')
        
        # Convert to MB if needed
        if echo "$NET_IO" | grep -q "kB"; then
            NET_RX=$(echo "$NET_RX / 1024" | bc -l | xargs printf "%.3f")
            NET_TX=$(echo "$NET_TX / 1024" | bc -l | xargs printf "%.3f")
        elif echo "$NET_IO" | grep -q "GB"; then
            NET_RX=$(echo "$NET_RX * 1024" | bc -l | xargs printf "%.3f")
            NET_TX=$(echo "$NET_TX * 1024" | bc -l | xargs printf "%.3f")
        fi
        
        # Parse block I/O (e.g., "12.3MB / 45.6MB")
        BLOCK_READ=$(echo "$BLOCK_IO" | awk '{print $1}' | sed 's/MB//g; s/kB//g; s/GB//g')
        BLOCK_WRITE=$(echo "$BLOCK_IO" | awk '{print $3}' | sed 's/MB//g; s/kB//g; s/GB//g')
        
        # Convert to MB if needed
        if echo "$BLOCK_IO" | grep -q "kB"; then
            BLOCK_READ=$(echo "$BLOCK_READ / 1024" | bc -l | xargs printf "%.3f")
            BLOCK_WRITE=$(echo "$BLOCK_WRITE / 1024" | bc -l | xargs printf "%.3f")
        elif echo "$BLOCK_IO" | grep -q "GB"; then
            BLOCK_READ=$(echo "$BLOCK_READ * 1024" | bc -l | xargs printf "%.3f")
            BLOCK_WRITE=$(echo "$BLOCK_WRITE * 1024" | bc -l | xargs printf "%.3f")
        fi
        
        # Write to CSV
        echo "$TIMESTAMP,$ELAPSED,$CPU,$MEM_USED,$MEM_PERCENT,$MEM_LIMIT,$NET_RX,$NET_TX,$BLOCK_READ,$BLOCK_WRITE" >> "$OUTPUT_FILE"
    fi
    
    # Sleep for 1 second
    sleep 1
    
    # Calculate elapsed time
    CURRENT_TIME=$(date +%s)
    ELAPSED=$((CURRENT_TIME - START_TIME))
    
    # Progress indicator
    if [ $((ELAPSED % 30)) -eq 0 ]; then
        echo "Monitoring... ${ELAPSED}s elapsed (CPU: ${CPU}%, Mem: ${MEM_USED}MB)"
    fi
done

echo ""
echo "Resource monitoring complete!"
echo "Timeline data saved to: $OUTPUT_FILE"
echo "Total samples collected: $(wc -l < "$OUTPUT_FILE" | xargs)"

