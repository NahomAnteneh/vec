// cmd/utils.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NahomAnteneh/vec/utils"
)

// getRepoRoot finds the repository root by locating the .vec directory
func getRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	for {
		if utils.FileExists(filepath.Join(dir, ".vec")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not inside a vec repository")
		}
		dir = parent
	}
}
