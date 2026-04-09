package cherrypick

import "github.com/theakshaypant/pronto/pkg/models"

// CherryPickOptions defines the parameters for a single cherry-pick operation.
type CherryPickOptions struct {
	// Repository identification
	Owner    string
	Repo     string
	CloneURL string
	Token    string

	// PR details
	PRNumber       int
	TargetBranch   *models.TargetBranch
	CommitSHAs     []string
	CommitMessages []string

	// Behavior
	HasWriteAccess bool
	AlwaysCreatePR bool
	DryRun         bool

	// Git identity
	BotName  string
	BotEmail string

	// Labels
	ConflictLabel string
	LabelPattern  string
}

// BatchOptions defines the parameters for batch cherry-pick operations.
type BatchOptions struct {
	// Repository identification
	Owner    string
	Repo     string
	CloneURL string
	Token    string

	// Target branches
	TargetBranches []*models.TargetBranch

	// Behavior
	HasWriteAccess bool
	AlwaysCreatePR bool
	DryRun         bool

	// Git identity
	BotName  string
	BotEmail string

	// Labels
	ConflictLabel string
	LabelPattern  string
}

// PRInput holds PR-specific data for batch operations.
type PRInput struct {
	PRNumber       int
	CommitSHAs     []string
	CommitMessages []string
	HeadSHA        string
}
