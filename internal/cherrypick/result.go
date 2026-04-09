package cherrypick

// Result tracks the result of a cherry-pick operation for a single PR+branch combination.
type Result struct {
	PRNumber     int
	TargetBranch string
	Success      bool
	Status       string // "success", "failed", "skipped", "conflict", "pending_pr"
	Message      string
	CreatedPR    int // PR number if a new PR was created
}
