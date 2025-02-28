package objects

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/NahomAnteneh/vec/internal/core"
)

func TestCreateGetCommit(t *testing.T) {
	testDir, err := os.MkdirTemp("", "vec-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(testDir)

	// Create a .vec for testing
	if err := os.MkdirAll(filepath.Join(testDir, ".vec"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create a commit.
	treeHash := "treehash" // Replace with a real tree hash in a more complete test.
	parentHashes := []string{"parenthash1"}
	author := "Test Author <test@example.com>"
	message := "Test commit message\n\nWith multiple lines."
	timestamp := time.Now().Unix()

	// Create some files to simulate the file and tree structure
	testFiles := map[string]string{
		"file1.txt":           "This is file1",
		"dir1/file2.txt":      "This is file2 in dir1",
		"dir1/dir2/file3.txt": "This is file3 in dir1/dir2",
	}
	for filePath, content := range testFiles {
		absPath := filepath.Join(testDir, filePath)
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Create index and add file
	index, err := core.ReadIndex(testDir)
	if err != nil {
		t.Fatal(err)
	}
	for filePath := range testFiles {
		if err := index.Add(testDir, filePath); err != nil { //Use relative path
			t.Fatal(err)
		}
	}
	if err := index.Write(); err != nil {
		t.Fatal(err)
	}

	// Create tree based on this index
	treeHash, err = CreateTree(testDir, index)
	if err != nil {
		t.Fatal(err)
	}

	commitHash, err := CreateCommit(testDir, treeHash, parentHashes, author, message, timestamp)
	if err != nil {
		t.Fatalf("CreateCommit() failed: %v", err)
	}

	if commitHash == "" {
		t.Fatal("CreateCommit() returned an empty hash")
	}

	// Get the commit.
	commit, err := GetCommit(testDir, commitHash)
	if err != nil {
		t.Fatalf("GetCommit() failed: %v", err)
	}

	// Verify the CommitID
	if commit.CommitID != commitHash {
		t.Errorf("Expected CommitID '%s', got '%s'", commitHash, commit.CommitID)
	}

	// Verify the commit contents.
	if commit.Tree != treeHash {
		t.Errorf("Expected tree hash '%s', got '%s'", treeHash, commit.Tree)
	}
	if !reflect.DeepEqual(commit.Parents, parentHashes) {
		t.Errorf("Expected parents '%v', got '%v'", parentHashes, commit.Parents)
	}
	if commit.Author != author {
		t.Errorf("Expected author '%s', got '%s'", author, commit.Author)
	}
	if commit.Message != message {
		t.Errorf("Expected message '%s', got '%s'", message, commit.Message)
	}
	if commit.Timestamp != timestamp {
		t.Errorf("Expected timestamp '%d', got '%d'", timestamp, commit.Timestamp)
	}
	if commit.GetCommitTime().Unix() != timestamp {
		t.Errorf("Expected time '%s', got '%s'", time.Unix(timestamp, 0), commit.GetCommitTime())
	}

	// Test invalid hash
	_, err = GetCommit(testDir, "invalid-hash")
	if err == nil {
		t.Fatalf("Expected error for invalid hash but got nil")
	}

	// Test serialize and deserialize directly
	serialized, err := commit.serialize()
	if err != nil {
		t.Fatalf("Error on serialize: %v", err)
	}
	deserialized, err := deserializeCommit(serialized)
	if err != nil {
		t.Fatalf("deserializeCommit() failed: %v", err)
	}
	deserialized.CommitID = commit.CommitID // Set commit id, because it is not serialized
	if !reflect.DeepEqual(commit, deserialized) {
		t.Errorf("Original and deserialized commits are not equal")
		t.Errorf("Original: %+v\n", commit)           // Print the original commit
		t.Errorf("Deserialized: %+v\n", deserialized) //Print the deserialized commit
	}

	// Test case with an empty message
	emptyMessageCommitHash, err := CreateCommit(testDir, treeHash, parentHashes, author, "", timestamp)
	if err != nil {
		t.Fatalf("Create commit failed with empty message")
	}
	emptyMessageCommit, err := GetCommit(testDir, emptyMessageCommitHash)
	if err != nil {
		t.Fatalf("Get commit with empty message failed: %v", err)
	}
	if emptyMessageCommit.Message != "" {
		t.Fatalf("Expected empty string but got %s", emptyMessageCommit.Message)
	}
}

func TestGetObjectPath(t *testing.T) {
	testDir, err := os.MkdirTemp("", "vec-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(testDir)

	// Create a .vec for testing
	if err := os.MkdirAll(filepath.Join(testDir, ".vec"), 0755); err != nil {
		t.Fatal(err)
	}

	hash := "abcdef1234567890"
	expectedPath := filepath.Join(testDir, ".vec", "objects", "ab", "cdef1234567890")
	actualPath := GetObjectPath(testDir, hash)
	if actualPath != expectedPath {
		t.Errorf("Expected path '%s', got '%s'", expectedPath, actualPath)
	}

	//Test with empty repoRoot
	emptyRepoRootPath := GetObjectPath("", hash)
	expectedEmptyRepoRootPath := filepath.Join(".vec", "objects", "ab", "cdef1234567890")
	if emptyRepoRootPath != expectedEmptyRepoRootPath {
		t.Errorf("Expected path '%s', got '%s'", expectedEmptyRepoRootPath, emptyRepoRootPath)
	}
}
