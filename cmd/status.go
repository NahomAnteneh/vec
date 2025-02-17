package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NahomAnteneh/vec/internal/staging"
	"github.com/NahomAnteneh/vec/utils"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the working tree status",
	Long:  `Show the working tree status (staged, unstaged, and untracked files).`,
	Args:  cobra.NoArgs, // The status command takes no arguments.
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.FindRepoRoot()
		if err != nil {
			return err
		}

		// Get staged files.  Create a StagingArea to do this.
		sa, err := staging.NewStagingArea(repoRoot)
		if err != nil {
			return err
		}
		stagedFiles := sa.GetEntries()

		unstagedFiles, untrackedFiles, err := getUnstagedAndUntrackedFiles(repoRoot, stagedFiles)
		if err != nil {
			return err
		}

		printStatus(stagedFiles, unstagedFiles, untrackedFiles)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

// --- Helper Functions for status ---

func getUnstagedAndUntrackedFiles(repoRoot string, stagedFiles map[string]string) (unstagedFiles []string, untrackedFiles []string, err error) {
	err = filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("prevent panic by handling failure accessing a path %q: %v", path, err)
		}

		// Skip the .vec directory
		if info.IsDir() && info.Name() == utils.VecDirName {
			return filepath.SkipDir
		}

		if !info.IsDir() {
			relPath, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return fmt.Errorf("could not get relative path: %w", err)
			}

			if _, ok := stagedFiles[relPath]; ok {
				currentHash, err := utils.HashFile(path)
				if err != nil {
					return err
				}
				if stagedFiles[relPath] != currentHash {
					unstagedFiles = append(unstagedFiles, relPath)
				}
			} else {
				untrackedFiles = append(untrackedFiles, relPath)
			}
		}
		return nil
	})
	return
}

func printStatus(stagedFiles map[string]string, unstagedFiles []string, untrackedFiles []string) {
	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	fmt.Println("On branch main") //  Future: get the actual branch name.

	anythingChanged := false

	if len(stagedFiles) > 0 {
		fmt.Println("Changes to be committed:")
		fmt.Println("  (use \"vec commit\" to commit changes)")
		fmt.Println()
		for filePath := range stagedFiles {
			fmt.Printf("\t%s\n", green(fmt.Sprintf("modified:   %s", filePath)))
		}
		fmt.Println()
		anythingChanged = true
	}

	if len(unstagedFiles) > 0 {
		fmt.Println("Changes not staged for commit:")
		fmt.Println("  (use \"vec add <file>...\" to update what will be committed)")
		fmt.Println()
		for _, filePath := range unstagedFiles {
			fmt.Printf("\t%s\n", red(fmt.Sprintf("modified:   %s", filePath)))
		}
		fmt.Println()
		anythingChanged = true
	}

	if len(untrackedFiles) > 0 {
		fmt.Println("Untracked files:")
		fmt.Println("  (use \"vec add <file>...\" to include in what will be committed)")
		fmt.Println()
		for _, filePath := range untrackedFiles {
			fmt.Printf("\t%s\n", filePath) // Default color for untracked
		}
		fmt.Println()
		anythingChanged = true
	}

	if !anythingChanged {
		fmt.Println("nothing to commit, working tree clean")
	}
}
