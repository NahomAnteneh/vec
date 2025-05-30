package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/NahomAnteneh/vec/internal/remote"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var (
	// Remote command options
	remoteVerbose bool
	remotePrune   bool
)

// remoteCmd represents the remote command
var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Manage set of tracked repositories",
	Long: `Manage the set of repositories ('remotes') whose branches you track.
	
With no arguments, shows a list of existing remotes.

Examples:
  vec remote                    # List all remotes
  vec remote -v                 # Show remote URLs
  vec remote add origin URL     # Add a new remote
  vec remote remove origin      # Remove a remote
  vec remote show origin        # Show details about a specific remote`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			// List all remotes if no arguments provided
			if err := listRemotes(remoteVerbose); err != nil {
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
	Long: `Remove the remote named <name>. All remote-tracking branches and 
configuration settings for the remote are removed.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Confirm removal if interactive
		fmt.Printf("Are you sure you want to remove the remote '%s'? [y/N] ", name)
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			fmt.Println("Remote removal canceled.")
			return
		}

		// Remove the remote
		if err := remote.RemoveRemote(repoRoot, name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Removed remote '%s'\n", name)
	},
}

// showRemoteCmd represents the 'remote show' command
var showRemoteCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show information about a remote",
	Long:  `Display information about the remote <name>.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Get remote info
		remoteInfo, err := remote.GetRemoteInfo(repoRoot, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Display remote info
		fmt.Printf("* remote %s\n", name)
		fmt.Printf("  URL: %s\n", remoteInfo.URL)
		
		if len(remoteInfo.Branches) > 0 {
			fmt.Printf("  Tracked branches:\n")
			for _, branch := range remoteInfo.Branches {
				fmt.Printf("    %s\n", branch)
			}
		} else {
			fmt.Printf("  No branches tracked\n")
		}

		// Display last fetch time if available
		if remoteInfo.LastFetched > 0 {
			lastFetchedTime := utils.FormatTimestamp(remoteInfo.LastFetched)
			fmt.Printf("  Last fetched: %s\n", lastFetchedTime)
		}
	},
}

// setUrlCmd represents the 'remote set-url' command
var setUrlCmd = &cobra.Command{
	Use:   "set-url <name> <url>",
	Short: "Change the URL for a remote",
	Long:  `Changes the URL for the remote named <name>.`,
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		url := args[1]

		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Update the URL
		if err := remote.SetRemoteURL(repoRoot, name, url); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Updated URL for remote '%s' to '%s'\n", name, url)
	},
}

// setCredentialsCmd represents the 'remote set-credentials' command
var setCredentialsCmd = &cobra.Command{
	Use:   "set-credentials <name> <username> <password>",
	Short: "Set authentication credentials for a remote",
	Long:  `Sets the username and password for authenticating with the remote named <name>.`,
	Args:  cobra.ExactArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		username := args[1]
		password := args[2]

		// Get repository root
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Set credentials
		if err := remote.SetRemoteAuth(repoRoot, name, username, password); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Stored credentials for remote '%s'\n", name)
	},
}

// listRemotes lists all configured remotes
func listRemotes(verbose bool) error {
	// Get repository root
	repoRoot, err := utils.GetVecRoot()
	if err != nil {
		return err
	}

	// Get remotes info
	remotes, err := remote.ListRemotes(repoRoot)
	if err != nil {
		return err
	}

	// No remotes configured
	if len(remotes) == 0 {
		fmt.Println("No remotes configured")
		return nil
	}

	// Display remotes
	for name, info := range remotes {
		if verbose {
			fmt.Printf("%s\t%s\n", name, info.URL)
		} else {
			fmt.Println(name)
		}
	}

	return nil
}

// isValidRemoteName checks if a remote name is valid
func isValidRemoteName(name string) bool {
	if len(name) == 0 {
		return false
	}

	// Check for invalid characters
	invalid := []string{
		" ", "~", "^", ":", "?", "*", "[", "\\", 
		"/", "@", "{", ".", "..", "\"", ">", "<"}
	
	for _, char := range invalid {
		if strings.Contains(name, char) {
			return false
		}
	}

	return true
}

func init() {
	// Add the remote command to the root command
	rootCmd.AddCommand(remoteCmd)

	// Add subcommands
	remoteCmd.AddCommand(addRemoteCmd)
	remoteCmd.AddCommand(removeRemoteCmd)
	remoteCmd.AddCommand(showRemoteCmd)
	remoteCmd.AddCommand(setUrlCmd)
	remoteCmd.AddCommand(setCredentialsCmd)

	// Add flags
	remoteCmd.Flags().BoolVarP(&remoteVerbose, "verbose", "v", false, "Show remote URL after name")
}
