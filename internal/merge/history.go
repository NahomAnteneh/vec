package merge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/objects"
)

// GetCurrentBranch determines the current branch from HEAD (legacy function).
func GetCurrentBranch(repoRoot string) (string, error) {
	repo := core.NewRepository(repoRoot)
	return GetCurrentBranchRepo(repo)
}

// GetCurrentBranchRepo determines the current branch from HEAD using Repository context.
func GetCurrentBranchRepo(repo *core.Repository) (string, error) {
	headFile := filepath.Join(repo.Root, ".vec", "HEAD")
	content, err := os.ReadFile(headFile)
	if err != nil {
		return "", fmt.Errorf("failed to read HEAD file: %w", err)
	}
	ref := strings.TrimSpace(string(content))
	if !strings.HasPrefix(ref, "ref: ") {
		return "", fmt.Errorf("HEAD is not a symbolic reference: %s", ref)
	}
	refPath := strings.TrimSpace(ref[5:])
	parts := strings.Split(refPath, "/")
	if len(parts) != 3 || parts[0] != "refs" || parts[1] != "heads" {
		return "", fmt.Errorf("invalid HEAD reference: %s", ref)
	}
	return parts[2], nil
}

// findMergeBase finds the most recent common ancestor of two commits (legacy function).
func findMergeBase(repoRoot, commit1, commit2 string) (string, error) {
	repo := core.NewRepository(repoRoot)
	return findMergeBaseRepo(repo, commit1, commit2)
}

// findMergeBaseRepo finds the most recent common ancestor using Repository context.
// Optimized version with better performance characteristics for deep histories.
func findMergeBaseRepo(repo *core.Repository, commit1, commit2 string) (string, error) {
	// If the commits are the same, that's the merge base.
	if commit1 == commit2 {
		return commit1, nil
	}

	// Use cached results if available
	cacheKey := fmt.Sprintf("%s-%s", commit1, commit2)
	if _, err := os.Stat(filepath.Join(repo.Root, ".vec", "cached_merge_base", cacheKey)); err == nil {
		data, err := os.ReadFile(filepath.Join(repo.Root, ".vec", "cached_merge_base", cacheKey))
		if err == nil && len(data) > 0 {
			return string(data), nil
		}
	}

	// Instead of collecting all ancestors of commit1 first (which is inefficient for
	// large repositories), we'll use a more efficient algorithm that traverses both
	// commit histories simultaneously.

	// Use a generation number approach
	generations1 := make(map[string]int)

	// Traverse commit1 lineage with generation numbers
	q1 := []string{commit1}
	for gen := 0; len(q1) > 0; gen++ {
		var nextQ []string
		for _, c := range q1 {
			if _, exists := generations1[c]; exists {
				continue // Skip if already encountered
			}
			generations1[c] = gen

			commit, err := objects.GetCommit(repo.Root, c)
			if err != nil {
				return "", fmt.Errorf("failed to load commit %s: %w", c, err)
			}

			nextQ = append(nextQ, commit.Parents...)
		}
		q1 = nextQ
	}

	// Use a priority queue approach for commit2 traversal to find the
	// lowest common ancestor with the minimum sum of generation numbers
	bestBase := ""
	bestCost := -1

	visited := make(map[string]bool)
	q2 := []string{commit2}

	for len(q2) > 0 {
		c := q2[0]
		q2 = q2[1:]

		if visited[c] {
			continue
		}
		visited[c] = true

		// Check if this is a common ancestor
		if gen1, ok := generations1[c]; ok {
			// This is a common ancestor
			cost := gen1
			if bestBase == "" || cost < bestCost {
				bestBase = c
				bestCost = cost
			}
		}

		// Continue traversal
		commit, err := objects.GetCommit(repo.Root, c)
		if err != nil {
			return "", fmt.Errorf("failed to load commit %s: %w", c, err)
		}

		q2 = append(q2, commit.Parents...)
	}

	if bestBase == "" {
		return "", fmt.Errorf("no common ancestor found between %s and %s", commit1, commit2)
	}

	// Cache the result for future use
	cacheDir := filepath.Join(repo.Root, ".vec", "cached_merge_base")
	if err := os.MkdirAll(cacheDir, 0755); err == nil {
		os.WriteFile(filepath.Join(cacheDir, cacheKey), []byte(bestBase), 0644)
	}

	return bestBase, nil
}
