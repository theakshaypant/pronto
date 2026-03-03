# PROnto Quick Start Guide

Get PROnto running in your repository in 5 minutes.

## For Testing (Local Development)

### 1. Build and Test Locally

```bash
# Build Docker image
docker build -t pronto:test .

# Run local test
./scripts/test-local.sh

# Edit test-event.json to try different scenarios
```

### 2. Test in a Real Repository

**Step 1: Create test repository**

```bash
# Create new repo on GitHub or use existing one
gh repo create pronto-test --public

# Or use your existing repo
cd /path/to/your/repo
```

**Step 2: Add workflow file**

Create `.github/workflows/pronto.yml`:

```yaml
name: PROnto Test

on:
  pull_request:
    types: [labeled, closed]

permissions:
  contents: write
  pull-requests: write

jobs:
  pronto:
    runs-on: ubuntu-latest
    if: github.event.pull_request.merged == true
    steps:
      - uses: docker://ghcr.io/theakshaypant/pronto:latest
        # Or use: theakshaypant/pronto@main (once pushed)
        with:
          github_token: ${{ secrets.GITHUB_TOKEN }}
```

**Step 3: Create release branch**

```bash
git checkout -b release-1.0
git push -u origin release-1.0
git checkout main
```

**Step 4: Test the workflow**

```bash
# Create test PR
git checkout -b test-pr
echo "Test change" >> README.md
git add README.md
git commit -m "Test: Add test change"
git push -u origin test-pr

# Create PR via GitHub UI or:
gh pr create --title "Test PROnto" --body "Testing cherry-pick"

# Merge the PR
gh pr merge --merge

# Add label
gh pr edit <PR-NUMBER> --add-label "pronto/release-1.0"

# Check action status
gh run list
gh run view <RUN-ID> --log
```

**Step 5: Verify results**

```bash
# Check if commits were cherry-picked
git checkout release-1.0
git pull
git log -1

# Check PR comments
gh pr view <PR-NUMBER>
```

## For Production Use

### 1. Install in Your Repository

**Option A: Use published action (after release)**

```yaml
# .github/workflows/pronto.yml
name: PROnto

on:
  pull_request:
    types: [labeled, closed]

permissions:
  contents: write
  pull-requests: write

jobs:
  pronto:
    runs-on: ubuntu-latest
    if: github.event.pull_request.merged == true
    steps:
      - uses: theakshaypant/pronto@v1
        with:
          github_token: ${{ secrets.GITHUB_TOKEN }}
```

**Option B: Use Docker image directly**

```yaml
steps:
  - uses: docker://ghcr.io/theakshaypant/pronto:v1
    with:
      github_token: ${{ secrets.GITHUB_TOKEN }}
```

### 2. Configure (Optional)

Customize behavior with inputs:

```yaml
- uses: theakshaypant/pronto@v1
  with:
    github_token: ${{ secrets.GITHUB_TOKEN }}
    label_pattern: 'backport/'      # Use backport/* labels
    conflict_label: 'needs-resolve'  # Custom conflict label
    bot_name: 'Release Bot'          # Custom bot name
    bot_email: 'bot@company.com'     # Custom bot email
```

### 3. Use in Your Workflow

1. **Merge PR to main**
2. **Add label**: `pronto/release-1.0` or `pronto/release-1.0?tag=v1.0.1`
3. **Done!** PROnto automatically:
   - Cherry-picks commits
   - Creates Git tags (if specified)
   - Handles conflicts
   - Creates PRs if needed

## Common Scenarios

### Backport to existing release

```bash
# After merging PR
gh pr edit <PR> --add-label "pronto/release-1.0"
```

### Backport with Git tag

```bash
# Cherry-pick and create a tag
gh pr edit <PR> --add-label "pronto/release-1.0?tag=v1.0.1"
```

### Create new release branch

```bash
# Add label with .. notation
gh pr edit <PR> --add-label "pronto/release-2.0..main"
```

### Create branch and tag

```bash
# Create branch, cherry-pick, and tag
gh pr edit <PR> --add-label "pronto/release-2.0..main?tag=v2.0.0"
```

### Backport to multiple releases

```bash
# Add multiple labels (with selective tagging)
gh pr edit <PR> \
  --add-label "pronto/release-1.0?tag=v1.0.1" \
  --add-label "pronto/release-2.0" \
  --add-label "pronto/release-3.0?tag=v3.0.0"
```

## Troubleshooting

### Action doesn't trigger

**Check:**
- PR is merged
- Label starts with `pronto/`
- Workflow file exists in `.github/workflows/`

### Permission denied

**Check:**
- Workflow has `permissions` block
- `GITHUB_TOKEN` has write access

### Can't find branch

**Solution:**
- Create branch manually, OR
- Use `pronto/branch..base` notation

## Next Steps

- ✅ Read [README.md](README.md) for full documentation
- ✅ See [TESTING.md](TESTING.md) for comprehensive test scenarios
- ✅ Check [Examples](.github/workflows/pronto-example.yml) for more configurations

## Support

- 🐛 Issues: [GitHub Issues](https://github.com/theakshaypant/pronto/issues)
- 💬 Discussions: [GitHub Discussions](https://github.com/theakshaypant/pronto/discussions)
- 📖 Docs: [README.md](README.md)

---

**Happy cherry-picking!** 🍒
