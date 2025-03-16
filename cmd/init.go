// cmd/init.go
package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/NahomAnteneh/vec/internal/repository"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [directory]",
	Short: "Initialize a new, empty Vec repository",
	Args:  cobra.MaximumNArgs(1), // Allow optional directory argument.
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "." // Default to current directory.
		if len(args) > 0 {
			dir = args[0]
		}

		absDir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}

		return repository.CreateRepository(absDir)
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
