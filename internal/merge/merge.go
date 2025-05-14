package merge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/staging"
	"github.com/NahomAnteneh/vec/utils"
)

// MergeStrategy represents an auto-resolution strategy.
type MergeStrategy string

const (
	MergeStrategyRecursive MergeStrategy = "recursive" // default three-way merge
	MergeStrategyOurs      MergeStrategy = "ours"      // always use our changes
	MergeStrategyTheirs    MergeStrategy = "theirs"    // always use their changes
)

// MergeConfig holds options to influence merge behavior.
type MergeConfig struct {
	Strategy    MergeStrategy // Strategy for conflict resolution
	Interactive bool          // Whether to prompt user interactively on conflicts
}

// MergeResult captures the outcome of the merge operation.
type MergeResult struct {
	HasConflicts bool
	Path         string
	BaseSha      string
	OursSha      string
	TheirsSha    string
	ConflictType string
	Content      []byte
}

// Merge performs a merge of the sourceBranch into the current branch.
// Legacy function that calls MergeRepo with a repository context.
func Merge(repoRoot, sourceBranch string, config *MergeConfig) (bool, error) {
	repo := core.NewRepository(repoRoot)
	return MergeRepo(repo, sourceBranch, config)
}

// MergeRepo performs a merge of the sourceBranch into the current branch using a Repository context.
// Returns true if the merge was a fast-forward, false otherwise.
func MergeRepo(repo *core.Repository, sourceBranch string, config *MergeConfig) (bool, error) {
	if config == nil {
		// Default to recursive (normal three-way merge with conflict markers) and non-interactive.
		config = &MergeConfig{Strategy: MergeStrategyRecursive, Interactive: false}
	}

	// Validate repository and load index.
	vecDir := repo.VecDir
	if _, err := os.Stat(vecDir); os.IsNotExist(err) {
		return false, fmt.Errorf("not a vec repository: %s", repo.Root)
	}
	index, err := staging.LoadIndex(repo.Root)
	if err != nil {
		return false, fmt.Errorf("failed to load index: %w", err)
	}

	// Check for uncommitted changes.
	if index.HasUncommittedChanges(repo.Root) {
		return false, fmt.Errorf("uncommitted changes detected; commit or stash them before merging")
	}

	// Load current branch and HEAD.
	currentBranch, err := GetCurrentBranch(repo.Root)
	if err != nil {
		return false, fmt.Errorf("failed to determine current branch: %w", err)
	}
	headCommitID, err := utils.ReadHEAD(repo.Root)
	if err != nil {
		return false, fmt.Errorf("failed to read HEAD: %w", err)
	}
	if headCommitID == "" {
		return false, fmt.Errorf("HEAD is not set")
	}

	// Load source branch commit.
	sourceBranchFile := filepath.Join(vecDir, "refs", "heads", sourceBranch)
	sourceCommitIDBytes, err := os.ReadFile(sourceBranchFile)
	if err != nil {
		return false, fmt.Errorf("failed to read source branch '%s': %w", sourceBranch, err)
	}
	sourceCommitID := strings.TrimSpace(string(sourceCommitIDBytes))

	// Prevent self-merge.
	if currentBranch == sourceBranch {
		return false, fmt.Errorf("cannot merge a branch with itself")
	}

	// Find merge base.
	baseCommitID, err := findMergeBase(repo.Root, headCommitID, sourceCommitID)
	if err != nil {
		return false, fmt.Errorf("failed to find merge base: %w", err)
	}

	// Handle fast-forward or already up-to-date cases.
	if baseCommitID == headCommitID {
		// Fast-forward: current branch is behind source branch.
		if err := CheckoutCommitRepo(repo, sourceCommitID); err != nil {
			return false, fmt.Errorf("failed to checkout source commit for fast-forward: %w", err)
		}
		branchFile := filepath.Join(vecDir, "refs", "heads", currentBranch)
		if err := os.WriteFile(branchFile, []byte(sourceCommitID), 0644); err != nil {
			return false, fmt.Errorf("failed to update branch pointer: %w", err)
		}
		fmt.Println("Fast-forward merge completed.")
		return true, nil
	} else if baseCommitID == sourceCommitID {
		// Already up-to-date.
		return false, fmt.Errorf("already up-to-date")
	}

	// Load commit objects.
	baseCommit, err := objects.GetCommit(repo.Root, baseCommitID)
	if err != nil {
		return false, fmt.Errorf("failed to load base commit: %w", err)
	}
	ourCommit, err := objects.GetCommit(repo.Root, headCommitID)
	if err != nil {
		return false, fmt.Errorf("failed to load our commit: %w", err)
	}
	theirCommit, err := objects.GetCommit(repo.Root, sourceCommitID)
	if err != nil {
		return false, fmt.Errorf("failed to load their commit: %w", err)
	}

	// Load tree objects.
	baseTree, err := objects.GetTree(repo.Root, baseCommit.Tree)
	if err != nil {
		return false, fmt.Errorf("failed to load base tree: %w", err)
	}
	ourTree, err := objects.GetTree(repo.Root, ourCommit.Tree)
	if err != nil {
		return false, fmt.Errorf("failed to load our tree: %w", err)
	}
	theirTree, err := objects.GetTree(repo.Root, theirCommit.Tree)
	if err != nil {
		return false, fmt.Errorf("failed to load their tree: %w", err)
	}

	// Perform the three-way merge.
	result, err := performMergeRepo(repo, index, baseTree, ourTree, theirTree, config)
	if err != nil {
		return false, fmt.Errorf("merge failed: %w", err)
	}

	// Write updated index.
	if err := index.Write(); err != nil {
		return false, fmt.Errorf("failed to write index: %w", err)
	}

	if result.HasConflicts {
		fmt.Println("Merge conflicts detected. Please resolve them and commit the result.")
		return true, nil
	}

	// Create tree from merged index.
	treeID, err := staging.CreateTreeFromIndex(repo.Root, index)
	if err != nil {
		return false, fmt.Errorf("failed to create tree from index: %w", err)
	}

	// Create merge commit.
	author := ourCommit.Author
	committer := ourCommit.Committer
	if committer == "" {
		committer = author
	}
	message := fmt.Sprintf("Merge branch '%s' into %s", sourceBranch, currentBranch)
	timestamp := time.Now().Unix()
	commitHash, err := objects.CreateCommit(repo.Root, treeID, []string{headCommitID, sourceCommitID}, author, committer, message, timestamp)
	if err != nil {
		return false, fmt.Errorf("failed to create merge commit: %w", err)
	}

	// Update branch pointer.
	branchFile := filepath.Join(vecDir, "refs", "heads", currentBranch)
	if err := os.WriteFile(branchFile, []byte(commitHash), 0644); err != nil {
		return false, fmt.Errorf("failed to update branch pointer: %w", err)
	}

	fmt.Println("Merge completed successfully.")
	return false, nil
}
