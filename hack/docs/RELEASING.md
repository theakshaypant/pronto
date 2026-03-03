# Release Guide

## Quick Manual Push

### 1. Create a GitHub Personal Access Token

1. Go to https://github.com/settings/tokens/new
2. Select scopes:
   - `write:packages` (required)
   - `read:packages` (required)
3. Generate token and save it

### 2. Login to GHCR

```bash
# Login with your username and the token you just created
echo $YOUR_TOKEN | docker login ghcr.io -u theakshaypant --password-stdin
```

### 3. Build and Push

```bash
# Quick way - use the script
./hack/scripts/push-image.sh v1.0.0

# Or manually:
docker build -t ghcr.io/theakshaypant/pronto:v1.0.0 .
docker tag ghcr.io/theakshaypant/pronto:v1.0.0 ghcr.io/theakshaypant/pronto:latest
docker push ghcr.io/theakshaypant/pronto:v1.0.0
docker push ghcr.io/theakshaypant/pronto:latest
```

### 4. Make Package Public

After first push:
1. Go to https://github.com/theakshaypant/pronto/pkgs/container/pronto
2. Click "Package settings"
3. Scroll down to "Danger Zone"
4. Click "Change visibility" → "Public"

## Automated Releases (Recommended)

The repo has a GitHub Actions workflow that automatically builds and publishes on git tags.

### Create a Release

```bash
# Make sure you're on main and everything is committed
git checkout main
git pull

# Create and push a tag
git tag v1.0.0
git push origin v1.0.0
```

This will automatically:
- Build the Docker image
- Push to ghcr.io/theakshaypant/pronto:v1.0.0
- Push to ghcr.io/theakshaypant/pronto:latest
- Create a GitHub Release with notes

### Version Tags

The workflow supports semantic versioning:
- `v1.0.0` → Creates tags: `v1.0.0`, `v1.0`, `v1`, `latest`
- `v1.2.3` → Creates tags: `v1.2.3`, `v1.2`, `v1`, `latest`

## Testing the Image

After pushing:

```bash
# Pull and test
docker pull ghcr.io/theakshaypant/pronto:latest
docker run --rm ghcr.io/theakshaypant/pronto:latest

# Test in a workflow
# Update .github/workflows/pronto-example.yml to use:
uses: docker://ghcr.io/theakshaypant/pronto:latest
```

## Troubleshooting

### "denied: permission_denied"

You need to login first:
```bash
docker login ghcr.io
```

### "package does not exist or you do not have permission"

The package is private by default. Make it public:
1. Go to package settings
2. Change visibility to public

### "authentication required"

Your token might have expired or lacks the right scopes. Create a new one with `write:packages`.

## Checklist for First Release

- [ ] Build Docker image locally
- [ ] Test the image works
- [ ] Login to GHCR
- [ ] Push image with version tag
- [ ] Push image with latest tag
- [ ] Make package public
- [ ] Update README to reference the published image
- [ ] Create git tag
- [ ] Push git tag (triggers automated release)

## Update Action Marketplace

After publishing to GHCR, you can also publish to GitHub Actions Marketplace:

1. Make sure `action.yml` is correct
2. Create a release on GitHub
3. Check "Publish this Action to the GitHub Marketplace"
4. Fill in the required metadata
5. Publish!

Then users can use:
```yaml
uses: theakshaypant/pronto@v1
```

Instead of the longer Docker URL.
