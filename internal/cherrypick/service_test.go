package cherrypick

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-github/v81/github"
	ghclient "github.com/theakshaypant/pronto/internal/github"
	"github.com/theakshaypant/pronto/pkg/models"
)

// newTestService creates a Service backed by an httptest server.
// The handler receives all GitHub API requests.
func newTestService(t *testing.T, handler http.Handler) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	ghClient := github.NewClient(nil)
	baseURL, _ := url.Parse(server.URL + "/")
	ghClient.BaseURL = baseURL

	client := ghclient.NewTestClient(ghClient, "test-owner", "test-repo")
	return NewService(context.Background(), client)
}

// branchMux returns an http.ServeMux that responds to GetBranch requests.
// branches maps branch name → exists.
func branchMux(branches map[string]bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Match: GET /repos/{owner}/{repo}/branches/{branch}
		path := r.URL.Path
		prefix := "/repos/test-owner/test-repo/branches/"
		if r.Method == "GET" && strings.HasPrefix(path, prefix) {
			branch := strings.TrimPrefix(path, prefix)
			if branches[branch] {
				fmt.Fprintf(w, `{"name":"%s","commit":{"sha":"abc123"}}`, branch)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Branch not found"}`)
			return
		}

		// Match: POST /repos/{owner}/{repo}/git/refs (CreateBranch)
		if r.Method == "POST" && path == "/repos/test-owner/test-repo/git/refs" {
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"ref":"refs/heads/new-branch","object":{"sha":"abc123"}}`)
			return
		}

		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"message":"not found: %s"}`, path)
	})
	return mux
}

func TestCherryPickToTarget_DryRun_ExistingBranch(t *testing.T) {
	svc := newTestService(t, branchMux(map[string]bool{"release-1.0": true}))

	result := svc.CherryPickToTarget(CherryPickOptions{
		Owner:    "test-owner",
		Repo:     "test-repo",
		PRNumber: 42,
		TargetBranch: &models.TargetBranch{
			Name: "release-1.0",
		},
		CommitSHAs:     []string{"aaa", "bbb"},
		CommitMessages: []string{"fix: one", "fix: two"},
		HasWriteAccess: true,
		DryRun:         true,
	})

	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Message)
	}
	if result.Status != "success" {
		t.Errorf("status = %q, want %q", result.Status, "success")
	}
	if result.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", result.PRNumber)
	}
	if result.TargetBranch != "release-1.0" {
		t.Errorf("TargetBranch = %q, want %q", result.TargetBranch, "release-1.0")
	}
	if !strings.Contains(result.Message, "dry-run") {
		t.Errorf("message should mention dry-run: %s", result.Message)
	}
	if !strings.Contains(result.Message, "2 commit(s)") {
		t.Errorf("message should mention commit count: %s", result.Message)
	}
	if !strings.Contains(result.Message, "direct push") {
		t.Errorf("message should say direct push for write access without always_create_pr: %s", result.Message)
	}
}

func TestCherryPickToTarget_DryRun_ViaPR(t *testing.T) {
	svc := newTestService(t, branchMux(map[string]bool{"release-1.0": true}))

	result := svc.CherryPickToTarget(CherryPickOptions{
		Owner:    "test-owner",
		Repo:     "test-repo",
		PRNumber: 42,
		TargetBranch: &models.TargetBranch{
			Name: "release-1.0",
		},
		CommitSHAs:     []string{"aaa"},
		CommitMessages: []string{"fix: one"},
		AlwaysCreatePR: true,
		HasWriteAccess: true,
		DryRun:         true,
	})

	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Message)
	}
	if !strings.Contains(result.Message, "via PR") {
		t.Errorf("message should say via PR when always_create_pr=true: %s", result.Message)
	}
}

func TestCherryPickToTarget_DryRun_NoWriteAccess(t *testing.T) {
	svc := newTestService(t, branchMux(map[string]bool{"release-1.0": true}))

	result := svc.CherryPickToTarget(CherryPickOptions{
		Owner:    "test-owner",
		Repo:     "test-repo",
		PRNumber: 42,
		TargetBranch: &models.TargetBranch{
			Name: "release-1.0",
		},
		CommitSHAs:     []string{"aaa"},
		CommitMessages: []string{"fix: one"},
		HasWriteAccess: false,
		DryRun:         true,
	})

	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Message)
	}
	if !strings.Contains(result.Message, "via PR") {
		t.Errorf("message should say via PR when no write access: %s", result.Message)
	}
}

func TestCherryPickToTarget_BranchDoesNotExist_NoCreate(t *testing.T) {
	svc := newTestService(t, branchMux(map[string]bool{}))

	result := svc.CherryPickToTarget(CherryPickOptions{
		Owner:    "test-owner",
		Repo:     "test-repo",
		PRNumber: 42,
		TargetBranch: &models.TargetBranch{
			Name:         "nonexistent",
			ShouldCreate: false,
		},
		CommitSHAs:     []string{"aaa"},
		CommitMessages: []string{"fix: one"},
		LabelPattern:   "pronto/",
		DryRun:         true,
	})

	if result.Success {
		t.Fatal("expected failure for non-existent branch without create")
	}
	if result.Status != "failed" {
		t.Errorf("status = %q, want %q", result.Status, "failed")
	}
	if !strings.Contains(result.Message, "does not exist") {
		t.Errorf("message should say branch does not exist: %s", result.Message)
	}
}

func TestCherryPickToTarget_DryRun_CreateBranch(t *testing.T) {
	svc := newTestService(t, branchMux(map[string]bool{
		"main": true, // base branch exists
		// "release-2.0" does not exist → triggers creation
	}))

	result := svc.CherryPickToTarget(CherryPickOptions{
		Owner:    "test-owner",
		Repo:     "test-repo",
		PRNumber: 42,
		TargetBranch: &models.TargetBranch{
			Name:         "release-2.0",
			BaseBranch:   "main",
			ShouldCreate: true,
		},
		CommitSHAs:     []string{"aaa"},
		CommitMessages: []string{"fix: one"},
		HasWriteAccess: true,
		DryRun:         true,
	})

	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Message)
	}
	if !strings.Contains(result.Message, "dry-run") {
		t.Errorf("message should mention dry-run: %s", result.Message)
	}
}

func TestCherryPickToTarget_CreateBranch_BaseMissing(t *testing.T) {
	svc := newTestService(t, branchMux(map[string]bool{})) // no branches exist

	result := svc.CherryPickToTarget(CherryPickOptions{
		Owner:    "test-owner",
		Repo:     "test-repo",
		PRNumber: 42,
		TargetBranch: &models.TargetBranch{
			Name:         "release-2.0",
			BaseBranch:   "nonexistent-base",
			ShouldCreate: true,
		},
		CommitSHAs:     []string{"aaa"},
		CommitMessages: []string{"fix: one"},
		DryRun:         true,
	})

	if result.Success {
		t.Fatal("expected failure when base branch doesn't exist")
	}
	if !strings.Contains(result.Message, "base branch") {
		t.Errorf("message should mention base branch: %s", result.Message)
	}
}

func TestCherryPickToTarget_ProgressCallback(t *testing.T) {
	svc := newTestService(t, branchMux(map[string]bool{"release-1.0": true}))

	var steps []string
	svc.SetProgressFunc(func(step string) {
		steps = append(steps, step)
	})

	svc.CherryPickToTarget(CherryPickOptions{
		Owner:    "test-owner",
		Repo:     "test-repo",
		PRNumber: 42,
		TargetBranch: &models.TargetBranch{
			Name: "release-1.0",
		},
		CommitSHAs:     []string{"aaa"},
		CommitMessages: []string{"fix: one"},
		DryRun:         true,
	})

	if len(steps) == 0 {
		t.Error("progress callback was never called")
	}

	foundBranchCheck := false
	for _, s := range steps {
		if strings.Contains(s, "release-1.0") {
			foundBranchCheck = true
		}
	}
	if !foundBranchCheck {
		t.Errorf("progress should mention target branch, got: %v", steps)
	}
}

func TestCherryPickBatch_DryRun(t *testing.T) {
	svc := newTestService(t, branchMux(map[string]bool{
		"release-1.0": true,
		"release-2.0": true,
	}))

	results := svc.CherryPickBatch(
		BatchOptions{
			Owner:    "test-owner",
			Repo:     "test-repo",
			CloneURL: "https://github.com/test-owner/test-repo.git",
			Token:    "test-token",
			TargetBranches: []*models.TargetBranch{
				{Name: "release-1.0"},
				{Name: "release-2.0"},
			},
			HasWriteAccess: true,
			DryRun:         true,
			BotName:        "Test Bot",
			BotEmail:       "test@bot.com",
		},
		[]PRInput{
			{PRNumber: 10, CommitSHAs: []string{"aaa"}, CommitMessages: []string{"fix: a"}},
			{PRNumber: 20, CommitSHAs: []string{"bbb"}, CommitMessages: []string{"fix: b"}},
		},
	)

	// 2 PRs × 2 branches = 4 results
	if len(results) != 4 {
		t.Fatalf("got %d results, want 4", len(results))
	}

	// Verify all are dry-run successes
	for i, r := range results {
		if !r.Success {
			t.Errorf("result[%d] failed: %s", i, r.Message)
		}
		if !strings.Contains(r.Message, "dry-run") {
			t.Errorf("result[%d] should mention dry-run: %s", i, r.Message)
		}
	}

	// Verify PR×branch matrix coverage
	type key struct {
		pr     int
		branch string
	}
	seen := make(map[key]bool)
	for _, r := range results {
		seen[key{r.PRNumber, r.TargetBranch}] = true
	}

	expected := []key{
		{10, "release-1.0"}, {10, "release-2.0"},
		{20, "release-1.0"}, {20, "release-2.0"},
	}
	for _, k := range expected {
		if !seen[k] {
			t.Errorf("missing result for PR #%d → %s", k.pr, k.branch)
		}
	}
}

func TestCherryPickBatch_PartialFailure(t *testing.T) {
	svc := newTestService(t, branchMux(map[string]bool{
		"release-1.0": true,
		// "release-2.0" does not exist
	}))

	results := svc.CherryPickBatch(
		BatchOptions{
			Owner:    "test-owner",
			Repo:     "test-repo",
			TargetBranches: []*models.TargetBranch{
				{Name: "release-1.0"},
				{Name: "release-2.0"},
			},
			HasWriteAccess: true,
			DryRun:         true,
			LabelPattern:   "pronto/",
		},
		[]PRInput{
			{PRNumber: 10, CommitSHAs: []string{"aaa"}, CommitMessages: []string{"fix: a"}},
		},
	)

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// First should succeed (branch exists)
	if !results[0].Success {
		t.Errorf("result[0] should succeed: %s", results[0].Message)
	}

	// Second should fail (branch doesn't exist, no create)
	if results[1].Success {
		t.Errorf("result[1] should fail for non-existent branch")
	}
}

func TestNewService(t *testing.T) {
	ctx := context.Background()
	ghClient := github.NewClient(nil)
	client := ghclient.NewTestClient(ghClient, "owner", "repo")

	svc := NewService(ctx, client)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.progress != nil {
		t.Error("progress should be nil initially")
	}
}

func TestSetProgressFunc(t *testing.T) {
	ctx := context.Background()
	ghClient := github.NewClient(nil)
	client := ghclient.NewTestClient(ghClient, "owner", "repo")
	svc := NewService(ctx, client)

	called := false
	svc.SetProgressFunc(func(step string) {
		called = true
	})

	svc.report("test")
	if !called {
		t.Error("progress func was not called")
	}
}

func TestReport_NilProgress(t *testing.T) {
	ctx := context.Background()
	ghClient := github.NewClient(nil)
	client := ghclient.NewTestClient(ghClient, "owner", "repo")
	svc := NewService(ctx, client)

	// Should not panic with nil progress
	svc.report("test")
}
