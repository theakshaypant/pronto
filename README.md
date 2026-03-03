# PROnto

> Get your **PR onto** release branches, *pronto*.

Automatically cherry-pick merged PRs to release branches. Just add a label.

You know that feeling when you merge a bug fix to `main`, then realize you need it on three different release branches? Manually cherry-picking is tedious and error-prone.

**PROnto does it for you.** Merge your PR, add a `pronto/release-1.0` label, and walk away. The commits get cherry-picked automatically.

## How it works

1. Merge a PR to your main branch
2. Add a label like `pronto/release-1.0`
3. PROnto cherry-picks those commits to `release-1.0`

If you have write access, it pushes directly. If not, it creates a PR for you. If there's a conflict, it adds a comment with the exact git commands to resolve it.

## Setup

Create `.github/workflows/pronto.yml`:

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

That's it. Now when you add a `pronto/*` label to a merged PR, it cherry-picks automatically.

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
Cherry-picks to `release-1.0` and creates an annotated tag `v1.0.1`.

**Create branch, cherry-pick, and tag:**
```
pronto/release-2.0..main?tag=v2.0.0
```
Creates `release-2.0` from `main`, cherry-picks commits, and creates tag `v2.0.0`.

**Multiple branches:**
Add multiple labels:
- `pronto/release-1.0?tag=v1.0.1`
- `pronto/release-2.0` (no tag)
- `pronto/hotfix-1.5?tag=hotfix-123`

Each branch gets processed independently. Tags are optional per branch.

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
```

## What happens when...

**You have write access:**
Commits are pushed directly to the target branch. If you specified a tag with `?tag=`, it's created and pushed too. A success comment is added to the PR.

**You don't have write access:**
A fallback PR is created with the cherry-picked commits. Someone with permissions can review and merge it.

**There's a conflict:**
A comment is added with the exact git commands to resolve it manually, including the commit SHAs.

**The branch doesn't exist:**
If you used `pronto/branch-name` (without `..`), you'll get a comment saying the branch is missing.
If you used `pronto/branch-name..base`, we create the branch from `base` first.

**You specify a tag:**
After a successful cherry-pick, PROnto creates an annotated Git tag with metadata (PR number, branch, commit count) and pushes it to the remote.

## Troubleshooting

**Nothing happens when I add the label:**
- Make sure the PR is merged
- Check that your workflow file has both `labeled` and `closed` in the triggers
- Verify the label starts with `pronto/` (or your configured prefix)

**Permission denied:**
Add these to your workflow:
```yaml
permissions:
  contents: write
  pull-requests: write
```

**Branch doesn't exist:**
Either create it manually, or use the `..` notation: `pronto/release-1.0..main`

## Requirements

- GitHub Actions enabled
- Git repository
- `GITHUB_TOKEN` with write permissions

## License

MIT - see [LICENSE](LICENSE)
