package objects

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NahomAnteneh/vec/utils"
)

type Commit struct {
	CommitID  string // Hash of the serialized data. Calculated, *not* stored.
	Tree      string
	Parents   []string
	Author    string
	Message   string
	Timestamp int64
}

// serialize serializes the commit object into a byte slice, *excluding* CommitID.
func (c *Commit) serialize() ([]byte, error) { // Now returns an error.
	var buf bytes.Buffer

	// Tree (length + string)
	treeBytes := []byte(c.Tree)
	treeLength := uint32(len(treeBytes))
	if err := binary.Write(&buf, binary.LittleEndian, treeLength); err != nil {
		return nil, fmt.Errorf("failed to write tree length: %w", err)
	}
	if _, err := buf.Write(treeBytes); err != nil {
		return nil, fmt.Errorf("failed to write tree: %w", err)
	}

	// Parent IDs (count + IDs)
	parentCount := uint32(len(c.Parents))
	if err := binary.Write(&buf, binary.LittleEndian, parentCount); err != nil {
		return nil, fmt.Errorf("failed to write parent count: %w", err)
	}
	for _, parentID := range c.Parents {
		parentIDBytes := []byte(parentID)
		parentIDLength := uint32(len(parentIDBytes))
		if err := binary.Write(&buf, binary.LittleEndian, parentIDLength); err != nil {
			return nil, fmt.Errorf("failed to write parent ID length: %w", err)
		}
		if _, err := buf.Write(parentIDBytes); err != nil {
			return nil, fmt.Errorf("failed to write parent ID: %w", err)
		}
	}

	// Author (length + string)
	authorBytes := []byte(c.Author)
	authorLength := uint32(len(authorBytes))
	if err := binary.Write(&buf, binary.LittleEndian, authorLength); err != nil {
		return nil, fmt.Errorf("failed to write author length: %w", err)
	}
	if _, err := buf.Write(authorBytes); err != nil {
		return nil, fmt.Errorf("failed to write author: %w", err)
	}

	// Timestamp
	if err := binary.Write(&buf, binary.LittleEndian, c.Timestamp); err != nil {
		return nil, fmt.Errorf("failed to write timestamp: %w", err)
	}

	// Message (length + string)
	messageBytes := []byte(c.Message)
	messageLength := uint32(len(messageBytes))
	if err := binary.Write(&buf, binary.LittleEndian, messageLength); err != nil {
		return nil, fmt.Errorf("failed to write message length: %w", err)
	}
	if _, err := buf.Write(messageBytes); err != nil {
		return nil, fmt.Errorf("failed to write message: %w", err)
	}

	return buf.Bytes(), nil
}

// deserializeCommit deserializes a byte slice into a Commit object.
func deserializeCommit(data []byte) (*Commit, error) {
	buf := bytes.NewReader(data)
	commit := &Commit{}

	// Tree
	var treeLength uint32
	if err := binary.Read(buf, binary.LittleEndian, &treeLength); err != nil {
		return nil, fmt.Errorf("failed to read tree hash length: %w", err)
	}
	treeBytes := make([]byte, treeLength)
	if _, err := buf.Read(treeBytes); err != nil {
		return nil, fmt.Errorf("failed to read tree hash: %w", err)
	}
	commit.Tree = string(treeBytes)

	// Parent IDs
	var parentCount uint32
	if err := binary.Read(buf, binary.LittleEndian, &parentCount); err != nil {
		return nil, fmt.Errorf("failed to read parent count: %w", err)
	}
	commit.Parents = make([]string, parentCount)
	for i := uint32(0); i < parentCount; i++ {
		var parentIDLength uint32
		if err := binary.Read(buf, binary.LittleEndian, &parentIDLength); err != nil {
			return nil, fmt.Errorf("failed to read parent ID length: %w", err)
		}
		parentIDBytes := make([]byte, parentIDLength)
		if _, err := buf.Read(parentIDBytes); err != nil {
			return nil, fmt.Errorf("failed to read parent ID: %w", err)
		}
		commit.Parents[i] = string(parentIDBytes)
	}

	// Author
	var authorLength uint32
	if err := binary.Read(buf, binary.LittleEndian, &authorLength); err != nil {
		return nil, fmt.Errorf("failed to read author length: %w", err)
	}
	authorBytes := make([]byte, authorLength)
	if _, err := buf.Read(authorBytes); err != nil {
		return nil, fmt.Errorf("failed to read author: %w", err)
	}
	commit.Author = string(authorBytes)

	// Timestamp
	if err := binary.Read(buf, binary.LittleEndian, &commit.Timestamp); err != nil {
		return nil, fmt.Errorf("failed to read timestamp: %w", err)
	}

	// Message
	var messageLength uint32
	if err := binary.Read(buf, binary.LittleEndian, &messageLength); err != nil {
		return nil, fmt.Errorf("failed to read message length: %w", err)
	}
	messageBytes := make([]byte, messageLength)
	if _, err := buf.Read(messageBytes); err != nil {
		return nil, fmt.Errorf("failed to read message: %w", err)
	}
	commit.Message = string(messageBytes)

	return commit, nil
}

// CreateCommit creates a new commit object, including the header.
func CreateCommit(repoRoot, treeHash string, parentHashes []string, author, message string, timestamp int64) (string, error) {

	commit := &Commit{
		Tree:      treeHash,
		Parents:   parentHashes,
		Author:    author, // Now using author directly.
		Message:   message,
		Timestamp: timestamp,
	}

	data, err := commit.serialize() // Get the serialized data.
	if err != nil {
		return "", err
	}

	header := fmt.Sprintf("commit %d\n", len(data))
	var buf bytes.Buffer
	buf.WriteString(header) // Prepend the header.
	buf.Write(data)
	content := buf.Bytes()

	hash := utils.HashBytes("commit", content) // Hash the *entire* content (header + data).
	commit.CommitID = hash                     // Set the CommitID.

	objectPath := GetObjectPath(repoRoot, hash)
	objectDir := filepath.Dir(objectPath)

	if err := utils.EnsureDirExists(objectDir); err != nil {
		return "", err
	}

	if err := os.WriteFile(objectPath, content, 0644); err != nil {
		return "", fmt.Errorf("failed to write commit file: %w", err)
	}

	return hash, nil // Return the *hash* (the CommitID).
}

// GetCommit reads a commit object from disk.
func GetCommit(repoRoot string, hash string) (*Commit, error) {
	objectPath := GetObjectPath(repoRoot, hash)
	content, err := os.ReadFile(objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read commit file: %w", err)
	}

	headerEnd := bytes.IndexByte(content, '\n')
	if headerEnd == -1 {
		return nil, fmt.Errorf("invalid commit format: missing header")
	}
	commitContent := content[headerEnd+1:]

	commit, err := deserializeCommit(commitContent)
	if err != nil {
		return nil, err
	}
	commit.CommitID = hash // Set commit id after creating commit object

	return commit, nil
}

// GetObjectPath returns the path to a commit object.
func GetObjectPath(repoRoot string, hash string) string {
	return filepath.Join(repoRoot, ".vec", "objects", hash[:2], hash[2:])
}

// GetCommitTime is a helper function that returns commit time
func (c *Commit) GetCommitTime() time.Time {
	return time.Unix(c.Timestamp, 0)
}
