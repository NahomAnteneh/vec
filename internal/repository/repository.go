package repository

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NahomAnteneh/vec/utils"
)

func CreateRepository(dir string) error {
	vecDir := filepath.Join(dir, ".vec")

	// Check if repository already exists.
	if utils.FileExists(vecDir) {
		return fmt.Errorf("vec repository already initialized at %s", dir)
	}

	// Create .vec directory.
	if err := os.MkdirAll(vecDir, 0755); err != nil {
		return fmt.Errorf("failed to create .vec directory: %w", err)
	}

	// Create subdirectories.
	subDirs := []string{
		filepath.Join(vecDir, "objects"),
		filepath.Join(vecDir, "refs", "heads"),
		filepath.Join(vecDir, "refs", "remotes"),
		filepath.Join(vecDir, "logs", "refs", "heads"),
		filepath.Join(vecDir, "logs"),
	}
	for _, subDir := range subDirs {
		if err := os.MkdirAll(subDir, 0755); err != nil {
			return fmt.Errorf("failed to create subdirectory %s: %w", subDir, err)
		}
	}

	// Create HEAD file.
	headFile := filepath.Join(vecDir, "HEAD")
	if err := os.WriteFile(headFile, []byte("ref: refs/heads/main\n"), 0644); err != nil {
		return fmt.Errorf("failed to create HEAD file: %w", err)
	}

	// Create logs/HEAD file
	logHeadFile := filepath.Join(vecDir, "logs", "HEAD")
	if err := os.WriteFile(logHeadFile, []byte(""), 0644); err != nil { // Initialize as empty
		return fmt.Errorf("failed to create logs/HEAD file: %w", err)
	}

	fmt.Printf("Initialized empty Vec repository in %s\n", vecDir)
	return nil
}

func CreateRepositoryBare(dir string) error {
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

	// Create subdirectories directly in the specified directory
	subDirs := []string{
		filepath.Join(dir, "objects"),
		filepath.Join(dir, "refs", "heads"),
		filepath.Join(dir, "refs", "remotes"),
		filepath.Join(dir, "logs", "refs", "heads"),
		filepath.Join(dir, "logs"),
	}
	for _, subDir := range subDirs {
		if err := os.MkdirAll(subDir, 0755); err != nil {
			return fmt.Errorf("failed to create subdirectory %s: %w", subDir, err)
		}
	}

	// Create HEAD file
	headFile := filepath.Join(dir, "HEAD")
	if err := os.WriteFile(headFile, []byte("ref: refs/heads/main\n"), 0644); err != nil {
		return fmt.Errorf("failed to create HEAD file: %w", err)
	}

	// Create logs/HEAD file
	logHeadFile := filepath.Join(dir, "logs", "HEAD")
	if err := os.WriteFile(logHeadFile, []byte(""), 0644); err != nil { // Initialize as empty
		return fmt.Errorf("failed to create logs/HEAD file: %w", err)
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
