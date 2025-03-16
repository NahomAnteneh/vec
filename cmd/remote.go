package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/remote"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var (
	remoteVerbose      bool
	remoteDetailed     bool
	remoteDetailedInfo bool
)

// remoteCmd represents the remote command
var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Manage set of tracked repositories",
	Long: `Manage the set of repositories ('remotes') whose branches you track.
	
With no arguments, shows a list of existing remotes. Several subcommands are
available to perform operations on the remotes.`,
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
remote repository.`,
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

		// Add the remote
		if err := remote.AddRemote(repoRoot, name, url); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Added remote '%s' with URL '%s'\n", name, url)
	},
}

// removeRemoteCmd represents the 'remote remove' command
var removeRemoteCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm"},
	Short:   "Remove a remote repository",
	Long:    `Remove the remote named <name>. All remote-tracking branches and configuration settings for the remote are removed.`,
	Args:    cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
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
	Long:  `Rename the remote named <old> to <new>.`,
	Args:  cobra.ExactArgs(2),
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

		// Rename the remote
		if err := remote.RenameRemote(repoRoot, oldName, newName); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Renamed remote '%s' to '%s'\n", oldName, newName)
	},
}

// setUrlRemoteCmd represents the 'remote set-url' command
var setUrlRemoteCmd = &cobra.Command{
	Use:   "set-url <name> <newurl>",
	Short: "Change URL for a remote repository",
	Long:  `Change the URL for the remote named <name> to <newurl>.`,
	Args:  cobra.ExactArgs(2),
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

// pushRemoteCmd represents the 'remote push' command
var pushRemoteCmd = &cobra.Command{
	Use:   "push <remote> [<refspec>]",
	Short: "Update remote refs along with associated objects",
	Long: `Updates remote refs using local refs, while sending objects
necessary to complete the given refs.

When <refspec> is not specified, the current branch will be pushed to the 
corresponding upstream branch, but as a safety measure, the push is aborted
if the upstream branch does not have the same name as the local one.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		remoteName := args[0]

		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Determine refspec
		refspec := ""
		if len(args) > 1 {
			refspec = args[1]
		}

		// Get current branch if refspec is not provided
		if refspec == "" {
			branch, err := utils.GetCurrentBranch(repoRoot)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			refspec = branch
		}

		// Use the improved packfile implementation
		force := cmd.Flag("force").Changed
		if err := remote.Push(repoRoot, remoteName, refspec, force); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully pushed to remote '%s'\n", remoteName)
	},
}

// fetchRemoteCmd represents the 'remote fetch' command
var fetchRemoteCmd = &cobra.Command{
	Use:   "fetch <remote>",
	Short: "Download objects and refs from another repository",
	Long: `Fetch branches and/or tags (collectively, "refs") from one or 
more other repositories, along with the objects necessary to complete 
their histories. Remote-tracking branches are updated.

By default, remote branches that don't exist locally are created and
tracked in 'refs/remotes/<remote>/'.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		remoteName := args[0]

		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Use the modern packfile format
		if err := remote.Fetch(repoRoot, remoteName); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully fetched from remote '%s'\n", remoteName)
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
			if !r.LastFetched.IsZero() && r.LastFetched.Year() > 1 {
				lastFetched = r.LastFetched.Format(time.RFC1123)
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
	remoteCmd.AddCommand(pushRemoteCmd)
	remoteCmd.AddCommand(fetchRemoteCmd)

	// Add flags to the push command
	pushRemoteCmd.Flags().BoolP("force", "f", false, "Force update remote ref even if it is not a fast-forward change")
	pushRemoteCmd.Flags().BoolP("all", "a", false, "Push all branches")

	// Add flags to the fetch command
	fetchRemoteCmd.Flags().BoolP("all", "a", false, "Fetch all remotes")
	fetchRemoteCmd.Flags().BoolP("prune", "p", false, "Remove any remote-tracking references that no longer exist on the remote")

	// Add flags
	remoteCmd.Flags().BoolVarP(&remoteVerbose, "verbose", "v", false, "Show remote URL after name")
	remoteCmd.Flags().BoolVar(&remoteDetailed, "detailed", false, "Show detailed information about remotes")
	remoteCmd.Flags().BoolVar(&remoteDetailedInfo, "detailed-info", false, "Show detailed information about remotes")
}

// // cmd/remote.go
// package cmd

// import (
// 	"fmt"
// 	"os"
// 	"sort"

// 	"github.com/NahomAnteneh/vec/internal/config"
// 	"github.com/spf13/cobra"
// )

// // remoteCmd represents the remote command
// var remoteCmd = &cobra.Command{
// 	Use:   "remote",
// 	Short: "Manage remote repositories",
// 	Long:  `Add, remove, rename, or show remote repositories for the vec repository.`,
// }

// var remoteAddCmd = &cobra.Command{
// 	Use:   "add <name> <url>",
// 	Short: "Add a remote repository",
// 	Long:  `Add a remote repository with the given name and URL.`,
// 	Args:  cobra.ExactArgs(2),
// 	Run: func(cmd *cobra.Command, args []string) {
// 		name := args[0]
// 		url := args[1]

// 		auth, _ := cmd.Flags().GetString("auth")

// 		c, err := config.LoadConfig(".")
// 		if err != nil {
// 			fmt.Printf("Error loading config: %s\n", err)
// 			return
// 		}

// 		err = c.AddRemote(name, url)
// 		if err != nil {
// 			fmt.Printf("Error adding remote: %s\n", err)
// 			return
// 		}

// 		if auth != "" {
// 			err = c.SetRemoteAuth(name, auth)
// 			if err != nil {
// 				fmt.Printf("Error setting authentication: %s\n", err)
// 				return
// 			}
// 		}

// 		err = c.Write()
// 		if err != nil {
// 			fmt.Printf("Error saving config: %s\n", err)
// 			return
// 		}

// 		fmt.Printf("Added remote %s at %s\n", name, url)
// 		if auth != "" {
// 			fmt.Println("Authentication token has been set")
// 		}
// 	},
// }

// var remoteRemoveCmd = &cobra.Command{
// 	Use:   "remove <name>",
// 	Short: "Remove a remote repository",
// 	Args:  cobra.ExactArgs(1),
// 	Run: func(cmd *cobra.Command, args []string) {
// 		repoRoot, err := getRepoRoot()
// 		if err != nil {
// 			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
// 			os.Exit(1)
// 		}
// 		cfg, err := config.LoadConfig(repoRoot)
// 		if err != nil {
// 			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
// 			os.Exit(1)
// 		}
// 		name := args[0]
// 		if err := cfg.RemoveRemote(name); err != nil {
// 			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
// 			os.Exit(1)
// 		}
// 		if err := cfg.Write(); err != nil {
// 			fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
// 			os.Exit(1)
// 		}
// 		fmt.Printf("Removed remote '%s'\n", name)
// 	},
// }

// var remoteRenameCmd = &cobra.Command{
// 	Use:   "rename <old-name> <new-name>",
// 	Short: "Rename a remote repository",
// 	Args:  cobra.ExactArgs(2),
// 	Run: func(cmd *cobra.Command, args []string) {
// 		repoRoot, err := getRepoRoot()
// 		if err != nil {
// 			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
// 			os.Exit(1)
// 		}
// 		cfg, err := config.LoadConfig(repoRoot)
// 		if err != nil {
// 			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
// 			os.Exit(1)
// 		}
// 		oldName, newName := args[0], args[1]
// 		if err := cfg.RenameRemote(oldName, newName); err != nil {
// 			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
// 			os.Exit(1)
// 		}
// 		if err := cfg.Write(); err != nil {
// 			fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
// 			os.Exit(1)
// 		}
// 		fmt.Printf("Renamed remote '%s' to '%s'\n", oldName, newName)
// 	},
// }

// var remoteShowCmd = &cobra.Command{
// 	Use:   "show",
// 	Short: "Show all remote repositories",
// 	Run: func(cmd *cobra.Command, args []string) {
// 		repoRoot, err := getRepoRoot()
// 		if err != nil {
// 			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
// 			os.Exit(1)
// 		}
// 		cfg, err := config.LoadConfig(repoRoot)
// 		if err != nil {
// 			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
// 			os.Exit(1)
// 		}
// 		if len(cfg.Remotes) == 0 {
// 			fmt.Println("No remotes configured.")
// 			return
// 		}
// 		remoteNames := make([]string, 0, len(cfg.Remotes))
// 		for name := range cfg.Remotes {
// 			remoteNames = append(remoteNames, name)
// 		}
// 		sort.Strings(remoteNames)
// 		for _, name := range remoteNames {
// 			fmt.Printf("%s: %s\n", name, cfg.Remotes[name])
// 		}
// 	},
// }

// func init() {
// 	rootCmd.AddCommand(remoteCmd)
// 	remoteCmd.AddCommand(remoteAddCmd)
// 	remoteCmd.AddCommand(remoteRemoveCmd)
// 	remoteCmd.AddCommand(remoteRenameCmd)
// 	remoteCmd.AddCommand(remoteShowCmd)

// 	// Add flags to remoteAddCmd
// 	remoteAddCmd.Flags().String("auth", "", "Authentication token for the remote")
// }

// // getRepoRoot finds the repository root by looking for .vec directory
// func getRepoRoot() (string, error) {
// 	dir, err := os.Getwd()
// 	if err != nil {
// 		return "", fmt.Errorf("failed to get current directory: %v", err)
// 	}
// 	for {
// 		if utils.FileExists(filepath.Join(dir, ".vec")) {
// 			return dir, nil
// 		}
// 		parent := filepath.Dir(dir)
// 		if parent == dir { // Reached root directory
// 			return "", fmt.Errorf("not inside a vec repository")
// 		}
// 		dir = parent
// 	}
// }
