#!/bin/bash
set -e

# Push PROnto Docker image to GHCR
# Usage: ./scripts/push-image.sh [version]
# Example: ./scripts/push-image.sh v1.0.0

VERSION="${1:-latest}"
REPO="ghcr.io/theakshaypant/pronto"

echo "🐳 Building and pushing PROnto to GHCR"
echo "Version: $VERSION"
echo ""

# Build the image
echo "📦 Building Docker image..."
docker build -t "$REPO:$VERSION" .

# Also tag as latest if this is a version tag
if [[ "$VERSION" != "latest" ]]; then
    echo "🏷️  Tagging as latest..."
    docker tag "$REPO:$VERSION" "$REPO:latest"
fi

# Login to GHCR (you'll need to authenticate)
echo "🔐 Logging in to GHCR..."
echo "💡 Tip: Create a personal access token with 'write:packages' scope"
echo "   https://github.com/settings/tokens/new?scopes=write:packages"
echo ""

# Check if already logged in
if ! docker info 2>/dev/null | grep -q "ghcr.io"; then
    echo "Username: theakshaypant"
    docker login ghcr.io -u theakshaypant
fi

# Push the image
echo ""
echo "🚀 Pushing $REPO:$VERSION..."
docker push "$REPO:$VERSION"

if [[ "$VERSION" != "latest" ]]; then
    echo "🚀 Pushing $REPO:latest..."
    docker push "$REPO:latest"
fi

echo ""
echo "✅ Successfully pushed to GHCR!"
echo "📦 Image: $REPO:$VERSION"
echo "🔗 View at: https://github.com/theakshaypant/pronto/pkgs/container/pronto"
