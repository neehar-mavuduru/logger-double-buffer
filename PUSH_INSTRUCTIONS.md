# Instructions for Pushing to Remote Repository

## Quick Push

### Option 1: Using the Push Script (Recommended)

```bash
bash scripts/push_to_remote.sh <your-repo-url>
```

**Example:**
```bash
bash scripts/push_to_remote.sh https://github.com/yourusername/logger-double-buffer.git
# or
bash scripts/push_to_remote.sh git@github.com:yourusername/logger-double-buffer.git
```

The script will:
1. Initialize git (if needed)
2. Add remote repository
3. Stage all files
4. Commit with default message
5. Push to remote

### Option 2: Manual Push

```bash
# 1. Prepare repository
bash scripts/prepare_for_push.sh

# 2. Add remote
git remote add origin <your-repo-url>

# 3. Commit
git commit -m "feat: Add event-based logging and flush metrics

- Add LoggerManager for event-based log separation
- Add flush duration metrics tracking
- Add timeout and recovery tests
- Remove UseMMap field (reverted to separate-shards)"

# 4. Push
git checkout -b main  # if not on main branch
git push -u origin main
```

## What Will Be Pushed

### Core Files (98 files total)

**asynclogger package:**
- `buffer.go` - Aligned buffer with CAS writes
- `buffer_set.go` - Sharded buffer set management
- `config.go` - Configuration
- `directio_default.go` - Fallback for non-Linux
- `directio_linux.go` - Linux Direct I/O
- `logger.go` - Main logger (with flush metrics)
- `logger_manager.go` - **NEW** Event-based logger manager
- `shard.go` - Individual shard
- Test files: `*_test.go` (5 files)

**Other essential files:**
- `go.mod`, `go.sum` - Go dependencies
- `proto/random_numbers.proto` - Protocol buffer definition
- `docker/Dockerfile.server` - Docker build file
- `scripts/run_event_baseline_test.sh` - Test script
- `test_data/` - Test data files (event1.json, event2.json, event3.json)
- `CLONE_AND_RUN.md` - Setup guide for GCP VM

### Excluded Files

The following are **NOT** pushed (excluded by `.gitignore`):
- `results/` directory (test results)
- `*.log` files (log files)
- `*.csv` files (CSV results)
- `bin/` directory (compiled binaries)
- Analysis/documentation `.md` files (except essential ones)
- Profiling data (`*.prof`)

## Clone on GCP VM

After pushing, clone on your GCP VM:

```bash
# Clone repository
git clone <your-repo-url>
cd logger-double-buffer

# Install dependencies
bash scripts/setup_gcp_vm.sh

# Run baseline test
bash scripts/run_event_baseline_test.sh
```

See `CLONE_AND_RUN.md` for detailed setup instructions.

## Verify Push

After pushing, verify files are on remote:

```bash
# Clone to a temporary location to verify
cd /tmp
git clone <your-repo-url> verify-repo
cd verify-repo
ls -la

# Check key files exist
test -f asynclogger/logger_manager.go && echo "✓ logger_manager.go exists"
test -f scripts/run_event_baseline_test.sh && echo "✓ Test script exists"
test -f test_data/event1.json && echo "✓ Test data exists"
```

## Troubleshooting

### Remote Already Exists

If remote already exists:
```bash
git remote set-url origin <new-repo-url>
```

### Authentication Issues

For GitHub:
- Use HTTPS with personal access token
- Or use SSH keys: `git@github.com:username/repo.git`

For GitLab:
- Use HTTPS with personal access token
- Or use SSH keys: `git@gitlab.com:username/repo.git`

### Large Files

If you encounter large file issues:
```bash
# Check file sizes
git ls-files | xargs ls -lh | sort -k5 -hr | head -10

# Remove large files if needed
git rm --cached <large-file>
```

## Next Steps

1. Push to remote repository
2. Clone on GCP VM
3. Run baseline test
4. Download results for analysis

