package objects

import (
	"path/filepath"

	"github.com/NahomAnteneh/vec/core"
)

// GetObjectPath returns the path to an object (legacy function).
// This is exported for use by other packages.
func GetObjectPath(repoRoot string, hash string) string {
	return filepath.Join(repoRoot, ".vec", "objects", hash[:2], hash[2:])
}

// GetObjectPath returns the path to an object using Repository context.
// This is exported for use by other packages.
func GetObjectPathRepo(repo *core.Repository, hash string) string {
	return filepath.Join(repo.Root, ".vec", "objects", hash[:2], hash[2:])
}
