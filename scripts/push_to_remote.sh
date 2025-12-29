#!/bin/bash
# Script to push code to remote repository

set -e

if [ -z "$1" ]; then
    echo "Usage: $0 <remote-repo-url>"
    echo "Example: $0 https://github.com/username/logger-double-buffer.git"
    echo "Example: $0 git@github.com:username/logger-double-buffer.git"
    exit 1
fi

REMOTE_URL="$1"

echo "=== Pushing to Remote Repository ==="
echo "Remote URL: $REMOTE_URL"
echo ""

# Check if git is initialized
if [ ! -d .git ]; then
    echo "Initializing git repository..."
    git init
fi

# Check if remote already exists
if git remote get-url origin >/dev/null 2>&1; then
    echo "Remote 'origin' already exists: $(git remote get-url origin)"
    read -p "Update remote URL? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        git remote set-url origin "$REMOTE_URL"
    else
        echo "Using existing remote. To change: git remote set-url origin <new-url>"
    fi
else
    echo "Adding remote 'origin'..."
    git remote add origin "$REMOTE_URL"
fi

# Stage all files
echo ""
echo "Staging files..."
git add .

# Check if there are changes to commit
if git diff --cached --quiet; then
    echo "No changes to commit."
    exit 0
fi

# Show what will be committed
echo ""
echo "=== Files to be committed ==="
git status --short | head -20
echo "..."

# Commit
echo ""
read -p "Commit message (or press Enter for default): " COMMIT_MSG
if [ -z "$COMMIT_MSG" ]; then
    COMMIT_MSG="feat: Add event-based logging and flush metrics

- Add LoggerManager for event-based log separation
- Add flush duration metrics tracking
- Add timeout and recovery tests
- Remove UseMMap field (reverted to separate-shards)"
fi

echo ""
echo "Committing changes..."
git commit -m "$COMMIT_MSG"

# Push
echo ""
echo "Pushing to remote..."
BRANCH=$(git branch --show-current 2>/dev/null || echo "main")
if [ "$BRANCH" = "" ]; then
    git checkout -b main
    BRANCH="main"
fi

echo "Pushing branch: $BRANCH"
git push -u origin "$BRANCH"

echo ""
echo "=== Successfully pushed to remote ==="
echo "Repository URL: $REMOTE_URL"
echo "Branch: $BRANCH"
echo ""
echo "To clone on GCP VM:"
echo "  git clone $REMOTE_URL"
echo "  cd logger-double-buffer"
echo "  bash scripts/setup_gcp_vm.sh  # Install dependencies"
echo "  bash scripts/run_event_baseline_test.sh  # Run test"







