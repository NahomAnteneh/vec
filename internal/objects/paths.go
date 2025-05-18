package objects

import (
	"path/filepath"

	"github.com/NahomAnteneh/vec/core"
)

// GetObjectPathRepo returns the path to an object using Repository context.
// This is exported for use by other packages.
func GetObjectPathRepo(repo *core.Repository, hash string) string {
	return filepath.Join(repo.ObjectsDir, hash[:2], hash[2:])
}
