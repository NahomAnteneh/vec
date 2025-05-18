package repository

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/utils"
)

// createCommonDirectories creates the standard directory structure for a Vec repository
func createCommonDirectories(baseDir string) error {
	// Create subdirectories
	subDirs := []string{
		filepath.Join(baseDir, "objects"),
		filepath.Join(baseDir, "objects", "pack"),
		filepath.Join(baseDir, "objects", "info"),
		filepath.Join(baseDir, "refs", "heads"),
		filepath.Join(baseDir, "refs", "remotes"),
		filepath.Join(baseDir, "logs", "refs", "heads"),
		filepath.Join(baseDir, "logs"),
	}
	
	for _, subDir := range subDirs {
		if err := os.MkdirAll(subDir, 0755); err != nil {
			return fmt.Errorf("failed to create subdirectory %s: %w", subDir, err)
		}
	}
	
	// Create common files
	files := map[string]string{
		filepath.Join(baseDir, "objects", "info", "packs"):      "",
		filepath.Join(baseDir, "objects", "info", "alternates"): "",
		filepath.Join(baseDir, "HEAD"):                          "ref: refs/heads/main\n",
		filepath.Join(baseDir, "logs", "HEAD"):                  "",
	}
	
	for file, content := range files {
		if err := os.WriteFile(file, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to create file %s: %w", file, err)
		}
	}
	
	return nil
}

// CreateRepo initializes a new Vec repository using Repository context
func CreateRepo(repo *core.Repository) error {
	vecDir := repo.VecDir

	// Check if repository already exists.
	if utils.FileExists(vecDir) {
		return fmt.Errorf("vec repository already initialized at %s", repo.Root)
	}

	// Create .vec directory.
	if err := os.MkdirAll(vecDir, 0755); err != nil {
		return fmt.Errorf("failed to create .vec directory: %w", err)
	}

	// Create common directory structure
	if err := createCommonDirectories(vecDir); err != nil {
		return err
	}

	fmt.Printf("Initialized empty Vec repository in %s\n", vecDir)
	return nil
}

// CreateBareRepo creates a bare repository using Repository context
func CreateBareRepo(repo *core.Repository) error {
	dir := repo.Root

	// Check if directory already exists.
	if utils.FileExists(dir) {
		// If the directory exists, check if it's empty
		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("failed to read directory %s: %w", dir, err)
		}

		if len(entries) > 0 {
			return fmt.Errorf("directory %s is not empty", dir)
		}
	} else {
		// Create the directory if it doesn't exist
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Create common directory structure
	if err := createCommonDirectories(dir); err != nil {
		return err
	}

	// Create a config file with bare=true
	configFile := filepath.Join(dir, "config")
	config := "[core]\n\tbare = true\n"
	if err := os.WriteFile(configFile, []byte(config), 0644); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	fmt.Printf("Initialized empty bare Vec repository in %s\n", dir)
	return nil
}
