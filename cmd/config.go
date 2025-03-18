// cmd/config.go
package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/config"
	"github.com/NahomAnteneh/vec/internal/remote"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

// ConfigScope represents the scope of configuration
type ConfigScope string

const (
	// ScopeLocal represents repository-specific configuration stored in .vec/config
	ScopeLocal ConfigScope = "local"

	// ScopeGlobal represents user-specific configuration stored in ~/.vecconfig
	ScopeGlobal ConfigScope = "global"

	// ScopeSystem represents system-wide configuration stored in /etc/vec/config
	ScopeSystem ConfigScope = "system"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage Vec configuration",
	Long: `Manage Vec configuration files and settings.
Configuration can be stored in three different scopes:
- local: Repository-specific configuration (stored in .vec/config)
- global: User-specific configuration (stored in ~/.vecconfig)
- system: System-wide configuration (stored in /etc/vec/config)

Configuration values can be set using the following syntax:
  section.key=value
  section.subsection.key=value

Example:
  vec config user.name "John Doe"
  vec config --global user.email "john@example.com"
  vec config remote.origin.url "https://example.com/repo.git"`,
}

var listCmd = &cobra.Command{
	Use:   "list [scope]",
	Short: "List all configuration settings",
	Long: `List all configuration settings in the specified scope.
If no scope is specified, defaults to local configuration.
Valid scopes are: local, global, system`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		scope := getConfigScope(cmd, args)
		config, err := getConfigForScope(scope)
		if err != nil {
			return err
		}

		// Group settings by section
		sections := make(map[string]map[string]string)
		for key, value := range config {
			parts := strings.SplitN(key, ".", 2)
			section := parts[0]
			if len(parts) > 1 {
				if _, ok := sections[section]; !ok {
					sections[section] = make(map[string]string)
				}
				sections[section][parts[1]] = value
			}
		}

		// Print settings grouped by section
		for section, settings := range sections {
			fmt.Printf("[%s]\n", section)
			for key, value := range settings {
				fmt.Printf("    %s = %s\n", key, value)
			}
			fmt.Println()
		}

		return nil
	},
}

var getCmd = &cobra.Command{
	Use:   "get <key> [scope]",
	Short: "Get a configuration value",
	Long: `Get the value of a configuration key.
The key can be specified in the format section.key or section.subsection.key.
If no scope is specified, defaults to local configuration.
Valid scopes are: local, global, system`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		scope := getConfigScope(cmd, args[1:])

		value, err := getConfigValue(key, scope)
		if err != nil {
			return err
		}

		fmt.Println(value)
		return nil
	},
}

var setCmd = &cobra.Command{
	Use:   "set <key> <value> [scope]",
	Short: "Set a configuration value",
	Long: `Set a configuration value for the specified key.
The key can be specified in the format section.key or section.subsection.key.
If no scope is specified, defaults to local configuration.
Valid scopes are: local, global, system`,
	Args: cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		value := args[1]
		scope := getConfigScope(cmd, args[2:])

		if err := setConfigValue(key, value, scope); err != nil {
			return err
		}

		fmt.Printf("Set %s.%s = %s\n", scope, key, value)
		return nil
	},
}

var unsetCmd = &cobra.Command{
	Use:   "unset <key> [scope]",
	Short: "Remove a configuration value",
	Long: `Remove a configuration value for the specified key.
The key can be specified in the format section.key or section.subsection.key.
If no scope is specified, defaults to local configuration.
Valid scopes are: local, global, system`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		scope := getConfigScope(cmd, args[1:])

		if err := unsetConfigValue(key, scope); err != nil {
			return err
		}

		fmt.Printf("Unset %s.%s\n", scope, key)
		return nil
	},
}

var editCmd = &cobra.Command{
	Use:   "edit [scope]",
	Short: "Edit configuration file directly",
	Long: `Open the configuration file in your default editor.
If no scope is specified, defaults to local configuration.
Valid scopes are: local, global, system`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		scope := getConfigScope(cmd, args)
		configPath, err := getConfigPath(scope)
		if err != nil {
			return err
		}

		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vim" // Default to vim if no editor is set
		}

		execCmd := exec.Command(editor, configPath)
		execCmd.Stdin = os.Stdin
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr
		return execCmd.Run()
	},
}

// Helper functions
func getConfigScope(cmd *cobra.Command, args []string) ConfigScope {
	if len(args) > 0 {
		scope := ConfigScope(strings.ToLower(args[0]))
		if scope == ScopeLocal || scope == ScopeGlobal || scope == ScopeSystem {
			return scope
		}
	}

	global, _ := cmd.Parent().PersistentFlags().GetBool("global")
	if global {
		return ScopeGlobal
	}

	return ScopeLocal
}

func getConfigPath(scope ConfigScope) (string, error) {
	switch scope {
	case ScopeLocal:
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return "", err
		}
		return filepath.Join(repoRoot, ".vec", "config"), nil
	case ScopeGlobal:
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, ".veconfig"), nil
	case ScopeSystem:
		return "/etc/vec/config", nil
	default:
		return "", fmt.Errorf("invalid config scope: %s", scope)
	}
}

func getConfigForScope(scope ConfigScope) (map[string]string, error) {
	configPath, err := getConfigPath(scope)
	if err != nil {
		return nil, err
	}

	if !utils.FileExists(configPath) {
		// Create parent directories for global and system scopes
		if scope == ScopeGlobal || scope == ScopeSystem {
			configDir := filepath.Dir(configPath)
			if err := os.MkdirAll(configDir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create config directory: %w", err)
			}
		}
		return make(map[string]string), nil
	}

	return utils.ReadConfig(configPath)
}

func getConfigValue(key string, scope ConfigScope) (string, error) {
	config, err := getConfigForScope(scope)
	if err != nil {
		return "", err
	}

	value, ok := config[key]
	if !ok {
		return "", fmt.Errorf("key '%s' not found in %s config", key, scope)
	}

	return value, nil
}

// getCascadingConfigValue retrieves a configuration value by checking local, then global, then system configs
func getCascadingConfigValue(key string) (string, string, error) {
	// Try local config first
	localConfig, err := getConfigForScope(ScopeLocal)
	if err == nil {
		if value, ok := localConfig[key]; ok {
			return value, "local", nil
		}
	}

	// Try global config next
	globalConfig, err := getConfigForScope(ScopeGlobal)
	if err == nil {
		if value, ok := globalConfig[key]; ok {
			return value, "global", nil
		}
	}

	// Try system config last
	systemConfig, err := getConfigForScope(ScopeSystem)
	if err == nil {
		if value, ok := systemConfig[key]; ok {
			return value, "system", nil
		}
	}

	return "", "", fmt.Errorf("key '%s' not found in any configuration scope", key)
}

// GetCascadingConfigValue is an exported version of getCascadingConfigValue for use by other packages
func GetCascadingConfigValue(key string) (string, error) {
	value, _, err := getCascadingConfigValue(key)
	return value, err
}

func setConfigValue(key, value string, scope ConfigScope) error {
	configPath, err := getConfigPath(scope)
	if err != nil {
		return err
	}

	config, err := getConfigForScope(scope)
	if err != nil {
		return err
	}

	config[key] = value
	return utils.WriteConfig(configPath, config)
}

func unsetConfigValue(key string, scope ConfigScope) error {
	configPath, err := getConfigPath(scope)
	if err != nil {
		return err
	}

	config, err := getConfigForScope(scope)
	if err != nil {
		return err
	}

	delete(config, key)
	return utils.WriteConfig(configPath, config)
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

// jwtCmd manages JWT authentication for remotes
var jwtCmd = &cobra.Command{
	Use:   "jwt",
	Short: "Manage JWT authentication for remotes",
	Long:  `Manage JWT authentication for remote repositories, allowing you to set, validate, and inspect tokens.`,
}

// jwtSetCmd sets a JWT token for a remote
var jwtSetCmd = &cobra.Command{
	Use:   "set <remote-name> <token>",
	Short: "Set JWT token for a remote",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		remoteName := args[0]
		token := args[1]
		scope := getConfigScope(cmd, nil)

		// Validate the token format (simple check)
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			return fmt.Errorf("invalid JWT token format: token should have three parts separated by dots")
		}

		// Store the token using the remote package's method for credentials file
		if err := remote.StoreAuthToken(remoteName, token); err != nil {
			return fmt.Errorf("failed to store token in credentials file: %v", err)
		}

		// Get appropriate config path based on scope
		configPath, err := getConfigPath(scope)
		if err != nil {
			return fmt.Errorf("failed to determine config path: %w", err)
		}

		// If using local config, use the config package
		if scope == ScopeLocal {
			repoRoot, err := utils.GetVecRoot()
			if err != nil {
				return err
			}

			cfg, err := config.LoadConfig(repoRoot)
			if err != nil {
				return fmt.Errorf("failed to load config: %v", err)
			}

			if err := cfg.SetRemoteAuth(remoteName, token); err != nil {
				return err
			}

			if err := cfg.Write(); err != nil {
				return fmt.Errorf("failed to save config: %v", err)
			}
		} else {
			// For global/system configs, handle directly
			configData, err := utils.ReadConfig(configPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to read config: %v", err)
			}

			if configData == nil {
				configData = make(map[string]string)
			}

			// Set auth token for the remote
			remoteAuthKey := fmt.Sprintf("remote.%s.auth", remoteName)
			configData[remoteAuthKey] = token

			// Ensure parent directory exists for the config file
			configDir := filepath.Dir(configPath)
			if err := os.MkdirAll(configDir, 0755); err != nil {
				return fmt.Errorf("failed to create config directory: %w", err)
			}

			if err := utils.WriteConfig(configPath, configData); err != nil {
				return fmt.Errorf("failed to write config: %v", err)
			}
		}

		fmt.Printf("Set JWT token for remote '%s' in %s config\n", remoteName, scope)
		return nil
	},
}

// jwtInfoCmd shows information about a JWT token
var jwtInfoCmd = &cobra.Command{
	Use:   "info <remote-name>",
	Short: "Show information about a remote's JWT token",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		remoteName := args[0]
		scope := getConfigScope(cmd, nil)

		// Try to get the token from credentials file first
		token, err := remote.GetAuthToken(remoteName)
		if err != nil || token == "" {
			// Fall back to config based on scope
			configPath, err := getConfigPath(scope)
			if err != nil {
				return fmt.Errorf("failed to determine config path: %w", err)
			}

			// If using local config, use the config package
			if scope == ScopeLocal {
				repoRoot, err := utils.GetVecRoot()
				if err != nil {
					return err
				}

				cfg, err := config.LoadConfig(repoRoot)
				if err != nil {
					return fmt.Errorf("failed to load config: %v", err)
				}

				token, err = cfg.GetRemoteAuth(remoteName)
				if err != nil {
					return fmt.Errorf("failed to get auth token: %v", err)
				}
			} else {
				// For global/system configs, handle directly
				configData, err := utils.ReadConfig(configPath)
				if err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("failed to read config: %v", err)
				}

				if configData != nil {
					remoteAuthKey := fmt.Sprintf("remote.%s.auth", remoteName)
					token = configData[remoteAuthKey]
				}
			}
		}

		if token == "" {
			return fmt.Errorf("no JWT token found for remote '%s' in %s config", remoteName, scope)
		}

		// Parse and display JWT token information
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			return fmt.Errorf("invalid JWT token format")
		}

		// Decode the payload (second part)
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			fmt.Println("JWT Token (could not decode payload):")
			fmt.Printf("  Token: %s\n", token)
			return nil
		}

		// Parse the JSON payload
		var claims map[string]interface{}
		if err := json.Unmarshal(payload, &claims); err != nil {
			fmt.Println("JWT Token (could not parse claims):")
			fmt.Printf("  Token: %s\n", token)
			return nil
		}

		// Display token information
		fmt.Println("JWT Token Information:")
		fmt.Printf("  Remote: %s\n", remoteName)
		fmt.Printf("  Config Scope: %s\n", scope)

		// Check for standard claims
		if sub, ok := claims["sub"].(string); ok {
			fmt.Printf("  Subject: %s\n", sub)
		}

		if iss, ok := claims["iss"].(string); ok {
			fmt.Printf("  Issuer: %s\n", iss)
		}

		if exp, ok := claims["exp"].(float64); ok {
			expTime := time.Unix(int64(exp), 0)
			fmt.Printf("  Expires: %s", expTime.Format(time.RFC1123))
			if time.Now().After(expTime) {
				fmt.Printf(" (EXPIRED)\n")
			} else {
				fmt.Printf(" (valid)\n")
			}
		}

		if iat, ok := claims["iat"].(float64); ok {
			issuedAt := time.Unix(int64(iat), 0)
			fmt.Printf("  Issued At: %s\n", issuedAt.Format(time.RFC1123))
		}

		// Display other claims
		fmt.Println("  Additional Claims:")
		for key, value := range claims {
			if key != "sub" && key != "iss" && key != "exp" && key != "iat" {
				fmt.Printf("    %s: %v\n", key, value)
			}
		}

		return nil
	},
}

// jwtValidateCmd validates a JWT token
var jwtValidateCmd = &cobra.Command{
	Use:   "validate <remote-name>",
	Short: "Validate a remote's JWT token",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		remoteName := args[0]
		scope := getConfigScope(cmd, nil)

		// Try to get the token from credentials file first
		token, err := remote.GetAuthToken(remoteName)
		if err != nil || token == "" {
			// Fall back to config based on scope
			configPath, err := getConfigPath(scope)
			if err != nil {
				return fmt.Errorf("failed to determine config path: %w", err)
			}

			// If using local config, use the config package
			if scope == ScopeLocal {
				repoRoot, err := utils.GetVecRoot()
				if err != nil {
					return err
				}

				cfg, err := config.LoadConfig(repoRoot)
				if err != nil {
					return fmt.Errorf("failed to load config: %v", err)
				}

				token, err = cfg.GetRemoteAuth(remoteName)
				if err != nil {
					return fmt.Errorf("failed to get auth token: %v", err)
				}
			} else {
				// For global/system configs, handle directly
				configData, err := utils.ReadConfig(configPath)
				if err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("failed to read config: %v", err)
				}

				if configData != nil {
					remoteAuthKey := fmt.Sprintf("remote.%s.auth", remoteName)
					token = configData[remoteAuthKey]
				}
			}
		}

		if token == "" {
			return fmt.Errorf("no JWT token found for remote '%s' in %s config", remoteName, scope)
		}

		// Basic validation checks
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			return fmt.Errorf("invalid JWT token format")
		}

		// Check expiration if available
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err == nil {
			var claims map[string]interface{}
			if err := json.Unmarshal(payload, &claims); err == nil {
				if exp, ok := claims["exp"].(float64); ok {
					expTime := time.Unix(int64(exp), 0)
					if time.Now().After(expTime) {
						return fmt.Errorf("token for remote '%s' in %s config has expired on %s",
							remoteName, scope, expTime.Format(time.RFC1123))
					}
				}
			}
		}

		fmt.Printf("JWT token for remote '%s' in %s config is valid\n", remoteName, scope)
		return nil
	},
}

// jwtClearCmd removes a JWT token
var jwtClearCmd = &cobra.Command{
	Use:   "clear <remote-name>",
	Short: "Clear JWT token for a remote",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		remoteName := args[0]
		scope := getConfigScope(cmd, nil)

		// Get appropriate config path based on scope
		configPath, err := getConfigPath(scope)
		if err != nil {
			return fmt.Errorf("failed to determine config path: %w", err)
		}

		// If using local config, load from repo config
		if scope == ScopeLocal {
			repoRoot, err := utils.GetVecRoot()
			if err != nil {
				return err
			}

			cfg, err := config.LoadConfig(repoRoot)
			if err != nil {
				return fmt.Errorf("failed to load config: %v", err)
			}

			if err := cfg.SetRemoteAuth(remoteName, ""); err != nil {
				return err
			}

			if err := cfg.Write(); err != nil {
				return fmt.Errorf("failed to save config: %v", err)
			}
		} else {
			// For global/system configs, we need to handle directly
			// This assumes utils.ReadConfig and utils.WriteConfig can handle this
			configData, err := utils.ReadConfig(configPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to read config: %v", err)
			}

			if configData == nil {
				configData = make(map[string]string)
			}

			// Remove auth token from the remote
			remoteAuthKey := fmt.Sprintf("remote.%s.auth", remoteName)
			delete(configData, remoteAuthKey)

			// Ensure parent directory exists for the config file
			configDir := filepath.Dir(configPath)
			if err := os.MkdirAll(configDir, 0755); err != nil {
				return fmt.Errorf("failed to create config directory: %w", err)
			}

			if err := utils.WriteConfig(configPath, configData); err != nil {
				return fmt.Errorf("failed to write config: %v", err)
			}
		}

		// Clear from credentials file
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}

		credsPath := filepath.Join(homeDir, ".vec", "credentials")
		if utils.FileExists(credsPath) {
			if err := remote.StoreAuthToken(remoteName, ""); err != nil {
				return fmt.Errorf("failed to clear token from credentials file: %v", err)
			}
		}

		fmt.Printf("Cleared JWT token for remote '%s' in %s config\n", remoteName, scope)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)

	// Add basic config commands
	configCmd.AddCommand(listCmd)
	configCmd.AddCommand(getCmd)
	configCmd.AddCommand(setCmd)
	configCmd.AddCommand(unsetCmd)
	configCmd.AddCommand(editCmd)

	// Add remote-related commands
	configCmd.AddCommand(remoteAuthCmd)
	configCmd.AddCommand(remoteHeaderCmd)

	// Add JWT commands
	configCmd.AddCommand(jwtCmd)
	jwtCmd.AddCommand(jwtSetCmd)
	jwtCmd.AddCommand(jwtInfoCmd)
	jwtCmd.AddCommand(jwtValidateCmd)
	jwtCmd.AddCommand(jwtClearCmd)

	// Add scope flag to configCmd
	configCmd.PersistentFlags().BoolP("global", "g", false, "Use global config file")
	configCmd.PersistentFlags().BoolP("system", "s", false, "Use system config file")

	// Add user configuration commands
	configCmd.AddCommand(&cobra.Command{
		Use:   "user.name <name> [scope]",
		Short: "Set the user's name",
		Long:  `Set the user's name for commits.`,
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := getConfigScope(cmd, args[1:])
			return setConfigValue("user.name", args[0], scope)
		},
	})

	configCmd.AddCommand(&cobra.Command{
		Use:   "user.email <email> [scope]",
		Short: "Set the user's email",
		Long:  `Set the user's email for commits.`,
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := getConfigScope(cmd, args[1:])
			return setConfigValue("user.email", args[0], scope)
		},
	})
}
