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

// CreateCommit creates a new commit object, including the header.
func CreateCommit(repoRoot, treeHash string, parentHashes []string, author, committer, message string, timestamp int64) (string, error) {
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
	hash := utils.HashBytes("commit", content)
	commit.CommitID = hash

	// Store the commit object on disk
	objectPath := GetObjectPath(repoRoot, hash)
	objectDir := filepath.Dir(objectPath)
	if err := utils.EnsureDirExists(objectDir); err != nil {
		return "", fmt.Errorf("failed to create directory for commit: %w", err)
	}
	if err := os.WriteFile(objectPath, content, 0644); err != nil {
		return "", fmt.Errorf("failed to write commit file: %w", err)
	}

	return hash, nil
}

// GetCommit reads a commit object from disk.
func GetCommit(repoRoot string, hash string) (*Commit, error) {
	objectPath := GetObjectPath(repoRoot, hash)
	content, err := os.ReadFile(objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read commit file: %w", err)
	}

	// Find the header end
	headerEnd := bytes.IndexByte(content, '\x00')
	if headerEnd == -1 {
		return nil, fmt.Errorf("invalid commit format: missing header")
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

// GetObjectPath returns the path to a commit object.
func GetObjectPath(repoRoot string, hash string) string {
	return filepath.Join(repoRoot, ".vec", "objects", hash[:2], hash[2:])
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
