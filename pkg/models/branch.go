package models

// TargetBranch represents a target branch for cherry-picking.
type TargetBranch struct {
	// Name is the target branch name
	Name string

	// BaseBranch is the base branch to create from (if specified)
	BaseBranch string

	// ShouldCreate indicates if the branch should be created
	ShouldCreate bool

	// TagName is the optional tag to create after successful cherry-pick
	TagName string
}

// NewTargetBranch creates a TargetBranch from a branch string.
// Supports formats:
//   - "release-1.0" - existing branch
//   - "release-1.0..main" - create release-1.0 from main
//   - "release-1.0?tag=v1.0.1" - existing branch with tag creation
//   - "release-1.0..main?tag=v1.0.0" - create branch with tag
func NewTargetBranch(branchStr string) *TargetBranch {
	return &TargetBranch{
		Name:         branchStr,
		BaseBranch:   "",
		ShouldCreate: false,
		TagName:      "",
	}
}
