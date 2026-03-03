#!/bin/bash
set -e

# Local testing script for PROnto
# This simulates the GitHub Actions environment for manual testing

echo "🧪 PROnto Local Testing"
echo "======================="
echo ""

# Check if Docker is available
if ! command -v docker &> /dev/null; then
    echo "❌ Error: Docker is required but not installed"
    exit 1
fi

# Build Docker image
echo "🐳 Building Docker image..."
docker build -t pronto:test . -q
echo "✅ Docker image built successfully"
echo ""

# Check if test event file exists
if [ ! -f "test-event.json" ]; then
    echo "⚠️  No test-event.json found. Creating sample..."
    cat > test-event.json << 'EOF'
{
  "action": "labeled",
  "pull_request": {
    "number": 1,
    "merged": true,
    "head": {
      "sha": "abc123def456"
    },
    "user": {
      "login": "test-user"
    },
    "labels": [
      {
        "name": "pronto/release-1.0"
      }
    ]
  },
  "repository": {
    "name": "test-repo",
    "owner": {
      "login": "test-owner"
    },
    "clone_url": "https://github.com/test-owner/test-repo.git"
  },
  "sender": {
    "login": "test-user"
  }
}
EOF
    echo "✅ Created sample test-event.json"
    echo ""
fi

# Set up environment variables
export GITHUB_EVENT_PATH="$(pwd)/test-event.json"
export GITHUB_EVENT_NAME="pull_request"
export INPUT_GITHUB_TOKEN="${INPUT_GITHUB_TOKEN:-test-token}"
export INPUT_LABEL_PATTERN="${INPUT_LABEL_PATTERN:-pronto/}"
export INPUT_CONFLICT_LABEL="${INPUT_CONFLICT_LABEL:-pronto-conflict}"
export INPUT_BOT_NAME="${INPUT_BOT_NAME:-PROnto Bot}"
export INPUT_BOT_EMAIL="${INPUT_BOT_EMAIL:-pronto[bot]@users.noreply.github.com}"

echo "📋 Environment:"
echo "  Event: $GITHUB_EVENT_NAME"
echo "  Event Path: $GITHUB_EVENT_PATH"
echo "  Label Pattern: $INPUT_LABEL_PATTERN"
echo ""

# Run Docker container
echo "🚀 Running PROnto..."
echo ""

docker run --rm \
    -e GITHUB_EVENT_PATH="/github/workflow/event.json" \
    -e GITHUB_EVENT_NAME="$GITHUB_EVENT_NAME" \
    -e INPUT_GITHUB_TOKEN="$INPUT_GITHUB_TOKEN" \
    -e INPUT_LABEL_PATTERN="$INPUT_LABEL_PATTERN" \
    -e INPUT_CONFLICT_LABEL="$INPUT_CONFLICT_LABEL" \
    -e INPUT_BOT_NAME="$INPUT_BOT_NAME" \
    -e INPUT_BOT_EMAIL="$INPUT_BOT_EMAIL" \
    -v "$(pwd)/test-event.json:/github/workflow/event.json:ro" \
    pronto:test

echo ""
echo "✅ Test completed!"
echo ""
echo "💡 Tip: Edit test-event.json to test different scenarios"
echo "   - Change 'merged' to true/false"
echo "   - Add/remove labels"
echo "   - Change action to 'closed' or 'labeled'"
