package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/remote"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var (
	// Remote command options
	remoteVerbose     bool
	remoteDetailed    bool
	remotePrune       bool
	remoteForce       bool
	remoteTracking    bool
	remoteTimeout     int
	remoteTags        bool
	remoteQuiet       bool
	remoteVerboseAuth bool
)

// remoteCmd represents the remote command
var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Manage set of tracked repositories",
	Long: `Manage the set of repositories ('remotes') whose branches you track.
	
With no arguments, shows a list of existing remotes. Several subcommands are
available to perform operations on the remotes.

Examples:
  vec remote                       # List all remotes
  vec remote -v                    # Show remote URLs
  vec remote --detailed            # Show detailed information for all remotes
  vec remote add origin URL        # Add a new remote
  vec remote remove origin         # Remove a remote
  vec remote show origin           # Show details about a specific remote
  vec remote prune origin          # Remove stale branches from remote
  vec remote set-url origin URL    # Change remote URL`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			// List all remotes if no arguments provided
			if err := listRemotes(remoteVerbose, remoteDetailed); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		// If arguments are provided but don't match a subcommand, show help
		cmd.Help()
	},
}

// addRemoteCmd represents the 'remote add' command
var addRemoteCmd = &cobra.Command{
	Use:   "add <name> <url>",
	Short: "Add a remote repository",
	Long: `Add a remote named <name> for the repository at <url>.
	
The command 'vec fetch <name>' can then be used to create and update
remote-tracking branches <name>/<branch> for each branch in the
remote repository.

Example:
  vec remote add origin https://example.com/user/repo
  vec remote add local ../other-repo`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		url := args[1]

		// Validate remote name
		if !isValidRemoteName(name) {
			fmt.Fprintf(os.Stderr, "Error: invalid remote name '%s'\n", name)
			os.Exit(1)
		}

		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Record start time for performance reporting
		startTime := time.Now()

		// Add the remote
		if err := remote.AddRemote(repoRoot, name, url); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Added remote '%s' with URL '%s'\n", name, url)

		if remoteTracking {
			// Optionally fetch the remote right away
			fmt.Printf("Fetching remote '%s' to establish tracking references...\n", name)

			// Create fetch options
			fetchOptions := remote.FetchOptions{
				Prune:     false,
				Quiet:     remoteQuiet,
				Verbose:   remoteVerbose,
				Force:     remoteForce,
				FetchTags: remoteTags,
				Progress:  !remoteQuiet,
			}

			if err := remote.FetchWithOptions(repoRoot, name, fetchOptions); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to fetch remote: %v\n", err)
			} else {
				fmt.Printf("Initial fetch of remote '%s' completed successfully\n", name)
			}
		}

		// Report performance
		duration := time.Since(startTime).Round(time.Millisecond)
		if remoteVerbose {
			fmt.Printf("Operation completed in %v\n", duration)
		}
	},
}

// removeRemoteCmd represents the 'remote remove' command
var removeRemoteCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm"},
	Short:   "Remove a remote repository",
	Long: `Remove the remote named <name>. All remote-tracking branches and 
configuration settings for the remote are removed.

Example:
  vec remote remove origin
  vec remote rm upstream`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Confirm removal if it's not forced
		if !remoteForce {
			fmt.Printf("Are you sure you want to remove the remote '%s'? [y/N] ", name)
			var response string
			fmt.Scanln(&response)
			if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
				fmt.Println("Remote removal canceled.")
				return
			}
		}

		// Remove the remote
		if err := remote.RemoveRemote(repoRoot, name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Removed remote '%s'\n", name)
	},
}

// renameRemoteCmd represents the 'remote rename' command
var renameRemoteCmd = &cobra.Command{
	Use:   "rename <old> <new>",
	Short: "Rename a remote repository",
	Long: `Rename the remote named <old> to <new>.

Example:
  vec remote rename origin upstream`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		oldName := args[0]
		newName := args[1]

		// Validate new remote name
		if !isValidRemoteName(newName) {
			fmt.Fprintf(os.Stderr, "Error: invalid remote name '%s'\n", newName)
			os.Exit(1)
		}

		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Record start time for performance reporting
		startTime := time.Now()

		// Rename the remote
		if err := remote.RenameRemote(repoRoot, oldName, newName); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Renamed remote '%s' to '%s'\n", oldName, newName)

		// Report performance
		if remoteVerbose {
			duration := time.Since(startTime).Round(time.Millisecond)
			fmt.Printf("Operation completed in %v\n", duration)
		}
	},
}

// setUrlRemoteCmd represents the 'remote set-url' command
var setUrlRemoteCmd = &cobra.Command{
	Use:   "set-url <name> <newurl>",
	Short: "Change URL for a remote repository",
	Long: `Change the URL for the remote named <name> to <newurl>.

Example:
  vec remote set-url origin https://example.com/user/new-repo`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		newUrl := args[1]

		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Change the URL
		if err := remote.SetRemoteURL(repoRoot, name, newUrl); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Updated URL for remote '%s' to '%s'\n", name, newUrl)
	},
}

// showRemoteCmd represents the 'remote show' command
var showRemoteCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show information about a remote",
	Long: `Shows information about the remote <name>, including tracked
branches, URLs, and last fetch time.

Example:
  vec remote show origin`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Get remote info
		info, err := remote.GetRemoteInfo(repoRoot, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Display detailed information about the remote
		fmt.Printf("* Remote '%s'\n", info.Name)
		fmt.Printf("  URL: %s\n", info.URL)

		// Get fetch info if available
		fetchInfoPath := filepath.Join(repoRoot, ".vec", "FETCH_INFO", name)
		if utils.FileExists(fetchInfoPath) {
			fetchInfo, err := os.ReadFile(fetchInfoPath)
			if err == nil {
				timestamp := remote.ParseLastFetchedTime(string(fetchInfo))
				if timestamp > 0 {
					fetchTime := time.Unix(timestamp, 0)
					fmt.Printf("  Last Fetch: %s\n", fetchTime.Format(time.RFC1123))
				} else {
					fmt.Printf("  Last Fetch: unknown\n")
				}
			}
		} else {
			fmt.Printf("  Last Fetch: never\n")
		}

		// Default branch
		if info.DefaultBranch != "" {
			fmt.Printf("  Default Branch: %s\n", info.DefaultBranch)
		} else {
			fmt.Printf("  Default Branch: unknown\n")
		}

		// Tracked branches
		fmt.Println("  Tracked Branches:")
		if len(info.Branches) > 0 {
			for _, branch := range info.Branches {
				fmt.Printf("    %s\n", branch)
			}
		} else {
			fmt.Printf("    none\n")
		}

		// Auth info (if verbose auth requested)
		if remoteVerboseAuth {
			authToken, _ := remote.GetAuthToken(name)
			if authToken != "" {
				fmt.Printf("  Authentication: configured\n")
			} else {
				fmt.Printf("  Authentication: not configured\n")
			}
		}
	},
}

// pruneRemoteCmd represents the 'remote prune' command
var pruneRemoteCmd = &cobra.Command{
	Use:   "prune <name>",
	Short: "Remove stale remote-tracking branches",
	Long: `Deletes all stale remote-tracking branches for <name>.
These stale branches have already been removed from the remote repository,
but are still locally available in "remotes/<name>".

Example:
  vec remote prune origin`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Record start time for performance reporting
		startTime := time.Now()

		// Confirm prune if it's not forced
		if !remoteForce {
			fmt.Printf("Are you sure you want to prune stale branches for remote '%s'? [y/N] ", name)
			var response string
			fmt.Scanln(&response)
			if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
				fmt.Println("Prune operation canceled.")
				return
			}
		}

		// Perform prune operation
		if err := remote.Prune(repoRoot, name); err != nil {
			fmt.Fprintf(os.Stderr, "Error pruning remote '%s': %v\n", name, err)
			os.Exit(1)
		}

		fmt.Printf("Pruned stale branches from remote '%s'\n", name)

		// Report performance
		if remoteVerbose {
			duration := time.Since(startTime).Round(time.Millisecond)
			fmt.Printf("Operation completed in %v\n", duration)
		}
	},
}

// authRemoteCmd represents the 'remote auth' command
var authRemoteCmd = &cobra.Command{
	Use:   "auth <name> [<token>]",
	Short: "Set authentication token for a remote",
	Long: `Sets or removes authentication information for a remote.
If <token> is provided, it will be stored as the auth token for <name>.
If <token> is not provided, the current auth token will be displayed.
To remove a token, use "--remove" flag.

Example:
  vec remote auth origin eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
  vec remote auth origin                # Display current token
  vec remote auth origin --remove       # Remove token`,
	Args: cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Check for remove flag
		removeAuth, _ := cmd.Flags().GetBool("remove")
		if removeAuth {
			// Remove auth token
			if err := remote.SetRemoteAuth(repoRoot, name, ""); err != nil {
				fmt.Fprintf(os.Stderr, "Error removing auth token: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Removed authentication token for remote '%s'\n", name)
			return
		}

		// If token is provided, set it
		if len(args) == 2 {
			token := args[1]
			if err := remote.SetRemoteAuth(repoRoot, name, token); err != nil {
				fmt.Fprintf(os.Stderr, "Error setting auth token: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Set authentication token for remote '%s'\n", name)
			return
		}

		// Otherwise, show current token
		token, err := remote.GetAuthToken(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting auth token: %v\n", err)
			os.Exit(1)
		}

		if token == "" {
			fmt.Printf("No authentication token configured for remote '%s'\n", name)
		} else {
			// Show token in a redacted form unless verbose auth is requested
			if remoteVerboseAuth {
				fmt.Printf("Authentication token for remote '%s': %s\n", name, token)
			} else {
				redactedToken := token
				if len(token) > 8 {
					redactedToken = token[:4] + "..." + token[len(token)-4:]
				}
				fmt.Printf("Authentication token for remote '%s': %s\n", name, redactedToken)
				fmt.Println("Use --verbose-auth to show full token")
			}
		}
	},
}

// updateRemoteCmd represents the 'remote update' command
var updateRemoteCmd = &cobra.Command{
	Use:   "update [<name>]",
	Short: "Update remote references",
	Long: `Fetches updates for remote with the same options as 'vec fetch',
but doesn't update the checked-out branches.

Examples:
  vec remote update              # Update all remotes
  vec remote update origin       # Update just the origin remote
  vec remote update --prune      # Update and prune all remotes`,
	Args: cobra.RangeArgs(0, 1),
	Run: func(cmd *cobra.Command, args []string) {
		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Record start time for performance reporting
		startTime := time.Now()

		// Create fetch options
		fetchOptions := remote.FetchOptions{
			Prune:     remotePrune,
			Quiet:     remoteQuiet,
			Verbose:   remoteVerbose,
			Force:     remoteForce,
			FetchTags: !remoteTags, // Since we use --no-tags flag to disable tags
			Progress:  !remoteQuiet,
		}

		if len(args) == 0 {
			// Update all remotes
			remotes, err := remote.ListRemotes(repoRoot)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error listing remotes: %v\n", err)
				os.Exit(1)
			}

			if len(remotes) == 0 {
				fmt.Println("No remotes configured")
				return
			}

			successCount := 0
			for name := range remotes {
				fmt.Printf("Updating remote '%s'...\n", name)
				if err := remote.FetchWithOptions(repoRoot, name, fetchOptions); err != nil {
					fmt.Fprintf(os.Stderr, "Error updating remote '%s': %v\n", name, err)
				} else {
					successCount++
				}
			}

			if successCount == 0 {
				fmt.Fprintf(os.Stderr, "Failed to update any remotes\n")
				os.Exit(1)
			} else if successCount < len(remotes) {
				fmt.Printf("Updated %d of %d remotes\n", successCount, len(remotes))
			} else {
				fmt.Println("All remotes updated successfully")
			}
		} else {
			// Update specific remote
			name := args[0]
			if err := remote.FetchWithOptions(repoRoot, name, fetchOptions); err != nil {
				fmt.Fprintf(os.Stderr, "Error updating remote '%s': %v\n", name, err)
				os.Exit(1)
			}
			fmt.Printf("Updated remote '%s' successfully\n", name)
		}

		// Report performance
		duration := time.Since(startTime).Round(time.Millisecond)
		if remoteVerbose {
			fmt.Printf("Operation completed in %v\n", duration)
		}
	},
}

// listRemotes handles the listing of all remote repositories
func listRemotes(verbose bool, detailed bool) error {
	// Get repository root
	repoRoot, err := utils.GetVecRoot()
	if err != nil {
		return err
	}

	// Get all remotes
	remotes, err := remote.ListRemotes(repoRoot)
	if err != nil {
		return err
	}

	if len(remotes) == 0 {
		fmt.Println("No remotes configured")
		return nil
	}

	// Display all remotes with proper formatting (Git-style)
	if verbose {
		// Verbose mode - show each remote with URLs for fetch and push (Git style)
		for _, r := range remotes {
			fmt.Printf("%s\t%s (fetch)\n", r.Name, r.URL)
			fmt.Printf("%s\t%s (push)\n", r.Name, r.URL)
		}
	} else if detailed {
		// Detailed information about each remote
		for _, r := range remotes {
			branches := "none"
			if len(r.Branches) > 0 {
				branches = strings.Join(r.Branches, ", ")
			}
			lastFetched := "never"
			if r.LastFetched > 0 {
				fetchTime := time.Unix(r.LastFetched, 0)
				lastFetched = fetchTime.Format(time.RFC1123)
			}
			fmt.Printf("* %s\n  URL: %s\n  Default branch: %s\n  Tracked branches: %s\n  Last fetched: %s\n\n",
				r.Name, r.URL, r.DefaultBranch, branches, lastFetched)
		}
	} else {
		// Simple mode - just show remote names (Git style)
		for _, r := range remotes {
			fmt.Println(r.Name)
		}
	}

	return nil
}

// isValidRemoteName checks if a remote name is valid
func isValidRemoteName(name string) bool {
	// Remote names cannot have spaces, special characters, or be a reserved name
	if name == "" || strings.ContainsAny(name, " ~^:?*[\\") {
		return false
	}

	// Cannot start or end with a dot or slash
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") ||
		strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return false
	}

	// Cannot be a reserved name
	reserved := []string{"HEAD", "master", "main"}
	for _, r := range reserved {
		if name == r {
			return false
		}
	}

	return true
}

func init() {
	rootCmd.AddCommand(remoteCmd)

	// Add subcommands to the remote command
	remoteCmd.AddCommand(addRemoteCmd)
	remoteCmd.AddCommand(removeRemoteCmd)
	remoteCmd.AddCommand(renameRemoteCmd)
	remoteCmd.AddCommand(setUrlRemoteCmd)
	remoteCmd.AddCommand(showRemoteCmd)
	remoteCmd.AddCommand(pruneRemoteCmd)
	remoteCmd.AddCommand(authRemoteCmd)
	remoteCmd.AddCommand(updateRemoteCmd)

	// Add flags to all remote commands
	remoteCmd.PersistentFlags().BoolVarP(&remoteVerbose, "verbose", "v", false, "Show more information")
	remoteCmd.PersistentFlags().BoolVar(&remoteForce, "force", false, "Force operation without confirmation")
	remoteCmd.PersistentFlags().IntVar(&remoteTimeout, "timeout", 60, "Timeout in seconds for network operations")

	// Add flags to the add command
	addRemoteCmd.Flags().BoolVar(&remoteTracking, "track", false, "Fetch the remote after adding to establish tracking references")
	addRemoteCmd.Flags().BoolVar(&remoteTags, "tags", true, "Fetch tags when establishing tracking")
	addRemoteCmd.Flags().BoolVarP(&remoteQuiet, "quiet", "q", false, "Suppress output messages")

	// Add flags to the remote show command
	showRemoteCmd.Flags().BoolVar(&remoteVerboseAuth, "verbose-auth", false, "Show full authentication token")

	// Add flags to the auth command
	authRemoteCmd.Flags().Bool("remove", false, "Remove authentication token for the remote")
	authRemoteCmd.Flags().BoolVar(&remoteVerboseAuth, "verbose-auth", false, "Show full authentication token")

	// Add flags to the update command
	updateRemoteCmd.Flags().BoolVarP(&remotePrune, "prune", "p", false, "Prune stale remote-tracking branches")
	updateRemoteCmd.Flags().BoolVar(&remoteTags, "no-tags", false, "Don't fetch tags when updating")
	updateRemoteCmd.Flags().BoolVarP(&remoteQuiet, "quiet", "q", false, "Suppress output messages")

	// Add flags to the main remote command
	remoteCmd.Flags().BoolVar(&remoteDetailed, "detailed", false, "Show detailed information about remotes")
}
