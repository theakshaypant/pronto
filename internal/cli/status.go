package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/go-github/v81/github"
	"github.com/spf13/cobra"
	ghclient "github.com/theakshaypant/pronto/internal/github"
)

func newStatusCommand() *cobra.Command {
	var (
		prNumber int
		branch   string
		repo     string
		format   string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Query cherry-pick status for a PR or branch",
		Example: `  pronto status --pr 123
  pronto status --branch release-1.0
  pronto status --pr 123 --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if prNumber == 0 && branch == "" {
				return fmt.Errorf("specify --pr or --branch")
			}
			return runStatus(cmd, prNumber, branch, repo, format)
		},
	}

	cmd.Flags().IntVarP(&prNumber, "pr", "p", 0, "show cherry-pick status for a specific PR")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "show cherry-pick PRs targeting a specific branch")
	cmd.Flags().StringVar(&repo, "repo", "", "owner/repo override (default: auto-detected)")
	cmd.Flags().StringVarP(&format, "format", "f", "table", "output format: table, json")

	return cmd
}

type statusEntry struct {
	PR           int    `json:"pr"`
	Title        string `json:"title"`
	TargetBranch string `json:"target_branch"`
	State        string `json:"state"`
	URL          string `json:"url"`
	HasConflict  bool   `json:"has_conflict"`
}

func runStatus(cmd *cobra.Command, prNumber int, branch string, repoFlag string, format string) error {
	ctx := context.Background()
	p := getPrinter(cmd)

	cfg, err := resolveConfig(cmd)
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	token, err := cfg.ResolveToken()
	if err != nil {
		return err
	}

	owner, repoName, err := resolveRepo(repoFlag, p)
	if err != nil {
		return err
	}

	client, err := ghclient.NewClient(ctx, token, owner, repoName)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Search for pronto cherry-pick PRs
	var query string
	if prNumber > 0 {
		query = fmt.Sprintf("repo:%s/%s is:pr label:pronto \"Cherry-pick PR #%d\" in:title", owner, repoName, prNumber)
	} else {
		query = fmt.Sprintf("repo:%s/%s is:pr label:pronto base:%s in:title \"[PROnto]\"", owner, repoName, branch)
	}

	ghClient := client.GetClient()
	searchResult, _, err := ghClient.Search.Issues(ctx, query, &github.SearchOptions{
		Sort:  "created",
		Order: "desc",
		ListOptions: github.ListOptions{
			PerPage: 50,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to search for cherry-pick PRs: %w", err)
	}

	var entries []statusEntry
	for _, issue := range searchResult.Issues {
		entry := statusEntry{
			PR:          issue.GetNumber(),
			Title:       issue.GetTitle(),
			State:       issue.GetState(),
			URL:         issue.GetHTMLURL(),
			HasConflict: hasLabel(issue.Labels, cfg.ConflictLabel),
		}

		// Extract target branch from PR base (if it's a PR)
		if issue.PullRequestLinks != nil {
			// For PRs found via search, the base branch is in the title
			entry.TargetBranch = extractTargetFromTitle(issue.GetTitle())
		}

		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		if prNumber > 0 {
			p.Info("No cherry-pick PRs found for PR #%d", prNumber)
		} else {
			p.Info("No cherry-pick PRs found targeting %s", branch)
		}
		return nil
	}

	// Output
	switch format {
	case "json":
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(data))
	default:
		if prNumber > 0 {
			p.Header("Cherry-pick PRs for PR #%d", prNumber)
		} else {
			p.Header("Cherry-pick PRs targeting %s", branch)
		}
		fmt.Println()

		headers := []string{"PR", "Target", "State", "Conflict", "Title"}
		var rows [][]string
		for _, e := range entries {
			conflict := ""
			if e.HasConflict {
				conflict = "yes"
			}
			rows = append(rows, []string{
				fmt.Sprintf("#%d", e.PR),
				e.TargetBranch,
				e.State,
				conflict,
				truncate(e.Title, 50),
			})
		}
		p.Table(headers, rows)
	}

	return nil
}

func hasLabel(labels []*github.Label, name string) bool {
	for _, l := range labels {
		if l.GetName() == name {
			return true
		}
	}
	return false
}

// extractTargetFromTitle extracts the target branch from a PROnto PR title.
// Format: "[PROnto] Cherry-pick PR #123 to release-1.0"
func extractTargetFromTitle(title string) string {
	prefix := " to "
	idx := strings.LastIndex(title, prefix)
	if idx == -1 {
		return ""
	}
	target := title[idx+len(prefix):]
	// Remove trailing " (CONFLICTS)" if present
	target = strings.TrimSuffix(target, " (CONFLICTS)")
	return target
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
