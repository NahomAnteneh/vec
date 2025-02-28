// cmd/init.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NahomAnteneh/vec/utils"
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

		return initRepository(absDir)
	},
}

func initRepository(dir string) error {
	vecDir := filepath.Join(dir, ".vec")

	// Check if repository already exists.
	if utils.FileExists(vecDir) {
		return fmt.Errorf("vec repository already initialized at %s", dir)
	}

	// Create .vec directory.
	if err := os.MkdirAll(vecDir, 0755); err != nil {
		return fmt.Errorf("failed to create .vec directory: %w", err)
	}

	// Create subdirectories.
	subDirs := []string{
		filepath.Join(vecDir, "objects"),
		filepath.Join(vecDir, "refs", "heads"),
		filepath.Join(vecDir, "refs", "remotes"),
		filepath.Join(vecDir, "logs", "refs", "heads"),
		filepath.Join(vecDir, "logs"),
	}
	for _, subDir := range subDirs {
		if err := os.MkdirAll(subDir, 0755); err != nil {
			return fmt.Errorf("failed to create subdirectory %s: %w", subDir, err)
		}
	}

	// Create HEAD file.
	headFile := filepath.Join(vecDir, "HEAD")
	if err := os.WriteFile(headFile, []byte("ref: refs/heads/main\n"), 0644); err != nil {
		return fmt.Errorf("failed to create HEAD file: %w", err)
	}

	// Create logs/HEAD file
	logHeadFile := filepath.Join(vecDir, "logs", "HEAD")
	if err := os.WriteFile(logHeadFile, []byte(""), 0644); err != nil { // Initialize as empty
		return fmt.Errorf("failed to create logs/HEAD file: %w", err)
	}

	fmt.Printf("Initialized empty Vec repository in %s\n", vecDir)
	return nil
}

func init() {
	rootCmd.AddCommand(initCmd)
}
