// cmd/config.go
package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/internal/config"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage Vec configuration",
	// This command only serves as a parent for subcommands.
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists configuration key-value pairs",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}

		// Get the global flag from configCmd
		global, err := cmd.Parent().PersistentFlags().GetBool("global")
		if err != nil {
			return err
		}

		var config map[string]string
		if global {
			config, err = utils.ReadGlobalConfig()
		} else {
			configPath := filepath.Join(repoRoot, ".vec", "config")
			config, err = utils.ReadConfig(configPath)
		}
		if err != nil {
			return err
		}

		for key, value := range config {
			fmt.Printf("%s = %s\n", key, value)
		}
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get the value of a configuration key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}
		global, err := cmd.Parent().PersistentFlags().GetBool("global")
		if err != nil {
			return err
		}

		key := args[0]
		var value string
		if global {
			config, err := utils.ReadGlobalConfig()
			if err != nil {
				return err
			}
			val, ok := config[key]
			if !ok {
				return fmt.Errorf("config key %s is not set globally", key)
			}
			value = val
		} else {
			value, err = utils.GetConfigValue(repoRoot, key)
			if err != nil {
				return err
			}
		}
		fmt.Println(value)
		return nil
	},
}

var unsetCmd = &cobra.Command{
	Use:   "unset <key>",
	Short: "Remove a configuration key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}
		global, err := cmd.Parent().PersistentFlags().GetBool("global")
		if err != nil {
			return err
		}

		key := args[0]
		if err := utils.UnsetConfigValue(repoRoot, key, global); err != nil {
			return err
		}
		fmt.Printf("Unset: %s\n", key)
		return nil
	},
}

// remoteAuthCmd adds authentication token to a remote repository
var remoteAuthCmd = &cobra.Command{
	Use:   "remote.auth <remote-name> <token>",
	Short: "Set authentication token for a remote",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}

		remoteName := args[0]
		authToken := args[1]

		cfg, err := config.LoadConfig(repoRoot)
		if err != nil {
			return fmt.Errorf("failed to load config: %v", err)
		}

		if err := cfg.SetRemoteAuth(remoteName, authToken); err != nil {
			return err
		}

		if err := cfg.Write(); err != nil {
			return fmt.Errorf("failed to save config: %v", err)
		}

		fmt.Printf("Set auth token for remote '%s'\n", remoteName)
		return nil
	},
}

// remoteHeaderCmd adds a custom HTTP header to a remote repository
var remoteHeaderCmd = &cobra.Command{
	Use:   "remote.header <remote-name> <header-name> <header-value>",
	Short: "Set custom HTTP header for a remote",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}

		remoteName := args[0]
		headerName := args[1]
		headerValue := args[2]

		cfg, err := config.LoadConfig(repoRoot)
		if err != nil {
			return fmt.Errorf("failed to load config: %v", err)
		}

		// For Authorization header specifically
		if strings.EqualFold(headerName, "Authorization") {
			if err := cfg.SetRemoteAuth(remoteName, headerValue); err != nil {
				return err
			}
		} else {
			if err := cfg.SetRemoteHeader(remoteName, headerName, headerValue); err != nil {
				return err
			}
		}

		if err := cfg.Write(); err != nil {
			return fmt.Errorf("failed to save config: %v", err)
		}

		fmt.Printf("Set header '%s' for remote '%s'\n", headerName, remoteName)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(listCmd)
	configCmd.AddCommand(getCmd)
	configCmd.AddCommand(unsetCmd)
	configCmd.AddCommand(remoteAuthCmd)
	configCmd.AddCommand(remoteHeaderCmd)

	// Add the --global flag to configCmd
	configCmd.PersistentFlags().BoolP("global", "g", false, "Use global config file")

	// Nested command to set user.name
	configCmd.AddCommand(&cobra.Command{
		Use:   "user.name <n>",
		Short: "Set the user's name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := utils.GetVecRoot()
			if err != nil {
				return err
			}
			// Get the --global flag from configCmd's persistent flags.
			global, err := cmd.Parent().PersistentFlags().GetBool("global")
			if err != nil {
				return err
			}
			return utils.SetConfigValue(repoRoot, "user.name", strings.TrimSpace(args[0]), global)
		},
	})

	// Nested command to set user.email
	configCmd.AddCommand(&cobra.Command{
		Use:   "user.email <email>",
		Short: "Set the user's email",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := utils.GetVecRoot()
			if err != nil {
				return err
			}
			global, err := cmd.Parent().PersistentFlags().GetBool("global")
			if err != nil {
				return err
			}
			return utils.SetConfigValue(repoRoot, "user.email", strings.TrimSpace(args[0]), global)
		},
	})
}
