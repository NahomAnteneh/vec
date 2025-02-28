package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NahomAnteneh/vec/utils"
)

func TestReadWriteIndex(t *testing.T) {
	// Create a temporary directory for testing.  Important for isolation.
	testDir, err := os.MkdirTemp("", "vec-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(testDir) // Clean up after the test.

	// Create a .vec for testing
	if err := os.MkdirAll(filepath.Join(testDir, ".vec"), 0755); err != nil {
		t.Fatal(err)
	}

	index := &Index{Path: GetIndexFilePath(testDir)}

	// Add some entries.
	entry1 := IndexEntry{Mode: 0644, Filename: "file1.txt", SHA256: "hash1", Size: 10, Mtime: time.Now()}
	entry2 := IndexEntry{Mode: 0755, Filename: "src/file2.txt", SHA256: "hash2", Size: 20, Mtime: time.Now()}
	index.Entries = append(index.Entries, entry1, entry2)

	// Write the index.
	if err := index.Write(); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}

	// Read the index back.
	readIndex, err := ReadIndex(testDir)
	if err != nil {
		t.Fatalf("ReadIndex() failed: %v", err)
	}

	// Compare the read index with the original.
	if len(readIndex.Entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(readIndex.Entries))
	}
	if readIndex.Entries[0].Filename != "file1.txt" {
		t.Errorf("Expected filename 'file1.txt', got '%s'", readIndex.Entries[0].Filename)
	}
	if readIndex.Entries[1].Mode != 0755 {
		t.Errorf("Expected mode '0755', got '%d'", readIndex.Entries[1].Mode)
	}
	// ... (add more comparisons for other fields) ...

	//Test Empty index
	emptyIndex := &Index{Path: GetIndexFilePath(testDir)}
	if err := emptyIndex.Write(); err != nil {
		t.Fatalf("Write empty index failed: %v", err)
	}

	readEmptyIndex, err := ReadIndex(testDir)
	if err != nil {
		t.Fatalf("Read empty index failed: %v", err)
	}
	if len(readEmptyIndex.Entries) != 0 {
		t.Fatalf("Expected 0 entries, got %d", len(readEmptyIndex.Entries))
	}
}

func TestAdd(t *testing.T) {
	testDir, err := os.MkdirTemp("", "vec-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(testDir)

	// Create a .vec for testing
	if err := os.MkdirAll(filepath.Join(testDir, ".vec"), 0755); err != nil {
		t.Fatal(err)
	}
	// Create files for testing
	file1Path := filepath.Join(testDir, "file1.txt")
	file2Path := filepath.Join(testDir, "dir", "file2.txt")
	os.Mkdir(filepath.Join(testDir, "dir"), 0755)
	os.WriteFile(file1Path, []byte("File1 content"), 0644)
	os.WriteFile(file2Path, []byte("File2 content"), 0644)

	index := &Index{Path: GetIndexFilePath(testDir)}

	err = index.Add(testDir, "file1.txt")
	if err != nil {
		t.Fatalf("Failed to add file: %v", err)
	}
	if len(index.Entries) != 1 {
		t.Fatalf("Expected 1 entry, but got %d", len(index.Entries))
	}
	if index.Entries[0].Filename != "file1.txt" {
		t.Fatalf("Expected 'file1.txt' , but got %s", index.Entries[0].Filename)
	}
	//Test adding file to sub directory
	err = index.Add(testDir, filepath.Join("dir", "file2.txt"))
	if err != nil {
		t.Fatalf("Failed to add file in subdirectory : %v", err)
	}

	if len(index.Entries) != 2 {
		t.Fatalf("Expected 2 entry, but got %d", len(index.Entries))
	}
	if index.Entries[1].Filename != filepath.Join("dir", "file2.txt") {
		t.Fatalf("Expected '%s' , but got %s", filepath.Join("dir", "file2.txt"), index.Entries[1].Filename)
	}

	//Test adding not exist file
	err = index.Add(testDir, "file3.txt")
	if err == nil {
		t.Fatalf("Expected error but got nil")
	}

	//Test adding already added file
	// Modify file content
	if err := os.WriteFile(file1Path, []byte("New content"), 0644); err != nil {
		t.Fatal(err)
	}

	err = index.Add(testDir, "file1.txt")
	if err != nil {
		t.Fatalf("Expected nil but got %v", err)
	}
	if len(index.Entries) != 2 {
		t.Fatalf("Expected 2 entry, but got %d", len(index.Entries))
	}
	newHash, _ := utils.HashFile(file1Path)
	if index.Entries[0].SHA256 != newHash {
		t.Fatalf("Hash didn't change after modifying: expected '%s', got '%s'", newHash, index.Entries[0].SHA256)
	}
}

func TestRemove(t *testing.T) {
	// Create a temporary directory for testing.  Important for isolation.
	testDir, err := os.MkdirTemp("", "vec-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(testDir) // Clean up after the test.

	// Create a .vec for testing
	if err := os.MkdirAll(filepath.Join(testDir, ".vec"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create files for testing
	file1Path := filepath.Join(testDir, "file1.txt")
	file2Path := filepath.Join(testDir, "dir", "file2.txt")
	os.Mkdir(filepath.Join(testDir, "dir"), 0755)
	os.WriteFile(file1Path, []byte("File1 content"), 0644)
	os.WriteFile(file2Path, []byte("File2 content"), 0644)

	index := &Index{Path: GetIndexFilePath(testDir)}

	// Add files to the index
	index.Add(testDir, "file1.txt")
	index.Add(testDir, filepath.Join("dir", "file2.txt"))

	// Remove file
	err = index.Remove(testDir, "file1.txt")
	if err != nil {
		t.Fatalf("Failed to remove file: %v", err)
	}

	if len(index.Entries) != 1 {
		t.Fatalf("Expected 1 entry, but got: %d", len(index.Entries))
	}
	if index.Entries[0].Filename == "file1.txt" {
		t.Fatalf("Expected file to be removed")
	}

	// Remove file in sub directory
	err = index.Remove(testDir, filepath.Join("dir", "file2.txt"))
	if err != nil {
		t.Fatalf("Failed to remove file in sub directory: %v", err)
	}
	if len(index.Entries) != 0 {
		t.Fatalf("Expected 0 entry, but got: %d", len(index.Entries))
	}

	// Remove not exist file
	err = index.Remove(testDir, "file.txt")
	if err == nil {
		t.Fatalf("Expected error, but got nil")
	}
}
