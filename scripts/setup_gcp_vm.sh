#!/bin/bash
# Setup script for GCP VM - Installs all dependencies needed to run baseline test

set -e

echo "=== GCP VM Setup Script ==="
echo "This script installs Go, protoc, ghz, and jq"
echo "Note: Server runs directly on VM (no Docker required)"
echo ""

# Check if running as root
if [ "$EUID" -eq 0 ]; then 
   echo "Please do not run as root"
   exit 1
fi

# Update system
echo "Updating system packages..."
sudo apt-get update

# Install Go
if ! command -v go &> /dev/null; then
    echo ""
    echo "Installing Go 1.24.1..."
    wget -q https://go.dev/dl/go1.24.1.linux-amd64.tar.gz
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf go1.24.1.linux-amd64.tar.gz
    rm go1.24.1.linux-amd64.tar.gz
    
    # Add to PATH
    if ! grep -q "/usr/local/go/bin" ~/.bashrc; then
        echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
    fi
    export PATH=$PATH:/usr/local/go/bin
    echo "✓ Go installed"
else
    echo "✓ Go already installed: $(go version)"
fi

# Install Protocol Buffers compiler
if ! command -v protoc &> /dev/null; then
    echo ""
    echo "Installing Protocol Buffers compiler..."
    sudo apt-get install -y protobuf-compiler
    echo "✓ protoc installed"
else
    echo "✓ protoc already installed: $(protoc --version)"
fi

# Install ghz (gRPC load testing tool)
if ! command -v ghz &> /dev/null; then
    echo ""
    echo "Installing ghz (gRPC load testing tool)..."
    go install github.com/bojand/ghz@latest
    
    # Add go bin to PATH
    if ! grep -q "\$HOME/go/bin" ~/.bashrc; then
        echo 'export PATH=$PATH:$HOME/go/bin' >> ~/.bashrc
    fi
    export PATH=$PATH:$HOME/go/bin
    echo "✓ ghz installed"
else
    echo "✓ ghz already installed: $(ghz --version)"
fi

# Install jq (JSON parser)
if ! command -v jq &> /dev/null; then
    echo ""
    echo "Installing jq (JSON parser)..."
    sudo apt-get install -y jq
    echo "✓ jq installed"
else
    echo "✓ jq already installed: $(jq --version)"
fi

# Install coreutils (for numfmt)
if ! command -v numfmt &> /dev/null; then
    echo ""
    echo "Installing coreutils..."
    sudo apt-get install -y coreutils
    echo "✓ coreutils installed"
else
    echo "✓ coreutils already installed"
fi

echo ""
echo "=== Verification ==="
echo "Go: $(go version 2>/dev/null || echo 'Not found')"
echo "protoc: $(protoc --version 2>/dev/null || echo 'Not found')"
echo "ghz: $(ghz --version 2>/dev/null || echo 'Not found - check ~/go/bin')"
echo "jq: $(jq --version 2>/dev/null || echo 'Not found')"

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Next steps:"
echo "  1. If ghz not found, run: export PATH=\$PATH:\$HOME/go/bin"
echo "  2. Install Go dependencies: go mod download"
echo "  3. Regenerate proto files: protoc --go_out=. --go-grpc_out=. proto/random_numbers.proto"
echo "  4. Build server: go build -o bin/server ./server"
echo "  5. Run baseline test: bash scripts/run_event_baseline_test.sh"
echo ""

