package models

// TargetBranch represents a target branch for cherry-picking.
type TargetBranch struct {
	// Name is the target branch name
	Name string

	// BaseBranch is the base branch to create from (if specified)
	BaseBranch string

	// ShouldCreate indicates if the branch should be created
	ShouldCreate bool
}

// NewTargetBranch creates a TargetBranch from a branch string.
// Supports formats:
//   - "release-1.0" - existing branch
//   - "release-1.0..main" - create release-1.0 from main
func NewTargetBranch(branchStr string) *TargetBranch {
	return &TargetBranch{
		Name:         branchStr,
		BaseBranch:   "",
		ShouldCreate: false,
	}
}
