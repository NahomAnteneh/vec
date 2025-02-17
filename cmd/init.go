package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

const (
	objectsDirName     = "objects"
	refsDirName        = "refs"
	headsDirName       = "heads"
	headFileName       = "HEAD"
	descriptionName    = "description"
	configName         = "config"
	headFileContent    = "ref: refs/heads/main\n"
	descriptionContent = "Unnamed repository; edit this file 'description' to name the repository.\n"
)

// --- Helper Functions ---

// getInitDirectory determines the directory to initialize the repository in.
func getInitDirectory(args []string) (string, error) {
	if len(args) == 0 {
		return os.Getwd()
	}
	dir, err := filepath.Abs(args[0])
	if err != nil {
		return "", &ErrInvalidDirectory{Path: args[0], Err: err}
	}
	return dir, nil
}

// getVecDirectory determines the .vec directory path, handling bare repositories.
func getVecDirectory(dir string, bare bool) (string, error) {
	if bare {
		return dir, nil // For bare repos, the root *is* the .vec
	}
	return filepath.Join(dir, utils.VecDirName), nil
}

// createDirectory creates a directory, handling existing directory errors.
func createDirectory(path string) error {
	if _, err := os.Stat(path); err == nil {
		return &ErrRepositoryExists{Path: path}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check for existing directory '%s': %w", path, err)
	}

	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create directory '%s': %w", path, err)
	}
	return nil
}

// writeFile creates a file and writes content to it.
func writeFile(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file '%s': %w", path, err)
	}
	return nil
}

// initializeRepository creates the necessary directories and files.
func initializeRepository(vecDir string, bare bool) error {
	if err := createDirectory(vecDir); err != nil {
		return err
	}

	if err := createDirectory(filepath.Join(vecDir, objectsDirName)); err != nil {
		return err
	}

	if err := createDirectory(filepath.Join(vecDir, refsDirName, headsDirName)); err != nil {
		return err
	}

	if err := writeFile(filepath.Join(vecDir, headFileName), headFileContent); err != nil {
		return err
	}

	// Bare-specific files
	if bare {
		if err := writeFile(filepath.Join(vecDir, descriptionName), descriptionContent); err != nil {
			return err
		}
	}
	// config file
	configContent := []byte("[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n")
	if bare {
		configContent = append(configContent, []byte("\tbare = true\n")...)
	} else {
		configContent = append(configContent, []byte("\tbare = false\n")...)
	}
	if err := writeFile(filepath.Join(vecDir, configName), string(configContent)); err != nil {
		return err
	}
	return nil
}

// --- Cobra Command ---
var (
	bare bool // Flag to indicate a bare repository
)

var initCmd = &cobra.Command{
	Use:   "init [directory]",
	Short: "Initialize a new Vec repository",
	Long: `Initialize a new Vec repository in the specified directory.
If no directory is provided, the current directory is used.
Use the --bare flag to create a bare repository (no working directory).`,
	Args: cobra.MaximumNArgs(1), // Allow at most one argument (the directory).
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := getInitDirectory(args)
		if err != nil {
			return err
		}

		vecDir, err := getVecDirectory(dir, bare)
		if err != nil {
			return err
		}

		if err := initializeRepository(vecDir, bare); err != nil {
			return err
		}

		if bare {
			fmt.Printf("Initialized empty bare Vec repository in %s\n", vecDir)
		} else {
			fmt.Printf("Initialized empty Vec repository in %s\n", vecDir)
		}
		return nil
	},
}

func init() {
	initCmd.Flags().BoolVar(&bare, "bare", false, "Create a bare repository")
	rootCmd.AddCommand(initCmd)
}
