package github

import (
	"testing"

	"github.com/google/go-github/v81/github"
	"github.com/theakshaypant/pronto/pkg/models"
)

func TestParseBranchSpec(t *testing.T) {
	tests := []struct {
		name         string
		spec         string
		wantName     string
		wantBase     string
		wantCreate   bool
		wantNil      bool
	}{
		{
			name:       "simple branch",
			spec:       "release-1.0",
			wantName:   "release-1.0",
			wantBase:   "",
			wantCreate: false,
		},
		{
			name:       "branch with .. notation",
			spec:       "release-1.0..main",
			wantName:   "release-1.0",
			wantBase:   "main",
			wantCreate: true,
		},
		{
			name:       "branch with .. from release branch",
			spec:       "hotfix-2.1..release-2.0",
			wantName:   "hotfix-2.1",
			wantBase:   "release-2.0",
			wantCreate: true,
		},
		{
			name:       "target with @ character",
			spec:       "release@user-1.0..main",
			wantName:   "release@user-1.0",
			wantBase:   "main",
			wantCreate: true,
		},
		{
			name:       "base with @ character",
			spec:       "release-1.0..feature@alice",
			wantName:   "release-1.0",
			wantBase:   "feature@alice",
			wantCreate: true,
		},
		{
			name:    "empty target with ..",
			spec:    "..main",
			wantNil: true,
		},
		{
			name:    "empty base with ..",
			spec:    "release-1.0..",
			wantNil: true,
		},
		{
			name:    "invalid - starts with slash",
			spec:    "/release-1.0",
			wantNil: true,
		},
		{
			name:    "invalid - contains spaces",
			spec:    "release 1.0",
			wantNil: true,
		},
		{
			name:    "invalid - base branch has spaces",
			spec:    "release-1.0..main branch",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseBranchSpec(tt.spec)

			if tt.wantNil {
				if result != nil {
					t.Errorf("parseBranchSpec() = %+v, want nil", result)
				}
				return
			}

			if result == nil {
				t.Fatalf("parseBranchSpec() = nil, want non-nil")
			}

			if result.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", result.Name, tt.wantName)
			}

			if result.BaseBranch != tt.wantBase {
				t.Errorf("BaseBranch = %q, want %q", result.BaseBranch, tt.wantBase)
			}

			if result.ShouldCreate != tt.wantCreate {
				t.Errorf("ShouldCreate = %v, want %v", result.ShouldCreate, tt.wantCreate)
			}
		})
	}
}

func TestParseTargetBranches(t *testing.T) {
	tests := []struct {
		name    string
		labels  []*github.Label
		pattern string
		want    int
		checks  []func(*testing.T, []*models.TargetBranch)
	}{
		{
			name: "simple branches",
			labels: []*github.Label{
				{Name: github.Ptr("pronto/release-1.0")},
				{Name: github.Ptr("pronto/release-2.0")},
			},
			pattern: "pronto/",
			want:    2,
		},
		{
			name: "branches with .. notation",
			labels: []*github.Label{
				{Name: github.Ptr("pronto/release-1.0..main")},
				{Name: github.Ptr("pronto/hotfix-2.1..release-2.0")},
			},
			pattern: "pronto/",
			want:    2,
			checks: []func(*testing.T, []*models.TargetBranch){
				func(t *testing.T, branches []*models.TargetBranch) {
					if !branches[0].ShouldCreate {
						t.Error("First branch should have ShouldCreate=true")
					}
					if branches[0].BaseBranch != "main" {
						t.Errorf("First branch BaseBranch = %q, want main", branches[0].BaseBranch)
					}
				},
			},
		},
		{
			name: "mixed branches",
			labels: []*github.Label{
				{Name: github.Ptr("pronto/release-1.0")},
				{Name: github.Ptr("pronto/release-2.0..main")},
				{Name: github.Ptr("bug")}, // non-pronto label
			},
			pattern: "pronto/",
			want:    2,
		},
		{
			name: "duplicate branches",
			labels: []*github.Label{
				{Name: github.Ptr("pronto/release-1.0")},
				{Name: github.Ptr("pronto/release-1.0")}, // duplicate
			},
			pattern: "pronto/",
			want:    1, // should deduplicate
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseTargetBranches(tt.labels, tt.pattern)

			if len(result) != tt.want {
				t.Errorf("ParseTargetBranches() returned %d branches, want %d", len(result), tt.want)
			}

			for _, check := range tt.checks {
				check(t, result)
			}
		})
	}
}
