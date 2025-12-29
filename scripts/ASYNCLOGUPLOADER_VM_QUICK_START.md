# Async Log Uploader - VM Quick Start Guide

## Prerequisites Verification

```bash
# Run verification script
bash scripts/verify_vm_prerequisites.sh
```

**Required:**
- ✓ Go 1.21+
- ✓ Linux OS
- ✓ 50GB+ disk space
- ✓ 4GB+ RAM
- ✓ `top` command (for resource monitoring)

**Optional:**
- ⚠ `iostat` (for detailed disk I/O - not critical, fallback available)
- ⚠ GCS: On GCP VMs, default service account credentials are used automatically

## Quick Test Commands

### 1. Baseline Test (Single Logger)

```bash
bash scripts/run_asyncloguploader_test_vm.sh \
    --duration 10m \
    --threads 100 \
    --rps 1000 \
    --buffer-mb 64 \
    --shards 8
```

### 2. Multi-Event Test

```bash
bash scripts/run_asyncloguploader_test_vm.sh \
    --duration 10m \
    --threads 100 \
    --rps 1000 \
    --buffer-mb 64 \
    --shards 8 \
    --use-events \
    --num-events 3
```

### 3. High Throughput Test

```bash
bash scripts/run_asyncloguploader_test_vm.sh \
    --duration 5m \
    --threads 200 \
    --rps 5000 \
    --buffer-mb 128 \
    --shards 16
```

### 4. File Rotation Test

```bash
bash scripts/run_asyncloguploader_test_vm.sh \
    --duration 10m \
    --threads 100 \
    --rps 1000 \
    --buffer-mb 64 \
    --shards 8 \
    --max-file-size-gb 1 \
    --preallocate-size-gb 1
```

### 5. With GCS Upload

```bash
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/credentials.json

bash scripts/run_asyncloguploader_test_vm.sh \
    --duration 10m \
    --threads 100 \
    --rps 1000 \
    --buffer-mb 64 \
    --shards 8 \
    --max-file-size-gb 1 \
    --gcs-bucket my-bucket \
    --gcs-prefix logs/test/
```

## Output Locations

- **Test Log:** `results/asyncloguploader_test/test_TIMESTAMP.log`
- **Resource Data:** `results/asyncloguploader_test/resource_timeline_TIMESTAMP.csv`
- **Log Files:** `logs/test_*.log` or `logs/eventN_*.log`

## Key Metrics

- **Drop Rate:** Should be < 0.1%
- **Flush Latency:** P95 should be < 50ms
- **Pwritev Duration:** Should be > 90% of flush time

## Troubleshooting

**High Drop Rate:**
- Increase `--buffer-mb`
- Decrease `--flush-interval`

**Flush Errors:**
- Check disk space: `df -h`
- Check permissions: `ls -ld logs/`

**GCS Upload Fails:**
- Verify credentials: `echo $GOOGLE_APPLICATION_CREDENTIALS`
- Check network: `ping storage.googleapis.com`

