package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NahomAnteneh/vec/utils"
)

// TestCreateRepository tests successful repository creation
func TestCreateRepository(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "vec-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up after test

	// Test repository creation
	err = CreateRepository(tempDir)
	if err != nil {
		t.Errorf("CreateRepository failed: %v", err)
	}

	// Verify the repository structure
	vecDir := filepath.Join(tempDir, ".vec")
	expectedDirs := []string{
		filepath.Join(vecDir, "objects"),
		filepath.Join(vecDir, "refs", "heads"),
		filepath.Join(vecDir, "refs", "remotes"),
		filepath.Join(vecDir, "logs", "refs", "heads"),
		filepath.Join(vecDir, "logs"),
	}

	for _, dir := range expectedDirs {
		if !utils.FileExists(dir) {
			t.Errorf("Expected directory %s does not exist", dir)
		}
	}

	// Check HEAD file exists and has correct content
	headFile := filepath.Join(vecDir, "HEAD")
	if !utils.FileExists(headFile) {
		t.Errorf("HEAD file does not exist")
	}

	headContent, err := os.ReadFile(headFile)
	if err != nil {
		t.Errorf("Failed to read HEAD file: %v", err)
	}

	expectedHeadContent := "ref: refs/heads/main\n"
	if string(headContent) != expectedHeadContent {
		t.Errorf("HEAD file content incorrect. Expected: %q, Got: %q", expectedHeadContent, string(headContent))
	}

	// Check logs/HEAD file exists
	logHeadFile := filepath.Join(vecDir, "logs", "HEAD")
	if !utils.FileExists(logHeadFile) {
		t.Errorf("logs/HEAD file does not exist")
	}
}

// TestCreateRepositoryBare tests successful bare repository creation
func TestCreateRepositoryBare(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "vec-test-bare-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up after test

	// Test bare repository creation
	err = CreateRepositoryBare(tempDir)
	if err != nil {
		t.Errorf("CreateRepositoryBare failed: %v", err)
	}

	// Verify the repository structure
	expectedDirs := []string{
		filepath.Join(tempDir, "objects"),
		filepath.Join(tempDir, "refs", "heads"),
		filepath.Join(tempDir, "refs", "remotes"),
		filepath.Join(tempDir, "logs", "refs", "heads"),
		filepath.Join(tempDir, "logs"),
	}

	for _, dir := range expectedDirs {
		if !utils.FileExists(dir) {
			t.Errorf("Expected directory %s does not exist", dir)
		}
	}

	// Check HEAD file exists and has correct content
	headFile := filepath.Join(tempDir, "HEAD")
	if !utils.FileExists(headFile) {
		t.Errorf("HEAD file does not exist")
	}

	headContent, err := os.ReadFile(headFile)
	if err != nil {
		t.Errorf("Failed to read HEAD file: %v", err)
	}

	expectedHeadContent := "ref: refs/heads/main\n"
	if string(headContent) != expectedHeadContent {
		t.Errorf("HEAD file content incorrect. Expected: %q, Got: %q", expectedHeadContent, string(headContent))
	}

	// Check logs/HEAD file exists
	logHeadFile := filepath.Join(tempDir, "logs", "HEAD")
	if !utils.FileExists(logHeadFile) {
		t.Errorf("logs/HEAD file does not exist")
	}

	// Check config file exists and has bare = true
	configFile := filepath.Join(tempDir, "config")
	if !utils.FileExists(configFile) {
		t.Errorf("config file does not exist")
	}

	configContent, err := os.ReadFile(configFile)
	if err != nil {
		t.Errorf("Failed to read config file: %v", err)
	}

	expectedConfigContent := "[core]\n\tbare = true\n"
	if string(configContent) != expectedConfigContent {
		t.Errorf("config file content incorrect. Expected: %q, Got: %q", expectedConfigContent, string(configContent))
	}
}

// TestCreateRepositoryAlreadyExists tests error handling when repo already exists
func TestCreateRepositoryAlreadyExists(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "vec-test-repo-exists-*")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up after test

	// Create a repository first
	err = CreateRepository(tempDir)
	if err != nil {
		t.Fatalf("Failed to create initial repository: %v", err)
	}

	// Try to create another repository in the same directory
	err = CreateRepository(tempDir)
	if err == nil {
		t.Errorf("Expected error when creating repository in existing location, but got nil")
	}
}

// TestCreateRepositoryBareNonEmptyDir tests error handling when directory is not empty
func TestCreateRepositoryBareNonEmptyDir(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "vec-test-bare-nonempty-*")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up after test

	// Create a dummy file to make the directory non-empty
	dummyFile := filepath.Join(tempDir, "dummy.txt")
	err = os.WriteFile(dummyFile, []byte("dummy content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create dummy file: %v", err)
	}

	// Try to create a bare repository in the non-empty directory
	err = CreateRepositoryBare(tempDir)
	if err == nil {
		t.Errorf("Expected error when creating bare repository in non-empty directory, but got nil")
	}
}
