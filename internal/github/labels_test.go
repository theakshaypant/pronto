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
		wantTag      string
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
		// Tag notation tests
		{
			name:       "simple branch with tag",
			spec:       "release-1.0?tag=v1.0.1",
			wantName:   "release-1.0",
			wantBase:   "",
			wantCreate: false,
			wantTag:    "v1.0.1",
		},
		{
			name:       "branch creation with tag",
			spec:       "release-1.0..main?tag=v1.0.0",
			wantName:   "release-1.0",
			wantBase:   "main",
			wantCreate: true,
			wantTag:    "v1.0.0",
		},
		{
			name:       "tag with semantic version",
			spec:       "release-2.0?tag=v2.0.0-beta.1",
			wantName:   "release-2.0",
			wantBase:   "",
			wantCreate: false,
			wantTag:    "v2.0.0-beta.1",
		},
		{
			name:       "tag with custom name",
			spec:       "hotfix-1.5?tag=hotfix-123",
			wantName:   "hotfix-1.5",
			wantBase:   "",
			wantCreate: false,
			wantTag:    "hotfix-123",
		},
		{
			name:       "branch with @ and tag",
			spec:       "release@user-1.0?tag=v1.0.1",
			wantName:   "release@user-1.0",
			wantBase:   "",
			wantCreate: false,
			wantTag:    "v1.0.1",
		},
		{
			name:       "branch creation with @ and tag",
			spec:       "release-1.0..feature@alice?tag=v1.0.0",
			wantName:   "release-1.0",
			wantBase:   "feature@alice",
			wantCreate: true,
			wantTag:    "v1.0.0",
		},
		{
			name:    "invalid - empty tag",
			spec:    "release-1.0?tag=",
			wantNil: true,
		},
		{
			name:    "invalid - tag with spaces",
			spec:    "release-1.0?tag=v1.0.1 beta",
			wantNil: true,
		},
		{
			name:    "invalid - tag starts with period",
			spec:    "release-1.0?tag=.hidden",
			wantNil: true,
		},
		{
			name:    "invalid - tag starts with hyphen",
			spec:    "release-1.0?tag=-invalid",
			wantNil: true,
		},
		{
			name:    "invalid - tag contains ..",
			spec:    "release-1.0?tag=v1..0",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseBranchSpec(tt.spec)

			if tt.wantNil {
				if result != nil {
					t.Errorf("ParseBranchSpec() = %+v, want nil", result)
				}
				return
			}

			if result == nil {
				t.Fatalf("ParseBranchSpec() = nil, want non-nil")
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

			if result.TagName != tt.wantTag {
				t.Errorf("TagName = %q, want %q", result.TagName, tt.wantTag)
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
		{
			name: "branches with tags",
			labels: []*github.Label{
				{Name: github.Ptr("pronto/release-1.0?tag=v1.0.1")},
				{Name: github.Ptr("pronto/release-2.0?tag=v2.0.0")},
			},
			pattern: "pronto/",
			want:    2,
			checks: []func(*testing.T, []*models.TargetBranch){
				func(t *testing.T, branches []*models.TargetBranch) {
					if branches[0].TagName != "v1.0.1" {
						t.Errorf("First branch TagName = %q, want v1.0.1", branches[0].TagName)
					}
					if branches[1].TagName != "v2.0.0" {
						t.Errorf("Second branch TagName = %q, want v2.0.0", branches[1].TagName)
					}
				},
			},
		},
		{
			name: "branch creation with tag",
			labels: []*github.Label{
				{Name: github.Ptr("pronto/release-1.0..main?tag=v1.0.0")},
			},
			pattern: "pronto/",
			want:    1,
			checks: []func(*testing.T, []*models.TargetBranch){
				func(t *testing.T, branches []*models.TargetBranch) {
					if !branches[0].ShouldCreate {
						t.Error("Branch should have ShouldCreate=true")
					}
					if branches[0].BaseBranch != "main" {
						t.Errorf("Branch BaseBranch = %q, want main", branches[0].BaseBranch)
					}
					if branches[0].TagName != "v1.0.0" {
						t.Errorf("Branch TagName = %q, want v1.0.0", branches[0].TagName)
					}
				},
			},
		},
		{
			name: "mixed branches with and without tags",
			labels: []*github.Label{
				{Name: github.Ptr("pronto/release-1.0?tag=v1.0.1")},
				{Name: github.Ptr("pronto/release-2.0")}, // no tag
				{Name: github.Ptr("pronto/hotfix-1.5..release-1.0?tag=hotfix-123")},
			},
			pattern: "pronto/",
			want:    3,
			checks: []func(*testing.T, []*models.TargetBranch){
				func(t *testing.T, branches []*models.TargetBranch) {
					// First branch should have tag
					if branches[0].TagName != "v1.0.1" {
						t.Errorf("First branch TagName = %q, want v1.0.1", branches[0].TagName)
					}
					// Second branch should NOT have tag
					if branches[1].TagName != "" {
						t.Errorf("Second branch TagName = %q, want empty", branches[1].TagName)
					}
					// Third branch should have tag and base
					if branches[2].TagName != "hotfix-123" {
						t.Errorf("Third branch TagName = %q, want hotfix-123", branches[2].TagName)
					}
					if branches[2].BaseBranch != "release-1.0" {
						t.Errorf("Third branch BaseBranch = %q, want release-1.0", branches[2].BaseBranch)
					}
				},
			},
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
