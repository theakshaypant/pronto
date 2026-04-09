package cli

import "testing"

func TestParseRepoURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "HTTPS with .git",
			url:       "https://github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS without .git",
			url:       "https://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "SSH format with .git",
			url:       "git@github.com:owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "SSH format without .git",
			url:       "git@github.com:owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "SSH protocol URL with .git",
			url:       "ssh://git@github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "SSH protocol URL without .git",
			url:       "ssh://git@github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS with hyphenated names",
			url:       "https://github.com/my-org/my-repo.git",
			wantOwner: "my-org",
			wantRepo:  "my-repo",
		},
		{
			name:      "SSH with hyphenated names",
			url:       "git@github.com:my-org/my-repo.git",
			wantOwner: "my-org",
			wantRepo:  "my-repo",
		},
		{
			name:      "SSH with different host",
			url:       "git@gitlab.com:owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "HTTPS with different host",
			url:       "https://gitlab.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "underscores in names",
			url:       "https://github.com/my_org/my_repo.git",
			wantOwner: "my_org",
			wantRepo:  "my_repo",
		},
		{
			name:    "invalid URL",
			url:     "not-a-url",
			wantErr: true,
		},
		{
			name:    "just a path",
			url:     "/home/user/repo",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
		{
			name:    "HTTP URL without path",
			url:     "https://github.com",
			wantErr: true,
		},
		{
			name:    "HTTP URL with only owner",
			url:     "https://github.com/owner",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ParseRepoURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseRepoURL(%q) error = nil, want error", tt.url)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseRepoURL(%q) unexpected error: %v", tt.url, err)
			}

			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestInferCloneURL(t *testing.T) {
	tests := []struct {
		owner string
		repo  string
		want  string
	}{
		{"theakshaypant", "pronto", "https://github.com/theakshaypant/pronto.git"},
		{"my-org", "my-repo", "https://github.com/my-org/my-repo.git"},
	}

	for _, tt := range tests {
		t.Run(tt.owner+"/"+tt.repo, func(t *testing.T) {
			got := InferCloneURL(tt.owner, tt.repo)
			if got != tt.want {
				t.Errorf("InferCloneURL(%q, %q) = %q, want %q", tt.owner, tt.repo, got, tt.want)
			}
		})
	}
}
