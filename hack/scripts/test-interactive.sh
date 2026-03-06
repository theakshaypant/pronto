#!/bin/bash
set -e

# Interactive PROnto Testing Script
# Guides you through testing PROnto functionality step-by-step

REPO_NAME="${1:-pronto-test}"
GITHUB_USER="${2:-$(gh api user -q .login)}"

echo "🧪 PROnto Interactive Test Suite"
echo "=================================="
echo ""
echo "This script will guide you through testing PROnto functionality."
echo "Repository: $GITHUB_USER/$REPO_NAME"
echo ""
echo "ℹ️  Note: Tests are isolated to prevent conflicts:"
echo "   - Test 1: Single PR cherry-pick (release-1.0)"
echo "   - Test 2: Batch operations (release-2.0)"
echo "   - Test 3: Conflict handling (release-1.0, designed to conflict)"
echo "   - Test 4: Tracking issue auto-updates"
echo "   - Test 5: Branch creation (test-branch)"
echo "   - Test 6: Tag creation from PR label (tagged-branch + v1.0.0-test)"
echo "   - Test 7: Tag creation on issue close (v1.0.0)"
echo ""

# Check if repo exists
if ! gh repo view "$GITHUB_USER/$REPO_NAME" &>/dev/null; then
  echo "❌ Repository not found: $GITHUB_USER/$REPO_NAME"
  echo ""
  echo "Run the setup script first:"
  echo "  ./hack/scripts/test-setup.sh $REPO_NAME $GITHUB_USER"
  exit 1
fi

echo "✅ Repository found"
echo ""

# Get PR numbers
echo "📋 Available PRs:"
gh pr list -R "$GITHUB_USER/$REPO_NAME" --state all
echo ""

function pause() {
  echo ""
  read -p "Press Enter to continue..."
  echo ""
}

function run_test() {
  local test_name="$1"
  local description="$2"

  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "🧪 TEST: $test_name"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo ""
  echo "$description"
  echo ""
}

# Test 1: Single PR Cherry-pick
run_test "Single PR Cherry-pick" \
"This test merges a single PR and applies the pronto/release-1.0 label.
PROnto should automatically cherry-pick the commits to the release-1.0 branch."

# Get first open PR number (first column is the PR number)
FIRST_PR=$(gh pr list -R "$GITHUB_USER/$REPO_NAME" --state open --limit 1 | awk '{print $1}' | head -1)

if [ -z "$FIRST_PR" ]; then
  echo "⚠️  No open PRs found. Skipping this test."
else
  echo "Step 1: Merge PR #$FIRST_PR"
  gh pr merge "$FIRST_PR" -R "$GITHUB_USER/$REPO_NAME" --merge --delete-branch || true

  echo ""
  echo "Step 2: Add pronto/release-1.0 label"
  gh pr edit "$FIRST_PR" -R "$GITHUB_USER/$REPO_NAME" --add-label pronto/release-1.0

  echo ""
  echo "Step 3: Check GitHub Actions"
  echo "  View: https://github.com/$GITHUB_USER/$REPO_NAME/actions"

  echo ""
  echo "Expected result:"
  echo "  ✅ PROnto workflow runs"
  echo "  ✅ Commits cherry-picked to release-1.0"
  echo "  ✅ Either pushed directly or PR created"
  echo "  ✅ Comment added to PR #$FIRST_PR"
fi

pause

# Test 2: Batch Cherry-pick via Issue
run_test "Batch Cherry-pick via Issue" \
"This test creates a tracking issue with multiple PR numbers.
PROnto should process all PR × branch combinations and post a status table.
Note: Uses release-2.0 to avoid conflicts with Test 1."

# Get next two open PRs (first column is the PR number)
PR_LIST=$(gh pr list -R "$GITHUB_USER/$REPO_NAME" --state open --limit 2 | awk '{print $1}' | tr '\n' ' ')

if [ -z "$PR_LIST" ]; then
  echo "⚠️  Not enough open PRs for batch test. Skipping."
else
  echo "Step 1: Merge PRs for testing"
  for pr in $PR_LIST; do
    echo "  Merging PR #$pr..."
    gh pr merge "$pr" -R "$GITHUB_USER/$REPO_NAME" --merge --delete-branch || true
  done

  echo ""
  echo "Step 2: Create tracking issue"

  # Build PR list for issue body
  PR_REFS=$(echo "$PR_LIST" | sed 's/ /, #/g' | sed 's/^/#/')

  ISSUE_BODY="Cherry-pick the following PRs to release branches:

$PR_REFS

This is a test of the batch cherry-pick functionality."

  # Note: Only using release-2.0 to avoid conflicts with Test 1
  ISSUE_URL=$(gh issue create -R "$GITHUB_USER/$REPO_NAME" \
    --title "Test: Batch backport" \
    --body "$ISSUE_BODY" \
    --label pronto/release-2.0)

  ISSUE_NUM=$(echo "$ISSUE_URL" | sed 's|.*/issues/||')
  echo "  ✅ Created issue #$ISSUE_NUM"

  echo ""
  echo "Step 3: Monitor the issue"
  echo "  View: https://github.com/$GITHUB_USER/$REPO_NAME/issues/$ISSUE_NUM"

  echo ""
  echo "Expected result:"
  echo "  ✅ PROnto workflow runs"
  echo "  ✅ Status table posted as comment"
  echo "  ✅ Each PR × branch combination processed (2 PRs × 1 branch = 2 operations)"
  echo "  ✅ Success/pending/conflict status shown"

  echo ""
  echo "Opening issue in browser..."
  gh issue view "$ISSUE_NUM" -R "$GITHUB_USER/$REPO_NAME" --web || true
fi

pause

# Test 3: Conflict Handling
run_test "Conflict PR Creation" \
"This test attempts to cherry-pick a PR that will likely conflict.
PROnto should create a conflict PR with the pronto-conflict label."

# Look for the refactor PR (usually has "refactor" or "class" in title)
CONFLICT_PR=$(gh pr list -R "$GITHUB_USER/$REPO_NAME" --state all --search "refactor" --limit 1 | awk '{print $1}' | head -1)

if [ -z "$CONFLICT_PR" ]; then
  # Try looking for "class" if refactor not found
  CONFLICT_PR=$(gh pr list -R "$GITHUB_USER/$REPO_NAME" --state all --search "class" --limit 1 | awk '{print $1}' | head -1)
fi

if [ -z "$CONFLICT_PR" ]; then
  echo "⚠️  No suitable PR found for conflict test. Skipping."
else
  PR_STATE=$(gh pr view "$CONFLICT_PR" -R "$GITHUB_USER/$REPO_NAME" | grep "state:" | awk '{print $2}')

  if [ "$PR_STATE" != "MERGED" ]; then
    echo "Step 1: Merge the refactor PR #$CONFLICT_PR"
    gh pr merge "$CONFLICT_PR" -R "$GITHUB_USER/$REPO_NAME" --merge --delete-branch || true
  fi

  echo ""
  echo "Step 2: Try cherry-picking to release-1.0"
  gh pr edit "$CONFLICT_PR" -R "$GITHUB_USER/$REPO_NAME" --add-label pronto/release-1.0

  echo ""
  echo "Step 3: Wait for PROnto to process"
  echo "  Actions: https://github.com/$GITHUB_USER/$REPO_NAME/actions"

  echo ""
  echo "Expected result:"
  echo "  ⚠️  Cherry-pick encounters conflicts"
  echo "  ✅ Conflict PR created automatically"
  echo "  ✅ PR has pronto-conflict label"
  echo "  ✅ PR includes resolution instructions"

  echo ""
  echo "Wait a few seconds for the workflow to complete..."
  sleep 10

  echo ""
  echo "Looking for conflict PRs..."
  gh pr list -R "$GITHUB_USER/$REPO_NAME" --label pronto-conflict || echo "⏳ No conflict PRs found yet. Check Actions tab."
fi

pause

# Test 4: Tracking Issue Updates
run_test "Tracking Issue Updates" \
"This test verifies that tracking issues update when cherry-pick PRs merge.
The status should change from 🔄 pending to ✅ success."

echo "Find a tracking issue with pending cherry-pick PRs:"
gh issue list -R "$GITHUB_USER/$REPO_NAME" --label pronto

echo ""
echo "Steps:"
echo "  1. Identify a pending cherry-pick PR from the tracking issue"
echo "  2. Merge that cherry-pick PR"
echo "  3. Check if the tracking issue updates automatically"

echo ""
echo "Expected result:"
echo "  ✅ Status changes from 🔄 to ✅"
echo "  ✅ Message shows 'Cherry-picked via PR #XXX'"
echo "  ✅ Summary counts recalculated"

pause

# Test 5: Branch Creation
run_test "Branch Creation from Cherry-pick" \
"This test verifies that PROnto can create a new branch and cherry-pick to it.
Uses the notation: pronto/new-branch..base-branch"

# Find an open or recently merged PR
TEST_PR=$(gh pr list -R "$GITHUB_USER/$REPO_NAME" --state all --limit 1 | awk '{print $1}' | head -1)

if [ -z "$TEST_PR" ]; then
  echo "⚠️  No PRs found for branch creation test. Skipping."
else
  # Make sure it's merged
  PR_STATE=$(gh pr view "$TEST_PR" -R "$GITHUB_USER/$REPO_NAME" --json state -q .state)
  if [ "$PR_STATE" != "MERGED" ]; then
    echo "Merging PR #$TEST_PR first..."
    gh pr merge "$TEST_PR" -R "$GITHUB_USER/$REPO_NAME" --merge --delete-branch || true
  fi

  echo "Step 1: Add label to create new branch from main"
  echo "  Using: pronto/test-branch..main"
  gh pr edit "$TEST_PR" -R "$GITHUB_USER/$REPO_NAME" --add-label "pronto/test-branch..main"

  echo ""
  echo "Step 2: Wait for workflow"
  sleep 10

  echo ""
  echo "Step 3: Verify branch was created"
  if gh api "repos/$GITHUB_USER/$REPO_NAME/git/refs/heads/test-branch" &>/dev/null; then
    echo "  ✅ Branch 'test-branch' was created successfully!"
  else
    echo "  ⏳ Branch not found yet. Check Actions tab."
  fi

  echo ""
  echo "Expected result:"
  echo "  ✅ New branch 'test-branch' created from 'main'"
  echo "  ✅ Commits cherry-picked to the new branch"
  echo "  ✅ Comment shows successful cherry-pick"
fi

pause

# Test 6: Tag Creation
run_test "Tag Creation from Cherry-pick" \
"This test verifies that PROnto can create a tag after cherry-picking.
Uses the notation: pronto/branch..base..tag-name"

# Find another open or recently merged PR
TAG_TEST_PR=$(gh pr list -R "$GITHUB_USER/$REPO_NAME" --state all --limit 1 --search "sort:updated-desc" | awk '{print $1}' | head -1)

if [ -z "$TAG_TEST_PR" ]; then
  echo "⚠️  No PRs found for tag creation test. Skipping."
else
  # Make sure it's merged
  PR_STATE=$(gh pr view "$TAG_TEST_PR" -R "$GITHUB_USER/$REPO_NAME" --json state -q .state)
  if [ "$PR_STATE" != "MERGED" ]; then
    echo "Merging PR #$TAG_TEST_PR first..."
    gh pr merge "$TAG_TEST_PR" -R "$GITHUB_USER/$REPO_NAME" --merge --delete-branch || true
  fi

  TAG_NAME="v1.0.0-test"

  echo "Step 1: Add label to create branch with tag"
  echo "  Using: pronto/tagged-branch..main?tag=$TAG_NAME"
  gh pr edit "$TAG_TEST_PR" -R "$GITHUB_USER/$REPO_NAME" --add-label "pronto/tagged-branch..main?tag=$TAG_NAME"

  echo ""
  echo "Step 2: Wait for workflow"
  sleep 15

  echo ""
  echo "Step 3: Verify branch and tag were created"

  if gh api "repos/$GITHUB_USER/$REPO_NAME/git/refs/heads/tagged-branch" &>/dev/null; then
    echo "  ✅ Branch 'tagged-branch' was created!"
  fi

  if gh api "repos/$GITHUB_USER/$REPO_NAME/git/refs/tags/$TAG_NAME" &>/dev/null; then
    echo "  ✅ Tag '$TAG_NAME' was created!"
  else
    echo "  ⏳ Tag not found yet. May require manual creation after PR merge."
    echo "  Check the cherry-pick PR for tag creation instructions."
  fi

  echo ""
  echo "Expected result:"
  echo "  ✅ New branch 'tagged-branch' created from 'main'"
  echo "  ✅ Commits cherry-picked to the new branch"
  echo "  ✅ Tag '$TAG_NAME' created (or instructions provided)"
fi

pause

# Test 7: Tag Creation on Issue Close
run_test "Tag Creation on Issue Close" \
"This test verifies that closing an issue can trigger tag creation.
Uses a tracking issue with tag notation in the label."

# Create an issue with a tag in the label
RELEASE_TAG="v1.0.0"
ISSUE_BODY="Release $RELEASE_TAG backports

Cherry-pick completed PRs to create a tagged release."

echo "Step 1: Create release issue with tag notation"
echo "  Using label: pronto/release-1.0?tag=$RELEASE_TAG"

RELEASE_ISSUE_URL=$(gh issue create -R "$GITHUB_USER/$REPO_NAME" \
  --title "Release $RELEASE_TAG" \
  --body "$ISSUE_BODY" \
  --label "pronto/release-1.0?tag=$RELEASE_TAG")

RELEASE_ISSUE=$(echo "$RELEASE_ISSUE_URL" | sed 's|.*/issues/||')
echo "  ✅ Created issue #$RELEASE_ISSUE"

echo ""
echo "Step 2: Add a merged PR to the issue body"
# Get any merged PR
MERGED_PR=$(gh pr list -R "$GITHUB_USER/$REPO_NAME" --state merged --limit 1 | awk '{print $1}' | head -1)

if [ -n "$MERGED_PR" ]; then
  # Update issue body to include the PR
  gh issue edit "$RELEASE_ISSUE" -R "$GITHUB_USER/$REPO_NAME" --body "Release $RELEASE_TAG backports

Cherry-pick #$MERGED_PR"
  echo "  Updated issue body to include PR #$MERGED_PR"
fi

echo ""
echo "Step 3: Wait for cherry-pick to complete (if any)"
sleep 10

echo ""
echo "Step 4: Close the issue to trigger tag creation"
gh issue close "$RELEASE_ISSUE" -R "$GITHUB_USER/$REPO_NAME" --comment "Cherry-picks complete, creating release tag"
echo "  ✅ Closed issue #$RELEASE_ISSUE"

echo ""
echo "Step 5: Wait for potential tag creation"
sleep 10

echo ""
echo "Step 6: Check if tag was created"
if gh api "repos/$GITHUB_USER/$REPO_NAME/git/refs/tags/$RELEASE_TAG" &>/dev/null; then
  echo "  ✅ Tag '$RELEASE_TAG' was created successfully!"
else
  echo "  ⏳ Tag not found. This feature may need to be implemented."
  echo "  Note: Tag creation on issue close is a planned feature"
fi

echo ""
echo "Expected result:"
echo "  ✅ Issue closed successfully"
echo "  ✅ Tag '$RELEASE_TAG' created on target branch (or feature to be implemented)"
echo "  ℹ️  If not implemented, this test demonstrates the expected workflow"

pause

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎉 Testing Complete!"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "📊 Repository Overview:"
gh repo view "$GITHUB_USER/$REPO_NAME"

echo ""
echo "🔍 Quick Links:"
echo "  Repository: https://github.com/$GITHUB_USER/$REPO_NAME"
echo "  Actions: https://github.com/$GITHUB_USER/$REPO_NAME/actions"
echo "  Pull Requests: https://github.com/$GITHUB_USER/$REPO_NAME/pulls"
echo "  Issues: https://github.com/$GITHUB_USER/$REPO_NAME/issues"

echo ""
echo "🧹 Cleanup (when done testing):"
echo "  gh repo delete $GITHUB_USER/$REPO_NAME --yes"
