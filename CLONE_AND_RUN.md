# Clone and Run Baseline Test on GCP VM

## Quick Start

### 1. Clone Repository

```bash
git clone <your-repo-url>
cd logger-double-buffer
```

### 2. Install Dependencies

```bash
bash scripts/setup_gcp_vm.sh
```

**Note:** After Docker installation, you may need to logout/login or run `newgrp docker` for permissions.

### 3. Run Baseline Test

```bash
bash scripts/run_event_baseline_test.sh
```

The test runs for 10 minutes and generates results in `results/event_baseline_test/`.

## Detailed Setup

### Prerequisites

- Ubuntu 22.04 LTS or Debian 11
- 4+ vCPUs, 15GB+ RAM
- 100GB+ disk space
- Internet connection

### Step-by-Step Installation

#### 1. Install Docker

```bash
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
sudo usermod -aG docker $USER
newgrp docker  # Or logout/login
```

#### 2. Install Go

```bash
wget https://go.dev/dl/go1.24.1.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.24.1.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
```

#### 3. Install Protocol Buffers

```bash
sudo apt-get update
sudo apt-get install -y protobuf-compiler
```

#### 4. Install ghz (gRPC Load Testing Tool)

```bash
go install github.com/bojand/ghz@latest
echo 'export PATH=$PATH:~/go/bin' >> ~/.bashrc
source ~/.bashrc
```

#### 5. Install jq (JSON Parser)

```bash
sudo apt-get install -y jq
```

### Verify Installation

```bash
docker --version
go version
protoc --version
ghz --version
jq --version
```

### Prepare Code

```bash
cd logger-double-buffer
go mod download
protoc --go_out=. --go-grpc_out=. proto/random_numbers.proto
```

### Run Test

```bash
chmod +x scripts/run_event_baseline_test.sh
bash scripts/run_event_baseline_test.sh
```

## Results

Results are saved in `results/event_baseline_test/`:

- `server_TIMESTAMP.log` - Server logs with metrics
- `ghz_event1_TIMESTAMP.json` - Event1 load test results
- `ghz_event2_TIMESTAMP.json` - Event2 load test results
- `ghz_event3_TIMESTAMP.json` - Event3 load test results
- `resource_timeline_TIMESTAMP.csv` - Resource usage timeline
- `flush_errors_TIMESTAMP.log` - Flush errors (if any)

## Download Results

From your local machine:

```bash
# Using gcloud
gcloud compute scp --recurse \
  vm-name:~/logger-double-buffer/results \
  ./gcp-test-results \
  --zone=your-zone

# Using SCP
scp -r user@vm-ip:~/logger-double-buffer/results ./gcp-test-results
```

## Troubleshooting

### Docker Permission Denied
```bash
sudo usermod -aG docker $USER
newgrp docker
```

### ghz Not Found
```bash
export PATH=$PATH:~/go/bin
# Or add to ~/.bashrc
```

### Port Already in Use
```bash
# Check port 8585
sudo netstat -tulpn | grep 8585
# Kill process or change SERVER_PORT in script
```

### Out of Disk Space
```bash
df -h
docker system prune -a
```

## Expected Performance

- **Total Logs**: ~598,000
- **Dropped**: 0 (0.00%)
- **Flushes**: ~3,200
- **Flush Errors**: 0
- **Avg Flush Duration**: ~47ms
- **Memory Usage**: ~600-800MB

