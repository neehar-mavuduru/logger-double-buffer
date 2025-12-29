#!/bin/bash

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}    VM Prerequisites Verification for Async Log Uploader   ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

ERRORS=0
WARNINGS=0

# Check Go version
echo -e "${CYAN}Checking Go installation...${NC}"
if command -v go >/dev/null 2>&1; then
    GO_VERSION=$(go version | awk '{print $3}')
    GO_MAJOR=$(echo "$GO_VERSION" | sed 's/go\([0-9]*\).*/\1/')
    if [ "$GO_MAJOR" -ge 1 ]; then
        GO_MINOR=$(echo "$GO_VERSION" | sed 's/go[0-9]*\.\([0-9]*\).*/\1/')
        if [ "$GO_MINOR" -ge 21 ]; then
            echo -e "${GREEN}✓ Go version: $GO_VERSION${NC}"
        else
            echo -e "${RED}✗ Go version too old: $GO_VERSION (requires 1.21+)${NC}"
            ERRORS=$((ERRORS + 1))
        fi
    else
        echo -e "${RED}✗ Invalid Go version: $GO_VERSION${NC}"
        ERRORS=$((ERRORS + 1))
    fi
else
    echo -e "${RED}✗ Go not found. Please install Go 1.21 or later.${NC}"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# Check OS
echo -e "${CYAN}Checking operating system...${NC}"
OS=$(uname -s)
if [ "$OS" = "Linux" ]; then
    echo -e "${GREEN}✓ OS: Linux${NC}"
    KERNEL=$(uname -r)
    echo "  Kernel: $KERNEL"
else
    echo -e "${YELLOW}⚠ OS: $OS (Direct I/O requires Linux, will use fallback)${NC}"
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# Check filesystem type (for Direct I/O)
if [ "$OS" = "Linux" ]; then
    echo -e "${CYAN}Checking filesystem type...${NC}"
    FS_TYPE=$(df -T . | tail -1 | awk '{print $2}')
    if [ "$FS_TYPE" = "ext4" ] || [ "$FS_TYPE" = "xfs" ]; then
        echo -e "${GREEN}✓ Filesystem: $FS_TYPE (supports O_DIRECT)${NC}"
    else
        echo -e "${YELLOW}⚠ Filesystem: $FS_TYPE (may not support O_DIRECT, will use fallback)${NC}"
        WARNINGS=$((WARNINGS + 1))
    fi
    echo ""
fi

# Check disk space
echo -e "${CYAN}Checking disk space...${NC}"
AVAILABLE=$(df -BG . | tail -1 | awk '{print $4}' | sed 's/G//')
if [ "$AVAILABLE" -ge 50 ]; then
    echo -e "${GREEN}✓ Available disk space: ${AVAILABLE}GB${NC}"
else
    echo -e "${YELLOW}⚠ Available disk space: ${AVAILABLE}GB (recommend at least 50GB)${NC}"
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# Check memory
echo -e "${CYAN}Checking memory...${NC}"
if [ "$OS" = "Linux" ]; then
    TOTAL_MEM=$(free -g | awk '/^Mem:/ {print $2}')
    if [ "$TOTAL_MEM" -ge 4 ]; then
        echo -e "${GREEN}✓ Total memory: ${TOTAL_MEM}GB${NC}"
    else
        echo -e "${YELLOW}⚠ Total memory: ${TOTAL_MEM}GB (recommend at least 4GB)${NC}"
        WARNINGS=$((WARNINGS + 1))
    fi
fi
echo ""

# Check CPU cores
echo -e "${CYAN}Checking CPU cores...${NC}"
if [ "$OS" = "Linux" ]; then
    CPU_CORES=$(nproc)
    if [ "$CPU_CORES" -ge 2 ]; then
        echo -e "${GREEN}✓ CPU cores: $CPU_CORES${NC}"
    else
        echo -e "${YELLOW}⚠ CPU cores: $CPU_CORES (recommend at least 2)${NC}"
        WARNINGS=$((WARNINGS + 1))
    fi
fi
echo ""

# Check required commands
echo -e "${CYAN}Checking required commands...${NC}"
REQUIRED_COMMANDS=("go" "bash")
for cmd in "${REQUIRED_COMMANDS[@]}"; do
    if command -v "$cmd" >/dev/null 2>&1; then
        echo -e "${GREEN}✓ $cmd found${NC}"
    else
        echo -e "${RED}✗ $cmd not found${NC}"
        ERRORS=$((ERRORS + 1))
    fi
done
echo ""

# Check optional commands
echo -e "${CYAN}Checking optional commands...${NC}"
OPTIONAL_COMMANDS=("iostat" "top")
for cmd in "${OPTIONAL_COMMANDS[@]}"; do
    if command -v "$cmd" >/dev/null 2>&1; then
        echo -e "${GREEN}✓ $cmd found (for resource monitoring)${NC}"
    else
        echo -e "${YELLOW}⚠ $cmd not found (resource monitoring may be limited)${NC}"
        WARNINGS=$((WARNINGS + 1))
    fi
done
echo ""

# Check GCS credentials (if GCS upload will be used)
echo -e "${CYAN}Checking GCS credentials (optional)...${NC}"
if [ -n "${GOOGLE_APPLICATION_CREDENTIALS:-}" ]; then
    if [ -f "$GOOGLE_APPLICATION_CREDENTIALS" ]; then
        echo -e "${GREEN}✓ GCS credentials file found: $GOOGLE_APPLICATION_CREDENTIALS${NC}"
    else
        echo -e "${YELLOW}⚠ GCS credentials file not found: $GOOGLE_APPLICATION_CREDENTIALS${NC}"
        WARNINGS=$((WARNINGS + 1))
    fi
elif [ -n "${GOOGLE_CLOUD_PROJECT:-}" ]; then
    echo -e "${GREEN}✓ GCS project set: $GOOGLE_CLOUD_PROJECT${NC}"
else
    echo -e "${YELLOW}⚠ GCS credentials not configured (GCS upload will be disabled)${NC}"
fi
echo ""

# Summary
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
if [ $ERRORS -eq 0 ] && [ $WARNINGS -eq 0 ]; then
    echo -e "${GREEN}✓ All checks passed!${NC}"
    exit 0
elif [ $ERRORS -eq 0 ]; then
    echo -e "${YELLOW}⚠ Checks completed with $WARNINGS warning(s)${NC}"
    echo -e "${YELLOW}  You can proceed, but some features may be limited.${NC}"
    exit 0
else
    echo -e "${RED}✗ Checks failed with $ERRORS error(s) and $WARNINGS warning(s)${NC}"
    echo -e "${RED}  Please fix the errors before running tests.${NC}"
    exit 1
fi

