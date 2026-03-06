#!/bin/bash
set -e

# Test Repository Setup Script for PROnto
# This script creates a test repository with branches and PRs to test PROnto functionality

REPO_NAME="${1:-pronto-test}"
GITHUB_USER="${2:-$(gh api user -q .login)}"

echo "🚀 Setting up test repository: $REPO_NAME"
echo "👤 GitHub user: $GITHUB_USER"
echo ""

# Create temporary directory
TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR"

echo "📦 Creating new repository..."
gh repo create "$GITHUB_USER/$REPO_NAME" --public --clone

cd "$REPO_NAME"

# Configure git
git config user.name "PROnto Test"
git config user.email "test@pronto.local"

echo ""
echo "📝 Creating initial files..."

# Create a simple app file
cat > app.js << 'EOF'
// Simple test application
function hello(name) {
  console.log(`Hello, ${name}!`);
}

function goodbye(name) {
  console.log(`Goodbye, ${name}!`);
}

module.exports = { hello, goodbye };
EOF

# Create README
cat > README.md << 'EOF'
# PROnto Test Repository

This repository is used for testing PROnto functionality.
EOF

git add .
git commit -m "Initial commit"
git push -u origin main

echo ""
echo "🌿 Creating release branches..."

# Create release-1.0 branch
git checkout -b release-1.0
cat > VERSION << 'EOF'
1.0.0
EOF
# Modify app.js on release branch to create future conflict with refactor
cat > app.js << 'EOF'
// Simple test application - Release 1.0 Version
function hello(name) {
  console.log(`Hello, ${name}!`);
}

function goodbye(name) {
  console.log(`Goodbye, ${name}!`);
}

function welcome(name) {
  console.log(`Welcome to v1.0, ${name}!`);
}

module.exports = { hello, goodbye, welcome };
EOF
git add VERSION app.js
git commit -m "Initialize release-1.0"
git push -u origin release-1.0

# Create release-2.0 branch
git checkout main
git checkout -b release-2.0
cat > VERSION << 'EOF'
2.0.0
EOF
# Modify app.js on release branch to create future conflict with refactor
cat > app.js << 'EOF'
// Simple test application - Release 2.0 Version
function hello(name) {
  console.log(`Hello, ${name}!`);
}

function goodbye(name) {
  console.log(`Goodbye, ${name}!`);
}

function celebrate(name) {
  console.log(`Celebrating v2.0 with ${name}! 🎉`);
}

module.exports = { hello, goodbye, celebrate };
EOF
git add VERSION app.js
git commit -m "Initialize release-2.0"
git push -u origin release-2.0

# Back to main
git checkout main

echo ""
echo "⚙️  Setting up PROnto workflow..."

mkdir -p .github/workflows
cat > .github/workflows/pronto.yml << 'EOF'
name: PROnto

on:
  pull_request:
    types: [labeled, closed]
  issues:
    types: [opened, edited, labeled, closed]

permissions:
  contents: write
  pull-requests: write
  issues: write

jobs:
  pronto:
    runs-on: ubuntu-latest
    if: |
      (github.event_name == 'pull_request' && github.event.pull_request.merged == true) ||
      github.event_name == 'issues'
    steps:
      - uses: docker://ghcr.io/theakshaypant/pronto:latest
        with:
          github_token: ${{ secrets.GITHUB_TOKEN }}
EOF

git add .github/workflows/pronto.yml
git commit -m "Add PROnto workflow"
git push

echo ""
echo "🏷️  Creating labels..."

# Create pronto labels if they don't exist
gh label create "pronto/release-1.0" --repo "$GITHUB_USER/$REPO_NAME" --color "0e8a16" --description "Cherry-pick to release-1.0 branch" 2>/dev/null || echo "  Label pronto/release-1.0 already exists"
gh label create "pronto/release-2.0" --repo "$GITHUB_USER/$REPO_NAME" --color "0e8a16" --description "Cherry-pick to release-2.0 branch" 2>/dev/null || echo "  Label pronto/release-2.0 already exists"
gh label create "pronto-conflict" --repo "$GITHUB_USER/$REPO_NAME" --color "d93f0b" --description "Cherry-pick conflict - manual resolution needed" 2>/dev/null || echo "  Label pronto-conflict already exists"
gh label create "pronto" --repo "$GITHUB_USER/$REPO_NAME" --color "0366d6" --description "PROnto automated cherry-pick" 2>/dev/null || echo "  Label pronto already exists"

echo "✅ Labels created"

echo ""
echo "🔧 Creating test PRs (each modifies different files to avoid conflicts)..."

# PR #1: Bug fix - modifies greeting.js
git checkout -b fix/bug-1
cat > greeting.js << 'EOF'
// Greeting module
function greet(name) {
  console.log(`Hi, ${name}!`);
}

module.exports = { greet };
EOF
git add greeting.js
git commit -m "fix: add greeting module with improved greeting"
git push -u origin fix/bug-1
PR1_URL=$(gh pr create --base main --head fix/bug-1 --title "Fix: Add greeting module" --body "This is a test bug fix - adds greeting.js")
PR1=$(echo "$PR1_URL" | sed 's|.*/pull/||')
echo "✅ Created PR #$PR1 (modifies greeting.js)"

# PR #2: Feature - modifies features.js
git checkout main
git checkout -b feature/wave
cat > features.js << 'EOF'
// Features module
function wave(name) {
  console.log(`👋 ${name}!`);
}

function highFive(name) {
  console.log(`High five, ${name}! ✋`);
}

module.exports = { wave, highFive };
EOF
git add features.js
git commit -m "feat: add wave and high-five features"
git push -u origin feature/wave
PR2_URL=$(gh pr create --base main --head feature/wave --title "Feature: Add wave function" --body "This is a test feature - adds features.js")
PR2=$(echo "$PR2_URL" | sed 's|.*/pull/||')
echo "✅ Created PR #$PR2 (modifies features.js)"

# PR #3: Another fix - modifies utils.js
git checkout main
git checkout -b fix/utils
cat > utils.js << 'EOF'
// Utility functions
function formatMessage(message, name) {
  return `${message}, ${name}!`;
}

function timestamp() {
  return new Date().toISOString();
}

module.exports = { formatMessage, timestamp };
EOF
git add utils.js
git commit -m "fix: add utility functions"
git push -u origin fix/utils
PR3_URL=$(gh pr create --base main --head fix/utils --title "Fix: Add utility functions" --body "This is another test fix - adds utils.js")
PR3=$(echo "$PR3_URL" | sed 's|.*/pull/||')
echo "✅ Created PR #$PR3 (modifies utils.js)"

# PR #4: Conflicting change (modifies app.js which exists in release branches - DESIGNED TO CONFLICT)
git checkout main
git checkout -b fix/conflict-test
cat > app.js << 'EOF'
// Simple test application - COMPLETELY REFACTORED
// This refactor will conflict with the original app.js in release branches
class Greeter {
  static hello(name) {
    console.log(`Hello, ${name}!`);
  }

  static goodbye(name) {
    console.log(`Goodbye, ${name}!`);
  }

  static welcome(name) {
    console.log(`Welcome, ${name}!`);
  }
}

module.exports = Greeter;
EOF
git add app.js
git commit -m "refactor: convert app.js to class-based API"
git push -u origin fix/conflict-test
PR4_URL=$(gh pr create --base main --head fix/conflict-test --title "Refactor: Class-based API" --body "⚠️ This PR modifies app.js and will conflict when cherry-picked to release branches")
PR4=$(echo "$PR4_URL" | sed 's|.*/pull/||')
echo "✅ Created PR #$PR4 (modifies app.js - DESIGNED TO CONFLICT)"

git checkout main

echo ""
echo "📋 Summary of created PRs:"
echo "  PR #$PR1: Add greeting module (greeting.js) - safe to cherry-pick"
echo "  PR #$PR2: Add wave features (features.js) - safe to cherry-pick"
echo "  PR #$PR3: Add utility functions (utils.js) - safe to cherry-pick"
echo "  PR #$PR4: Refactor app.js (CONFLICTS WITH RELEASE BRANCHES)"
echo ""
echo "ℹ️  PRs #1-3 modify different files, so they won't conflict with each other"
echo "ℹ️  PR #4 modifies app.js and is designed to test conflict handling"

echo ""
echo "✅ Repository setup complete!"
echo ""
echo "📍 Repository: https://github.com/$GITHUB_USER/$REPO_NAME"
echo "📂 Local clone: $TEMP_DIR/$REPO_NAME"
echo ""
echo "🧪 Next steps to test PROnto:"
echo ""
echo "Option 1: Run Interactive Test Suite (Recommended)"
echo "  ./hack/scripts/test-interactive.sh $REPO_NAME $GITHUB_USER"
echo ""
echo "Option 2: Manual Testing"
echo ""
echo "Test 1: Single PR Cherry-pick"
echo "  1. Merge PR #$PR1: gh pr merge $PR1 -R $GITHUB_USER/$REPO_NAME -m"
echo "  2. Add label: gh pr edit $PR1 -R $GITHUB_USER/$REPO_NAME --add-label pronto/release-1.0"
echo "  3. Check Actions tab for PROnto workflow"
echo ""
echo "Test 2: Batch Cherry-pick via Issue"
echo "  1. Merge PRs: gh pr merge $PR2 -R $GITHUB_USER/$REPO_NAME -m && gh pr merge $PR3 -R $GITHUB_USER/$REPO_NAME -m"
echo "  2. Create tracking issue:"
echo "     gh issue create -R $GITHUB_USER/$REPO_NAME --title 'Backport to releases' --body 'Cherry-pick #$PR2, #$PR3' --label pronto/release-2.0"
echo "  3. Check Actions tab and issue comments"
echo ""
echo "Test 3: Conflict Handling"
echo "  1. Merge PR #$PR4: gh pr merge $PR4 -R $GITHUB_USER/$REPO_NAME -m"
echo "  2. Try cherry-picking: gh pr edit $PR4 -R $GITHUB_USER/$REPO_NAME --add-label pronto/release-1.0"
echo "  3. Should create a conflict PR automatically"
echo ""
echo "Test 4: Tracking Issue Auto-Updates"
echo "  Note: This test happens automatically when cherry-pick PRs merge from Test 2"
echo "  1. Check the tracking issue created in Test 2"
echo "  2. Find pending cherry-pick PRs: gh pr list -R $GITHUB_USER/$REPO_NAME --label pronto"
echo "  3. Merge one: gh pr merge <pr-number> -R $GITHUB_USER/$REPO_NAME -m"
echo "  4. Tracking issue should auto-update from 🔄 to ✅"
echo ""
echo "Test 5: Branch Creation"
echo "  Note: Reuses PR #$PR1 (should already be merged from Test 1)"
echo "  Add label: gh pr edit $PR1 -R $GITHUB_USER/$REPO_NAME --add-label 'pronto/test-branch..main'"
echo "  Verify: gh api repos/$GITHUB_USER/$REPO_NAME/git/refs/heads/test-branch"
echo ""
echo "Test 6: Tag Creation from PR Label"
echo "  Note: Reuses PR #$PR2 (should already be merged from Test 2)"
echo "  Add label: gh pr edit $PR2 -R $GITHUB_USER/$REPO_NAME --add-label 'pronto/tagged-branch..main?tag=v1.0.0-test'"
echo "  Verify: gh api repos/$GITHUB_USER/$REPO_NAME/git/refs/tags/v1.0.0-test"
echo ""
echo "Test 7: Tag Creation on Issue Close"
echo "  1. Create issue: gh issue create -R $GITHUB_USER/$REPO_NAME --title 'Release v1.0' --body 'Cherry-pick #$PR1' --label 'pronto/release-1.0?tag=v1.0.0'"
echo "  2. Wait for cherry-picks to complete"
echo "  3. Close issue: gh issue close <issue-number> -R $GITHUB_USER/$REPO_NAME"
echo "  4. Verify tag created: gh api repos/$GITHUB_USER/$REPO_NAME/git/refs/tags/v1.0.0"
echo "  Note: Tag creation on issue close may need to be implemented"
echo ""
echo "Cleanup:"
echo "  gh repo delete $GITHUB_USER/$REPO_NAME --yes"
echo "  rm -rf $TEMP_DIR"
