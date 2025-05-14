package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/remote"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var (
	cloneDepth      int
	cloneBranch     string
	cloneRecursive  bool
	cloneQuiet      bool
	cloneNoCheckout bool
	cloneProgress   bool
	cloneBareBool   bool
)

// Note: Clone doesn't use the Repository context pattern because it creates a new repository

// cloneCmd represents the clone command
var cloneCmd = &cobra.Command{
	Use:   "clone <repository> [<directory>]",
	Short: "Clone a repository into a new directory",
	Long: `Clone a repository into a new directory.

The repository can be a remote URL or a local path. If no directory 
is specified, the repository name will be used as the target directory.

Examples:
  vec clone https://example.com/repo.vec           # Clone to folder named "repo"
  vec clone https://example.com/repo.vec myproject # Clone to "myproject" folder
  vec clone https://example.com/repo.vec --branch=dev # Clone specific branch
  vec clone https://example.com/repo.vec --depth=1    # Shallow clone (only latest commit)
  vec clone https://example.com/repo.vec --bare       # Create a bare repository
  vec clone https://example.com/repo.vec --no-checkout # Don't checkout working tree
`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		url := args[0]

		// Determine if URL needs normalization
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			// Check if it's a local path
			if utils.FileExists(url) {
				absPath, err := filepath.Abs(url)
				if err == nil {
					url = absPath
				}
			} else {
				// Try to normalize as a HTTP URL
				url = "https://" + url
			}
		}

		// Get destination path
		var destPath string
		if len(args) > 1 {
			destPath = args[1]
		} else {
			// Extract repository name from URL
			destPath = extractRepoName(url)
			if destPath == "" || destPath == "." || destPath == "/" {
				return core.RemoteError("unable to determine repository name from URL. Please specify a destination directory", nil)
			}
		}

		// Check if destination already exists and is not empty
		if utils.FileExists(destPath) {
			entries, err := os.ReadDir(destPath)
			if err != nil {
				return core.FSError(fmt.Sprintf("failed to read destination directory: %s", destPath), err)
			}
			if len(entries) > 0 {
				return core.FSError(fmt.Sprintf("destination path '%s' already exists and is not an empty directory", destPath), nil)
			}
		}

		// Get authentication token if provided
		auth, _ := cmd.Flags().GetString("auth")

		// Validate depth parameter
		if cloneDepth < 0 {
			return core.RemoteError(fmt.Sprintf("invalid depth value: %d (must be >= 0)", cloneDepth), nil)
		}

		// Show initial message
		if !cloneQuiet {
			fmt.Printf("Cloning into '%s'...\n", destPath)
		}

		// Start time measurement for performance reporting
		startTime := time.Now()

		// Clone the repository
		if err := remote.CloneWithOptions(remote.CloneOptions{
			URL:        url,
			DestPath:   destPath,
			Auth:       auth,
			Branch:     cloneBranch,
			Depth:      cloneDepth,
			Recursive:  cloneRecursive,
			NoCheckout: cloneNoCheckout,
			Bare:       cloneBareBool,
			Quiet:      cloneQuiet,
			Progress:   cloneProgress,
		}); err != nil {
			return core.RemoteError("clone failed", err)
		}

		// Show completion message with timing
		if !cloneQuiet {
			duration := time.Since(startTime).Round(time.Millisecond)
			fmt.Printf("Clone completed successfully in %s\n", duration)

			// Print additional info if not bare
			if !cloneBareBool {
				// Get current branch if available
				currentBranch := "unknown"
				if headContent, err := os.ReadFile(filepath.Join(destPath, ".vec", "HEAD")); err == nil {
					head := strings.TrimSpace(string(headContent))
					if strings.HasPrefix(head, "ref: refs/heads/") {
						currentBranch = strings.TrimPrefix(head, "ref: refs/heads/")
					}
				}
				fmt.Printf("Current branch: %s\n", currentBranch)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(cloneCmd)

	// Add flags
	cloneCmd.Flags().String("auth", "", "Authentication token for the remote repository")
	cloneCmd.Flags().IntVar(&cloneDepth, "depth", 0, "Create a shallow clone with specified depth")
	cloneCmd.Flags().StringVar(&cloneBranch, "branch", "", "Clone specific branch instead of default")
	cloneCmd.Flags().BoolVar(&cloneRecursive, "recursive", false, "Clone submodules recursively")
	cloneCmd.Flags().BoolVar(&cloneQuiet, "quiet", false, "Suppress progress output")
	cloneCmd.Flags().BoolVar(&cloneNoCheckout, "no-checkout", false, "Don't checkout HEAD after cloning")
	cloneCmd.Flags().BoolVar(&cloneProgress, "progress", true, "Show progress during clone")
	cloneCmd.Flags().BoolVar(&cloneBareBool, "bare", false, "Create a bare repository")
}

// extractRepoName derives a directory name from the remote URL
func extractRepoName(remoteURL string) string {
	// Handle URL schemes
	url := remoteURL
	for _, prefix := range []string{"http://", "https://", "ssh://", "git://"} {
		url = strings.TrimPrefix(url, prefix)
	}

	// Split on slashes and take the last part
	parts := strings.Split(url, "/")
	name := parts[len(parts)-1]

	// Remove common extensions
	for _, ext := range []string{".vec", ".git"} {
		name = strings.TrimSuffix(name, ext)
	}

	// Clean the name
	name = strings.TrimSpace(name)

	// If we still have nothing useful, use a default
	if name == "" {
		return "vec-repo"
	}

	return name
}
