#!/bin/bash
# Script to prepare repository for pushing to remote

set -e

echo "=== Preparing Repository for Remote Push ==="
echo ""

# Check if git is initialized
if [ ! -d .git ]; then
    echo "Initializing git repository..."
    git init
fi

# Add all files (respecting .gitignore)
echo "Staging files..."
git add .

# Show what will be committed
echo ""
echo "=== Files to be committed ==="
git status --short | head -30
echo "..."

# Count files
TOTAL_FILES=$(git ls-files | wc -l | tr -d ' ')
echo ""
echo "Total files to commit: $TOTAL_FILES"

# Show file breakdown
echo ""
echo "=== File Breakdown ==="
echo "Go files: $(git ls-files | grep '\.go$' | wc -l | tr -d ' ')"
echo "Proto files: $(git ls-files | grep '\.proto$' | wc -l | tr -d ' ')"
echo "Test files: $(git ls-files | grep '_test\.go$' | wc -l | tr -d ' ')"
echo "Scripts: $(git ls-files | grep '\.sh$' | wc -l | tr -d ' ')"
echo "Dockerfiles: $(git ls-files | grep -i dockerfile | wc -l | tr -d ' ')"
echo "Config files: $(git ls-files | grep -E 'go\.(mod|sum)|\.gitignore' | wc -l | tr -d ' ')"

echo ""
echo "=== Ready to commit ==="
echo ""
echo "To commit and push:"
echo "  1. git commit -m 'feat: Add event-based logging and flush metrics'"
echo "  2. git remote add origin <your-repo-url>"
echo "  3. git push -u origin main"
echo ""
echo "Or use the push script: bash scripts/push_to_remote.sh <repo-url>"







