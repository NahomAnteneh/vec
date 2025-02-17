package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/NahomAnteneh/vec/internal/staging"
	"github.com/NahomAnteneh/vec/utils"

	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add <file>...",
	Short: "Add file contents to the index",
	Long:  `Add file contents to the index (staging area).`,
	Args:  cobra.MinimumNArgs(1), // Require at least one file argument.
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.FindRepoRoot()
		if err != nil {
			return err
		}

		sa, err := staging.NewStagingArea(repoRoot)
		if err != nil {
			return err
		}

		for _, arg := range args {
			// Handle wildcards (globbing)
			matches, err := filepath.Glob(filepath.Join(repoRoot, arg))
			if err != nil {
				return fmt.Errorf("invalid pattern '%s': %w", arg, err)
			}
			if len(matches) == 0 {
				return fmt.Errorf("no files matched pattern: %s", arg)
			}

			for _, filePath := range matches {
				relPath, err := filepath.Rel(repoRoot, filePath)
				if err != nil {
					return fmt.Errorf("could not get relative path: %w", err)
				}
				if err := sa.AddFile(relPath); err != nil {
					return err
				}
			}
		}

		// Write the updated index file.
		return sa.WriteIndex(repoRoot)
	},
}

func init() {
	rootCmd.AddCommand(addCmd)
}
