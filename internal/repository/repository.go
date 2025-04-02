package repository

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/utils"
)

// CreateRepository initializes a new Vec repository at the specified directory (legacy function)
func CreateRepository(dir string) error {
	repo := core.NewRepository(dir)
	return CreateRepo(repo)
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

	// Create subdirectories.
	subDirs := []string{
		filepath.Join(vecDir, "objects"),
		filepath.Join(vecDir, "objects", "pack"),
		filepath.Join(vecDir, "objects", "info"),
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

	// Create an empty packfile index to initialize proper structure
	packInfoFile := filepath.Join(vecDir, "objects", "info", "packs")
	if err := os.WriteFile(packInfoFile, []byte(""), 0644); err != nil {
		return fmt.Errorf("failed to create packfile info file: %w", err)
	}

	// Create an empty alternates file for potential alternate object stores
	alternatesFile := filepath.Join(vecDir, "objects", "info", "alternates")
	if err := os.WriteFile(alternatesFile, []byte(""), 0644); err != nil {
		return fmt.Errorf("failed to create alternates file: %w", err)
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

// CreateRepositoryBare creates a bare repository at the specified directory (legacy function)
func CreateRepositoryBare(dir string) error {
	repo := core.NewRepository(dir)
	return CreateBareRepo(repo)
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

	// Create subdirectories directly in the specified directory
	subDirs := []string{
		filepath.Join(dir, "objects"),
		filepath.Join(dir, "objects", "pack"),
		filepath.Join(dir, "objects", "info"),
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

	// Create an empty packfile index to initialize proper structure
	packInfoFile := filepath.Join(dir, "objects", "info", "packs")
	if err := os.WriteFile(packInfoFile, []byte(""), 0644); err != nil {
		return fmt.Errorf("failed to create packfile info file: %w", err)
	}

	// Create an empty alternates file for potential alternate object stores
	alternatesFile := filepath.Join(dir, "objects", "info", "alternates")
	if err := os.WriteFile(alternatesFile, []byte(""), 0644); err != nil {
		return fmt.Errorf("failed to create alternates file: %w", err)
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
