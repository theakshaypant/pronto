package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/theakshaypant/pronto/internal/cherrypick"
	ghclient "github.com/theakshaypant/pronto/internal/github"
	"github.com/theakshaypant/pronto/pkg/models"
)

func newCherryPickCommand() *cobra.Command {
	var (
		prNumbers []int
		targets   []string
		repo      string
		createPR  bool
		dryRun    bool
		noComment bool
	)

	cmd := &cobra.Command{
		Use:   "cherry-pick",
		Short: "Cherry-pick merged PR(s) to target branch(es)",
		Long: `Cherry-pick one or more merged PRs to one or more target branches.

The --to flag supports the same spec syntax as pronto labels:
  release-1.0              existing branch
  release-1.0..main        create release-1.0 from main, then cherry-pick
  release-1.0?tag=v1.0.1   cherry-pick and create tag v1.0.1
  release-1.0..main?tag=v1.0.0  create branch, cherry-pick, and tag`,
		Example: `  pronto cherry-pick --pr 123 --to release-1.0
  pronto cherry-pick --pr 123 --to release-1.0 --to release-2.0
  pronto cherry-pick --pr 123 --pr 456 --to release-1.0 --to release-2.0
  pronto cherry-pick --pr 123 --to "release-1.0..main?tag=v1.0.1"
  pronto cherry-pick --pr 123 --to release-1.0 --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCherryPick(cmd, prNumbers, targets, repo, createPR, dryRun, noComment)
		},
	}

	cmd.Flags().IntSliceVarP(&prNumbers, "pr", "p", nil, "PR number(s) to cherry-pick, repeatable (required)")
	cmd.Flags().StringSliceVarP(&targets, "to", "t", nil, "target branch spec(s), repeatable (required)")
	cmd.Flags().StringVar(&repo, "repo", "", "owner/repo override (default: auto-detected from git remote)")
	cmd.Flags().BoolVar(&createPR, "create-pr", false, "force PR creation instead of direct push")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would happen without executing")
	cmd.Flags().BoolVar(&noComment, "no-comment", false, "skip posting GitHub comment")

	cmd.MarkFlagRequired("pr")
	cmd.MarkFlagRequired("to")

	return cmd
}

func runCherryPick(cmd *cobra.Command, prNumbers []int, targets []string, repoFlag string, createPR bool, dryRun bool, noComment bool) error {
	ctx := context.Background()
	p := getPrinter(cmd)

	// Resolve config
	cfg, err := resolveConfig(cmd)
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	token, err := cfg.ResolveToken()
	if err != nil {
		return err
	}

	// Resolve repo
	owner, repoName, err := resolveRepo(repoFlag, p)
	if err != nil {
		return err
	}

	// Parse target branch specs (same syntax as pronto labels)
	targetBranches, err := parseTargetSpecs(targets)
	if err != nil {
		return err
	}

	// Create GitHub client
	client, err := ghclient.NewClient(ctx, token, owner, repoName)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	if len(prNumbers) > 1 {
		p.Header("Cherry-pick: %d PR(s) x %d branch(es) = %d operations",
			len(prNumbers), len(targetBranches), len(prNumbers)*len(targetBranches))
		fmt.Println()
	}

	// Fetch and validate all PRs
	type prData struct {
		number         int
		commitSHAs     []string
		commitMessages []string
	}
	var prs []prData

	for _, prNumber := range prNumbers {
		p.Header("Fetching PR #%d...", prNumber)
		pr, err := client.GetPullRequest(ctx, prNumber)
		if err != nil {
			p.Error("  Failed to fetch: %v", err)
			continue
		}

		if !pr.GetMerged() {
			p.Warn("  Not merged, skipping")
			continue
		}

		p.Info("  Title:  %s", pr.GetTitle())

		commits, err := client.GetPullRequestCommits(ctx, prNumber)
		if err != nil {
			p.Error("  Failed to fetch commits: %v", err)
			continue
		}

		commitSHAs := make([]string, len(commits))
		commitMessages := make([]string, len(commits))
		for i, c := range commits {
			commitSHAs[i] = c.GetSHA()
			commitMessages[i] = fmt.Sprintf("%s (%s)", c.Commit.GetMessage(), c.GetSHA()[:7])
		}

		p.Info("  Commits: %d", len(commitSHAs))
		prs = append(prs, prData{number: prNumber, commitSHAs: commitSHAs, commitMessages: commitMessages})
	}

	if len(prs) == 0 {
		return fmt.Errorf("no valid merged PRs to process")
	}

	fmt.Println()

	// Create cherry-pick service
	svc := cherrypick.NewService(ctx, client)
	svc.SetProgressFunc(func(step string) {
		p.Detail("%s", step)
	})

	// Process each PR x branch combination
	var allResults []cherrypick.Result
	for _, pr := range prs {
		for _, target := range targetBranches {
			label := fmt.Sprintf("PR #%d -> %s", pr.number, target.Name)
			if target.ShouldCreate {
				label += " (new from " + target.BaseBranch + ")"
			}
			if target.TagName != "" {
				label += " + tag " + target.TagName
			}
			p.Header("Cherry-picking %s...", label)

			opts := cherrypick.CherryPickOptions{
				Owner:          owner,
				Repo:           repoName,
				CloneURL:       InferCloneURL(owner, repoName),
				Token:          token,
				PRNumber:       pr.number,
				TargetBranch:   target,
				CommitSHAs:     pr.commitSHAs,
				CommitMessages: pr.commitMessages,
				HasWriteAccess: !createPR,
				AlwaysCreatePR: createPR || cfg.AlwaysCreatePR,
				DryRun:         dryRun,
				BotName:        cfg.BotName,
				BotEmail:       cfg.BotEmail,
				ConflictLabel:  cfg.ConflictLabel,
				LabelPattern:   cfg.LabelPattern,
			}

			result := svc.CherryPickToTarget(opts)
			allResults = append(allResults, result)

			if result.Success {
				p.Success("  %s", result.Message)
			} else if result.Status == "conflict" {
				p.Warn("  %s", result.Message)
			} else {
				p.Error("  %s", result.Message)
			}
			fmt.Println()
		}
	}

	// Print summary table if multiple operations
	if len(allResults) > 1 {
		p.Header("Summary")
		headers := []string{"PR", "Branch", "Status", "Details"}
		var rows [][]string
		for _, r := range allResults {
			rows = append(rows, []string{
				fmt.Sprintf("#%d", r.PRNumber),
				r.TargetBranch,
				statusIcon(r.Status),
				r.Message,
			})
		}
		p.Table(headers, rows)
	}

	// Post summary comments unless --no-comment
	if !noComment && !dryRun {
		// Group results by PR for per-PR comments
		byPR := map[int][]cherrypick.Result{}
		for _, r := range allResults {
			byPR[r.PRNumber] = append(byPR[r.PRNumber], r)
		}
		for prNum, results := range byPR {
			postSummaryComment(ctx, client, prNum, results, p)
		}
	}

	// Check for any failures
	for _, r := range allResults {
		if !r.Success && r.Status != "conflict" {
			return fmt.Errorf("one or more cherry-picks failed")
		}
	}

	return nil
}

func statusIcon(status string) string {
	switch status {
	case "success":
		return "OK"
	case "pending_pr":
		return "PR"
	case "conflict":
		return "CONFLICT"
	case "skipped":
		return "SKIP"
	default:
		return "FAIL"
	}
}

// parseTargetSpecs parses --to flag values using the same spec syntax as pronto labels.
func parseTargetSpecs(specs []string) ([]*models.TargetBranch, error) {
	var targets []*models.TargetBranch
	for _, spec := range specs {
		tb := ghclient.ParseBranchSpec(spec)
		if tb == nil {
			return nil, fmt.Errorf("invalid branch spec %q — use format: branch, branch..base, branch?tag=name, or branch..base?tag=name", spec)
		}
		targets = append(targets, tb)
	}
	return targets, nil
}

// resolveRepo resolves owner/repo from the --repo flag or git remote.
func resolveRepo(repoFlag string, p *Printer) (owner, repo string, err error) {
	if repoFlag != "" {
		parts := splitRepo(repoFlag)
		if parts == nil {
			return "", "", fmt.Errorf("invalid --repo format %q, expected owner/repo", repoFlag)
		}
		return parts[0], parts[1], nil
	}

	p.Detail("Inferring repository from git remote...")
	owner, repo, err = InferRepo()
	if err != nil {
		return "", "", fmt.Errorf("could not detect repository: %w\nUse --repo owner/repo to specify manually", err)
	}
	p.Detail("Detected %s/%s", owner, repo)
	return owner, repo, nil
}

func splitRepo(s string) []string {
	for i, c := range s {
		if c == '/' {
			if i > 0 && i < len(s)-1 {
				return []string{s[:i], s[i+1:]}
			}
			return nil
		}
	}
	return nil
}

// postSummaryComment posts a summary comment on the PR.
func postSummaryComment(ctx context.Context, client *ghclient.Client, prNumber int, results []cherrypick.Result, p *Printer) {
	var body string
	body = "## 🤖 PROnto Summary\n\n"
	for _, r := range results {
		body += fmt.Sprintf("**`%s`**: %s\n\n", r.TargetBranch, r.Message)
	}
	body += "---\n🤖 Automated by [PROnto](https://github.com/theakshaypant/pronto)"

	if err := client.AddComment(ctx, prNumber, body); err != nil {
		p.Warn("Failed to post summary comment: %v", err)
	}
}
