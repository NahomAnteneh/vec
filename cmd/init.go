// cmd/init.go
package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/repository"
)

var bare bool

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

	repo := core.NewRepository(absDir)

	if bare {
		if err := repository.CreateBareRepo(repo); err != nil {
			return core.RepositoryError(fmt.Sprintf("failed to initialize bare repository in '%s'", absDir), err)
		}
	} else {
		if err := repository.CreateRepo(repo); err != nil {
			return core.RepositoryError(fmt.Sprintf("failed to initialize repository in '%s'", absDir), err)
		}
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

	initCmd.Flags().BoolVar(&bare, "bare", false, "Initialize a bare repository")
	rootCmd.AddCommand(initCmd)
}
