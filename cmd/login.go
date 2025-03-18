package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/NahomAnteneh/vec/internal/remote"
	"github.com/spf13/cobra"
)

var (
	loginUsername    string
	loginRemote      string
	loginStoreGlobal bool
)

// loginCmd represents the login command
var loginCmd = &cobra.Command{
	Use:   "login [<remote>]",
	Short: "Log in to a remote repository",
	Long: `Authenticate with a remote Vec repository server.
	
This command authenticates with a Vec server and stores the credentials
for future use. If no remote is specified, "origin" is used by default.
	
Examples:
  vec login                   # Log in to the default remote (origin)
  vec login custom-remote     # Log in to a specific remote
  vec login -u username       # Log in with a specific username
  vec login --global          # Store credentials globally`,
	Run: func(cmd *cobra.Command, args []string) {
		// Determine which remote to use
		loginRemote := "origin" // Default remote
		if len(args) > 0 {
			loginRemote = args[0]
		}

		// Get username if not provided
		if loginUsername == "" {
			reader := bufio.NewReader(os.Stdin)
			fmt.Printf("Username: ")
			username, err := reader.ReadString('\n')
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading username: %v\n", err)
				os.Exit(1)
			}
			loginUsername = strings.TrimSpace(username)
		}

		// Get password securely
		fmt.Printf("Password: ")

		// Read password from stdin
		reader := bufio.NewReader(os.Stdin)
		passwordStr, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
			os.Exit(1)
		}
		password := strings.TrimSpace(passwordStr)

		// Perform login
		err = remote.LoginToRemote(loginRemote, loginUsername, password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully logged in to %s as %s\n", loginRemote, loginUsername)
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)

	loginCmd.Flags().StringVarP(&loginUsername, "username", "u", "", "Username for login")
	loginCmd.Flags().StringVarP(&loginRemote, "remote", "r", "", "Remote to login to (default: origin)")
	loginCmd.Flags().BoolVarP(&loginStoreGlobal, "global", "g", false, "Store credentials globally")
}
