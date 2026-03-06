# PROnto Testing Guide

This guide helps you test PROnto functionality in a controlled environment using the provided test scripts.

## Prerequisites

- [GitHub CLI](https://cli.github.com/) (`gh`) installed and authenticated
- Git configured with your credentials
- Permissions to create repositories in your GitHub account

## Quick Start

### 1. Setup Test Repository

Create a new test repository with sample branches and PRs:

```bash
./hack/scripts/test-setup.sh [repo-name] [github-user]
```

**Example:**
```bash
./hack/scripts/test-setup.sh pronto-test
```

This script will:
- Create a new GitHub repository
- Set up `main`, `release-1.0`, and `release-2.0` branches
- Install the PROnto workflow
- Create 4 test PRs:
  - PR #1: Simple bug fix (safe to cherry-pick)
  - PR #2: New feature (safe to cherry-pick)
  - PR #3: Another bug fix (safe to cherry-pick)
  - PR #4: Refactor (will cause conflicts)

### 2. Run Interactive Tests

Guide through testing each PROnto feature:

```bash
./hack/scripts/test-interactive.sh [repo-name] [github-user]
```

**Example:**
```bash
./hack/scripts/test-interactive.sh pronto-test
```

This script will guide you through:
- **Test 1**: Single PR cherry-pick workflow (uses `release-1.0`)
- **Test 2**: Batch cherry-pick via tracking issue (uses `release-2.0`)
- **Test 3**: Automatic conflict PR creation (uses `release-1.0`, designed to conflict)
- **Test 4**: Tracking issue auto-updates
- **Test 5**: Branch creation from cherry-pick (creates new branch from base)
- **Test 6**: Tag creation from cherry-pick (creates tag after successful cherry-pick)

**Note:** Tests are isolated by using different target branches to prevent conflicts between tests.

## Manual Testing

If you prefer manual testing, follow these steps after running `test-setup.sh`:

### Test 1: Single PR Cherry-pick

```bash
# Merge a PR
gh pr merge 1 --repo youruser/pronto-test --merge

# Add pronto label
gh pr edit 1 --repo youruser/pronto-test --add-label pronto/release-1.0

# Watch Actions tab
gh run list --repo youruser/pronto-test
```

**Expected:**
- ✅ PROnto workflow runs
- ✅ Commits cherry-picked to `release-1.0`
- ✅ Success comment on PR #1

### Test 2: Batch Cherry-pick

**Note:** Use `release-2.0` only to avoid conflicts if you ran Test 1.

```bash
# Merge multiple PRs
gh pr merge 2 --repo youruser/pronto-test --merge
gh pr merge 3 --repo youruser/pronto-test --merge

# Create tracking issue
gh issue create --repo youruser/pronto-test \
  --title "Backport to releases" \
  --body "Cherry-pick #2, #3" \
  --label pronto/release-2.0

# Check the issue for status table
gh issue list --repo youruser/pronto-test --label pronto
```

**Expected:**
- ✅ PROnto processes 2 combinations (2 PRs × 1 branch)
- ✅ Status table comment posted
- ✅ Shows success/pending/conflict for each

### Test 3: Conflict Handling

```bash
# Merge the refactor PR (likely to conflict)
gh pr merge 4 --repo youruser/pronto-test --merge

# Try to cherry-pick
gh pr edit 4 --repo youruser/pronto-test --add-label pronto/release-1.0

# Wait for workflow, then check for conflict PRs
gh pr list --repo youruser/pronto-test --label pronto-conflict
```

**Expected:**
- ⚠️ Conflicts detected
- ✅ Conflict PR created automatically
- ✅ PR has `pronto-conflict` label
- ✅ Includes resolution instructions

### Test 4: Tracking Issue Updates

```bash
# Create a tracking issue (or use existing one)
gh issue create --repo youruser/pronto-test \
  --title "Test auto-updates" \
  --body "#2" \
  --label pronto/release-1.0

# PROnto creates a cherry-pick PR (check Actions)
# Once the cherry-pick PR is created, merge it
gh pr list --repo youruser/pronto-test --label pronto
gh pr merge <cherry-pick-pr-number> --repo youruser/pronto-test --merge

# Check if tracking issue updated
gh issue view <issue-number> --repo youruser/pronto-test
```

**Expected:**
- ✅ Status changes from 🔄 to ✅
- ✅ Shows "Cherry-picked via PR #X"
- ✅ Summary counts updated

### Test 5: Branch Creation

Test creating a new branch and cherry-picking to it using the `pronto/new-branch..base-branch` notation.

```bash
# Merge a PR
gh pr merge 1 --repo youruser/pronto-test --merge

# Add label to create new branch from main
gh pr edit 1 --repo youruser/pronto-test --add-label "pronto/test-branch..main"

# Wait for workflow to complete
sleep 10

# Verify branch was created
gh api repos/youruser/pronto-test/git/refs/heads/test-branch
```

**Expected:**
- ✅ New branch `test-branch` created from `main`
- ✅ Commits cherry-picked to the new branch
- ✅ Success comment on PR

### Test 6: Tag Creation

Test creating a tag after cherry-picking using the `pronto/branch..base?tag=tag-name` notation.

```bash
# Merge a PR
gh pr merge 2 --repo youruser/pronto-test --merge

# Add label to create branch with tag
gh pr edit 2 --repo youruser/pronto-test --add-label "pronto/tagged-branch..main?tag=v1.0.0-test"

# Wait for workflow to complete
sleep 15

# Verify branch and tag were created
gh api repos/youruser/pronto-test/git/refs/heads/tagged-branch
gh api repos/youruser/pronto-test/git/refs/tags/v1.0.0-test
```

**Expected:**
- ✅ New branch `tagged-branch` created from `main`
- ✅ Commits cherry-picked to the new branch
- ✅ Tag `v1.0.0-test` created (or instructions provided in PR for manual creation)
- ✅ PR body includes tag creation commands

## Cleanup

When done testing, delete the test repository:

```bash
gh repo delete youruser/pronto-test --yes
```

## Troubleshooting

**Workflows not running?**
- Check repository Settings → Actions → General
- Ensure "Allow all actions and reusable workflows" is enabled

**Permission errors?**
- Verify `GITHUB_TOKEN` has required permissions in workflow file
- Check repository Settings → Actions → Workflow permissions

**PROnto action not found?**
- Replace `theakshaypant/pronto@v1` with your fork or local path
- For local testing, use a commit SHA: `theakshaypant/pronto@abc1234`

**Conflicts not being created?**
- Ensure PR #4 (refactor) is actually conflicting with release branches
- Check Actions logs for detailed error messages

## Development Testing

To test local changes before pushing:

1. Build and push your Docker image:
   ```bash
   docker build -t ghcr.io/youruser/pronto:test .
   docker push ghcr.io/youruser/pronto:test
   ```

2. Update test workflow to use your image:
   ```yaml
   - uses: docker://ghcr.io/youruser/pronto:test
   ```

3. Run test scripts as normal
