// cmd/init.go
package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/repository"
)

// initHandler handles the initialization of a new repository
func initHandler(args []string) error {
	dir := "." // Default to current directory.
	if len(args) > 0 {
		dir = args[0]
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return core.FSError("failed to get absolute path", err)
	}

	if err := repository.CreateRepository(absDir); err != nil {
		return core.RepositoryError(fmt.Sprintf("failed to initialize repository in '%s'", absDir), err)
	}

	return nil
}

// init adds the init command to the root command
func init() {
	initCmd := NewInitCommand(
		"init [directory]",
		"Initialize a new, empty Vec repository",
		initHandler,
	)
	rootCmd.AddCommand(initCmd)
}
