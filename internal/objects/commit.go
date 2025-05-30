package objects

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/utils"
)

// Commit represents a commit object in the repository.
type Commit struct {
	CommitID  string   // Hash of the serialized commit data (calculated, not stored)
	Tree      string   // Hash of the tree object
	Parents   []string // Hashes of parent commits
	Author    string   // Author name and email (e.g., "Author Name <author@example.com>")
	Committer string   // Committer name and email (e.g., "Committer Name <committer@example.com>")
	Message   string   // Commit message
	Timestamp int64    // Commit timestamp (Unix time)
}

// serialize serializes the commit object into a byte slice, excluding CommitID.
func (c *Commit) serialize() ([]byte, error) {
	var buf bytes.Buffer

	// Tree (length-prefixed string)
	if err := writeLengthPrefixedString(&buf, c.Tree); err != nil {
		return nil, fmt.Errorf("failed to write tree: %w", err)
	}

	// Parents (count + length-prefixed strings)
	parentCount := uint32(len(c.Parents))
	if err := binary.Write(&buf, binary.LittleEndian, parentCount); err != nil {
		return nil, fmt.Errorf("failed to write parent count: %w", err)
	}
	for _, parent := range c.Parents {
		if err := writeLengthPrefixedString(&buf, parent); err != nil {
			return nil, fmt.Errorf("failed to write parent: %w", err)
		}
	}

	// Author (length-prefixed string)
	if err := writeLengthPrefixedString(&buf, c.Author); err != nil {
		return nil, fmt.Errorf("failed to write author: %w", err)
	}

	// Committer (length-prefixed string)
	if err := writeLengthPrefixedString(&buf, c.Committer); err != nil {
		return nil, fmt.Errorf("failed to write committer: %w", err)
	}

	// Timestamp (int64)
	if err := binary.Write(&buf, binary.LittleEndian, c.Timestamp); err != nil {
		return nil, fmt.Errorf("failed to write timestamp: %w", err)
	}

	// Message (length-prefixed string)
	if err := writeLengthPrefixedString(&buf, c.Message); err != nil {
		return nil, fmt.Errorf("failed to write message: %w", err)
	}

	return buf.Bytes(), nil
}

// deserializeCommit deserializes a byte slice into a Commit object.
func deserializeCommit(data []byte) (*Commit, error) {
	buf := bytes.NewReader(data)
	commit := &Commit{}

	// Tree
	var err error
	commit.Tree, err = readLengthPrefixedString(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read tree: %w", err)
	}

	// Parents
	var parentCount uint32
	if err := binary.Read(buf, binary.LittleEndian, &parentCount); err != nil {
		return nil, fmt.Errorf("failed to read parent count: %w", err)
	}
	commit.Parents = make([]string, parentCount)
	for i := uint32(0); i < parentCount; i++ {
		commit.Parents[i], err = readLengthPrefixedString(buf)
		if err != nil {
			return nil, fmt.Errorf("failed to read parent: %w", err)
		}
	}

	// Author
	commit.Author, err = readLengthPrefixedString(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read author: %w", err)
	}

	// Committer
	commit.Committer, err = readLengthPrefixedString(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read committer: %w", err)
	}

	// Timestamp
	if err := binary.Read(buf, binary.LittleEndian, &commit.Timestamp); err != nil {
		return nil, fmt.Errorf("failed to read timestamp: %w", err)
	}

	// Message
	commit.Message, err = readLengthPrefixedString(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read message: %w", err)
	}

	return commit, nil
}

// CreateCommitRepo creates a new commit object using Repository context.
func CreateCommitRepo(repo *core.Repository, treeHash string, parentHashes []string, author, committer, message string, timestamp int64) (string, error) {
	// Validate inputs
	if treeHash == "" {
		return "", fmt.Errorf("tree hash cannot be empty")
	}
	
	if author == "" || committer == "" {
		return "", fmt.Errorf("author and committer cannot be empty")
	}
	
	if timestamp == 0 {
		timestamp = time.Now().Unix()
	}

	commit := &Commit{
		Tree:      treeHash,
		Parents:   parentHashes,
		Author:    author,
		Committer: committer,
		Message:   message,
		Timestamp: timestamp,
	}

	// Serialize the commit data
	data, err := commit.serialize()
	if err != nil {
		return "", fmt.Errorf("failed to serialize commit: %w", err)
	}

	// Prepend the header
	header := fmt.Sprintf("commit %d\x00", len(data))
	var buf bytes.Buffer
	buf.WriteString(header)
	buf.Write(data)
	content := buf.Bytes()

	// Compute the hash of the entire content
	hash := utils.HashBytes(content)
	commit.CommitID = hash

	// Check if the object already exists
	objectPath := GetObjectPathRepo(repo, hash)
	if utils.FileExists(objectPath) {
		return hash, nil
	}

	// Store the commit object on disk
	objectDir := filepath.Dir(objectPath)
	if err := utils.EnsureDirExists(objectDir); err != nil {
		return "", fmt.Errorf("failed to create directory for commit: %w", err)
	}
	
	// Create a temporary file for atomic write
	tempPath := objectPath + ".tmp"
	if err := os.WriteFile(tempPath, content, 0644); err != nil {
		os.Remove(tempPath)
		return "", fmt.Errorf("failed to write commit file: %w", err)
	}
	
	// Atomic rename
	if err := os.Rename(tempPath, objectPath); err != nil {
		os.Remove(tempPath)
		return "", fmt.Errorf("failed to finalize commit file: %w", err)
	}

	return hash, nil
}

// GetCommitRepo reads a commit object from disk using Repository context.
func GetCommitRepo(repo *core.Repository, hash string) (*Commit, error) {
	objectPath := GetObjectPathRepo(repo, hash)
	content, err := os.ReadFile(objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read commit file: %w", err)
	}

	// Find the header end
	headerEnd := bytes.IndexByte(content, '\x00')
	if headerEnd == -1 {
		return nil, fmt.Errorf("invalid commit format: missing header")
	}
	
	// Validate header format
	header := string(content[:headerEnd])
	expectedHeader := fmt.Sprintf("commit %d", len(content)-headerEnd-1)
	if header != expectedHeader {
		return nil, fmt.Errorf("invalid commit header: got '%s', expected '%s'", header, expectedHeader)
	}
	
	commitContent := content[headerEnd+1:]

	// Deserialize the commit
	commit, err := deserializeCommit(commitContent)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize commit: %w", err)
	}
	commit.CommitID = hash
	return commit, nil
}

// GetCommitTime returns the commit time as a time.Time object.
func (c *Commit) GetCommitTime() time.Time {
	return time.Unix(c.Timestamp, 0)
}

// writeLengthPrefixedString writes a length-prefixed string to the buffer.
func writeLengthPrefixedString(buf *bytes.Buffer, s string) error {
	strBytes := []byte(s)
	length := uint32(len(strBytes))
	if err := binary.Write(buf, binary.LittleEndian, length); err != nil {
		return err
	}
	if _, err := buf.Write(strBytes); err != nil {
		return err
	}
	return nil
}

// readLengthPrefixedString reads a length-prefixed string from the buffer.
func readLengthPrefixedString(buf *bytes.Reader) (string, error) {
	var length uint32
	if err := binary.Read(buf, binary.LittleEndian, &length); err != nil {
		return "", err
	}
	strBytes := make([]byte, length)
	if _, err := buf.Read(strBytes); err != nil {
		return "", err
	}
	return string(strBytes), nil
}
