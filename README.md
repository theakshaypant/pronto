# PROnto

> Get your **PR onto** release branches, *pronto*.

Automatically cherry-pick merged PRs to release branches. Just add a label.

You know that feeling when you merge a bug fix to `main`, then realize you need it on three different release branches? Manually cherry-picking is tedious and error-prone.

**PROnto does it for you.** Merge your PR, add a `pronto/release-1.0` label, and walk away. The commits get cherry-picked automatically.

## How it works

**Single PR cherry-picking:**
1. Merge a PR to your main branch
2. Add a label like `pronto/release-1.0`
3. PROnto cherry-picks those commits to `release-1.0`

**Batch cherry-picking via issues:**
1. Create an issue listing PR numbers (e.g., `#123, #456, #789`)
2. Add `pronto/*` labels for target branches
3. PROnto processes all PRs × branches and tracks status in a table

If you have write access, it pushes directly. If not, it creates a PR for you. If there's a conflict, it creates a conflict PR with resolution instructions.

## Setup

Create `.github/workflows/pronto.yml`:

### Option 1: PR-Based Workflow Only

For single PR cherry-picking:

```yaml
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

### Option 2: Issue-Based Workflow Only

For batch cherry-picking via tracking issues:

```yaml
name: PROnto

on:
  issues:
    types: [opened, edited, labeled, closed]
  pull_request:
    types: [closed]

permissions:
  contents: write
  pull-requests: write
  issues: write

jobs:
  pronto:
    runs-on: ubuntu-latest
    if: |
      github.event_name == 'issues' ||
      (github.event_name == 'pull_request' && github.event.pull_request.merged == true)
    steps:
      - uses: theakshaypant/pronto@v1
        with:
          github_token: ${{ secrets.GITHUB_TOKEN }}
```

### Option 3: Both Workflows

For both single PR and batch issue-based cherry-picking:

```yaml
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
      - uses: theakshaypant/pronto@v1
        with:
          github_token: ${{ secrets.GITHUB_TOKEN }}
```

### Required Repository Setting

To allow PROnto to create pull requests (for conflict resolution or when you don't have write access), you must enable a repository setting:

1. Go to your repository **Settings** → **Actions** → **General**
2. Scroll down to **Workflow permissions**
3. Enable: **"Allow GitHub Actions to create and approve pull requests"**

Without this setting, PROnto will fail when trying to create cherry-pick PRs with a `403` error.

**When to use each option:**

- **Option 1 (PR-only)**: You only need single PR cherry-picking. Simpler setup, no issue permissions needed.
- **Option 2 (Issue-only)**: You primarily work with batch operations and want tracking issues to manage multiple backports.
- **Option 3 (Both)**: You want flexibility for both single PR cherry-picks and batch operations.

> **Note:** Option 2 still requires the `pull_request: closed` trigger to update tracking issues when cherry-pick PRs are merged.

## Usage

**Cherry-pick to an existing branch:**
```
pronto/release-1.0
```

**Create a branch and cherry-pick:**
```
pronto/release-2.0..main
```
This creates `release-2.0` from `main`, then cherry-picks your commits.

**Cherry-pick and create a Git tag:**
```
pronto/release-1.0?tag=v1.0.1
```
Cherry-picks to `release-1.0`. If `always_create_pr: false`, the tag `v1.0.1` is created automatically. If `always_create_pr: true` (default), the PR includes instructions for creating the tag after merge.

**Create branch, cherry-pick, and tag:**
```
pronto/release-2.0..main?tag=v2.0.0
```
Creates `release-2.0` from `main`, cherry-picks commits. Tag creation depends on the `always_create_pr` setting (see above).

**Multiple branches:**
Add multiple labels:
- `pronto/release-1.0?tag=v1.0.1`
- `pronto/release-2.0` (no tag)
- `pronto/hotfix-1.5?tag=hotfix-123`

Each branch gets processed independently. Tags are optional per branch.

## Batch Operations with Issues

Need to cherry-pick multiple PRs to the same release branches? Use a GitHub issue as a tracking issue.

**Create a tracking issue:**
1. Create an issue with any title (e.g., "Release v1.0 Backports")
2. List PR numbers in the body: `#123, #456, #789`
3. Add the same `pronto/*` labels you'd use on individual PRs

**Example:**

Issue title: `Release v1.0 Backports`

Issue body:
```
Cherry-pick the following PRs:
#123, #456, #789
```

Issue labels:
- `pronto/release-1.0`
- `pronto/release-2.0`

**What happens:**
- PROnto validates all PRs are merged
- Processes all 6 combinations (3 PRs × 2 branches)
- Posts a status table comment showing results for each PR+branch

**Status table example:**
```
| PR   | Branch       | Status | Details                        |
|------|--------------|--------|--------------------------------|
| #123 | release-1.0  | ✅     | Cherry-picked 3 commits        |
| #123 | release-2.0  | 🔄     | Pending merge of PR #500       |
| #456 | release-1.0  | ⚠️     | Conflicts - see PR #501        |
| #456 | release-2.0  | ✅     | Cherry-picked 2 commits        |
```

**Status indicators:**
- ✅ **Success** - Commits cherry-picked successfully
- 🔄 **Pending** - Cherry-pick PR created, waiting for merge
- ⚠️ **Conflicts** - Conflict PR created, needs manual resolution
- ❌ **Failed** - Operation failed (branch doesn't exist, etc.)
- ⏭️ **Skipped** - Already processed (duplicate)

**Benefits:**
- Track multiple related cherry-picks in one place
- See status of all operations at a glance
- Edit the issue to add/remove PRs and re-trigger
- Close the issue when all backports are complete

## Configuration

All inputs are optional except `github_token`.

```yaml
- uses: theakshaypant/pronto@v1
  with:
    github_token: ${{ secrets.GITHUB_TOKEN }}
    label_pattern: 'pronto/'           # default
    conflict_label: 'pronto-conflict'  # default
    bot_name: 'PROnto Bot'             # default
    bot_email: 'pronto[bot]@users.noreply.github.com'
    always_create_pr: 'true'           # default
```

## What happens when...

**By default (always_create_pr: 'true'):**
A pull request is created with the cherry-picked commits for review, even if you have write access. This enforces code review on cherry-picks. If you specified a tag with `?tag=`, the PR body will include instructions for creating the tag after merging.

**You want to push directly:**
Set `always_create_pr: 'false'` if you have write access and want commits pushed directly to the target branch. If you specified a tag with `?tag=`, it's created and pushed automatically. A success comment is added to the PR.

**You don't have write access:**
A pull request is created with the cherry-picked commits. Someone with permissions can review and merge it. If a tag was specified, instructions for creating it after merge are included in the PR body.

**There's a conflict:**
- **For single PRs:** A comment is added with the exact git commands to resolve it manually
- **For batch operations (issues):** A conflict PR is created automatically with the `pronto-conflict` label, allowing you to resolve conflicts through the PR interface and merge when ready

**The branch doesn't exist:**
If you used `pronto/branch-name` (without `..`), you'll get a comment saying the branch is missing.
If you used `pronto/branch-name..base`, we create the branch from `base` first.

**You specify a tag:**
- If `always_create_pr: false` and you have write access: PROnto creates an annotated Git tag with metadata (PR number, branch, commit count) and pushes it automatically.
- If `always_create_pr: true` (default) or you lack write access: The created PR includes a "Tag Creation Required" section with the exact commands to create the tag after merging.

## Troubleshooting

**Nothing happens when I add the label:**
- For PRs: Make sure the PR is merged
- For issues: Make sure the issue body contains PR numbers (e.g., `#123`)
- Check that your workflow file has the correct triggers (see Setup section)
- Verify the label starts with `pronto/` (or your configured prefix)

**Permission denied:**
Add these to your workflow:
```yaml
permissions:
  contents: write
  pull-requests: write
  issues: write  # Required for batch operations
```

**Branch doesn't exist:**
Either create it manually, or use the `..` notation: `pronto/release-1.0..main`

## Requirements

- GitHub Actions enabled
- Git repository
- `GITHUB_TOKEN` with write permissions

## License

MIT - see [LICENSE](LICENSE)
