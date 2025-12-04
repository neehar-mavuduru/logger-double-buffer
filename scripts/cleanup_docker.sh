#!/bin/bash

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}       Docker Storage Cleanup Utility                       ${NC}"
echo -e "${CYAN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Show current Docker storage usage
echo -e "${BLUE}Current Docker storage usage:${NC}"
echo ""
docker system df
echo ""

# Show detailed breakdown
echo -e "${BLUE}Detailed breakdown:${NC}"
echo ""
docker system df -v | head -20
echo ""

# Calculate total reclaimable space
RECLAIMABLE=$(docker system df | grep -E "Images|Containers|Local Volumes|Build Cache" | awk '{sum += $4} END {print sum}')
echo -e "${YELLOW}Potentially reclaimable space: ${RECLAIMABLE}GB${NC}"
echo ""

# Ask for confirmation
echo -e "${YELLOW}This will remove:${NC}"
echo "  - All stopped containers"
echo "  - All dangling images (untagged)"
echo "  - All unused networks"
echo "  - All build cache"
echo ""
read -p "$(echo -e ${YELLOW}Do you want to proceed? [y/N]:${NC} )" -n 1 -r
echo ""

if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo -e "${RED}Cleanup cancelled${NC}"
    exit 0
fi

echo ""
echo -e "${BLUE}Performing cleanup...${NC}"
echo ""

# Stop any running test containers
echo -e "${BLUE}1. Stopping test containers...${NC}"
docker stop cliff-investigation 2>/dev/null || true
docker stop profile-test 2>/dev/null || true
docker stop baseline-test 2>/dev/null || true
docker ps --filter "name=logger-" -q | xargs -r docker stop 2>/dev/null || true
echo -e "${GREEN}✓ Test containers stopped${NC}"
echo ""

# Remove stopped containers
echo -e "${BLUE}2. Removing stopped containers...${NC}"
REMOVED_CONTAINERS=$(docker container prune -f 2>&1 | grep "Total reclaimed space" || echo "0B")
echo -e "${GREEN}✓ $REMOVED_CONTAINERS${NC}"
echo ""

# Remove dangling images
echo -e "${BLUE}3. Removing dangling images...${NC}"
REMOVED_IMAGES=$(docker image prune -f 2>&1 | grep "Total reclaimed space" || echo "0B")
echo -e "${GREEN}✓ $REMOVED_IMAGES${NC}"
echo ""

# Remove unused networks
echo -e "${BLUE}4. Removing unused networks...${NC}"
REMOVED_NETWORKS=$(docker network prune -f 2>&1 | grep "Total reclaimed space" || echo "0B")
echo -e "${GREEN}✓ $REMOVED_NETWORKS${NC}"
echo ""

# Remove build cache
echo -e "${BLUE}5. Removing build cache...${NC}"
REMOVED_CACHE=$(docker builder prune -f 2>&1 | grep "Total reclaimed space" || echo "0B")
echo -e "${GREEN}✓ $REMOVED_CACHE${NC}"
echo ""

# Optional: Remove unused volumes (commented out for safety)
# echo -e "${BLUE}6. Removing unused volumes...${NC}"
# REMOVED_VOLUMES=$(docker volume prune -f 2>&1 | grep "Total reclaimed space" || echo "0B")
# echo -e "${GREEN}✓ $REMOVED_VOLUMES${NC}"
# echo ""

echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}       ✓✓✓ Cleanup Complete! ✓✓✓                          ${NC}"
echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"
echo ""

# Show new storage usage
echo -e "${BLUE}Updated Docker storage usage:${NC}"
echo ""
docker system df
echo ""

echo -e "${CYAN}Tips:${NC}"
echo "  - Run this script before major test runs"
echo "  - For aggressive cleanup, use: docker system prune -a --volumes"
echo "  - Monitor with: docker system df"
echo ""

