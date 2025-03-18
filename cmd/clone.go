package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/internal/config"
	"github.com/NahomAnteneh/vec/internal/merge"
	"github.com/NahomAnteneh/vec/internal/remote"
	"github.com/spf13/cobra"
)

// cloneCmd represents the clone command
var cloneCmd = &cobra.Command{
	Use:   "clone <repository> [<directory>]",
	Short: "Clone a repository into a new directory",
	Long: `Clone a repository into a new directory.
If no directory is specified, the repository name will be used.`,
	Args: cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		url := args[0]

		// Get destination path
		var destPath string
		if len(args) > 1 {
			destPath = args[1]
		} else {
			// Extract repository name from URL
			destPath = filepath.Base(url)
			if destPath == "." || destPath == "/" {
				fmt.Println("Unable to determine repository name from URL. Please specify a destination directory.")
				os.Exit(1)
			}

			// Remove .vec suffix if present
			if filepath.Ext(destPath) == ".vec" {
				destPath = destPath[:len(destPath)-4]
			}
		}

		// Get authentication token if provided
		auth, _ := cmd.Flags().GetString("auth")

		// Clone the repository
		if err := remote.Clone(url, destPath, auth); err != nil {
			fmt.Printf("Error: %s\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(cloneCmd)

	// Add flags
	cloneCmd.Flags().String("auth", "", "Optional authentication token for the remote repository")
}

// extractRepoName derives a directory name from the remote URL
func extractRepoName(remoteURL string) string {
	parts := strings.Split(remoteURL, "/")
	name := strings.TrimSuffix(parts[len(parts)-1], ".vec")
	if name == "" {
		return "vec-repo"
	}
	return name
}

// cloneRepository performs the cloning operation
func cloneRepository(remoteURL, dir string) error {
	// Create and enter the directory
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("failed to enter directory %s: %w", dir, err)
	}

	// Initialize repository structure
	repoRoot := "."
	if err := os.MkdirAll(filepath.Join(repoRoot, ".vec", "objects"), 0755); err != nil {
		return fmt.Errorf("failed to initialize .vec/objects: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, ".vec", "refs", "heads"), 0755); err != nil {
		return fmt.Errorf("failed to initialize .vec/refs/heads: %w", err)
	}

	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Configure remote
	if err := cfg.AddRemote("origin", remoteURL); err != nil {
		return fmt.Errorf("failed to configure remote: %w", err)
	}

	// Fetch all data
	if err := remote.Fetch(repoRoot, remoteURL); err != nil {
		return fmt.Errorf("failed to fetch data: %w", err)
	}

	// Determine default branch
	defaultBranch, err := getDefaultBranch(remoteURL)
	if err != nil {
		return fmt.Errorf("failed to determine default branch: %w", err)
	}

	// Set up local branch and HEAD
	remoteRef := filepath.Join(repoRoot, ".vec", "refs", "remotes", "origin", defaultBranch)
	commitHashBytes, err := os.ReadFile(remoteRef)
	if err != nil {
		return fmt.Errorf("failed to read remote ref %s: %w", defaultBranch, err)
	}
	commitHash := strings.TrimSpace(string(commitHashBytes))

	localRef := filepath.Join(repoRoot, ".vec", "refs", "heads", defaultBranch)
	if err := os.WriteFile(localRef, []byte(commitHash+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to create local branch ref: %w", err)
	}

	if err := os.WriteFile(filepath.Join(repoRoot, ".vec", "HEAD"), []byte("ref: refs/heads/"+defaultBranch+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to set HEAD: %w", err)
	}

	// Checkout the default branch
	if err := merge.CheckoutCommit(repoRoot, commitHash); err != nil {
		return fmt.Errorf("failed to checkout %s: %w", defaultBranch, err)
	}

	return nil
}

// getDefaultBranch retrieves the default branch from the remote HEAD
func getDefaultBranch(remoteURL string) (string, error) {
	url := fmt.Sprintf("%s/.vec/HEAD", remoteURL)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("cannot fetch HEAD: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HEAD fetch failed with status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read HEAD: %w", err)
	}
	head := strings.TrimSpace(string(data))
	if !strings.HasPrefix(head, "ref: refs/heads/") {
		return "", fmt.Errorf("invalid HEAD format: %s", head)
	}
	return strings.TrimPrefix(head, "ref: refs/heads/"), nil
}
