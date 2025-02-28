package core

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NahomAnteneh/vec/utils"
)

// Index represents the staging area (index) for the repository.
type Index struct {
	Entries []IndexEntry
	Path    string // Path to the index file
}

// IndexEntry represents a single entry in the index.
type IndexEntry struct {
	Mode     int32     // File mode (as int32 for binary encoding)
	Filename string    // File path *relative to the repository root*
	SHA256   string    // SHA-256 hash of the blob
	Size     int64     // File size
	Mtime    time.Time // Last modification time
}

// ReadIndex reads the index file from disk, or returns an empty index if none exists.
func ReadIndex(repoRoot string) (*Index, error) {
	indexPath := filepath.Join(repoRoot, ".vec", "index")
	index := &Index{Path: indexPath}

	if !utils.FileExists(indexPath) {
		return index, nil // Return an empty index if the file doesn't exist
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read index file: %w", err)
	}

	buf := bytes.NewBuffer(data)
	for buf.Len() > 0 {
		var entry IndexEntry
		var mode int32

		if err := binary.Read(buf, binary.BigEndian, &mode); err != nil {
			return nil, fmt.Errorf("failed to read mode: %w", err)
		}
		entry.Mode = mode

		filenameBytes, err := buf.ReadBytes(0)
		if err != nil {
			return nil, fmt.Errorf("failed to read filename: %w", err)
		}
		entry.Filename = string(filenameBytes[:len(filenameBytes)-1])

		shaBytes := make([]byte, 64)
		if _, err := buf.Read(shaBytes); err != nil {
			return nil, fmt.Errorf("failed to read SHA256: %w", err)
		}
		entry.SHA256 = string(shaBytes)

		if err := binary.Read(buf, binary.BigEndian, &entry.Size); err != nil {
			return nil, fmt.Errorf("failed to read size: %w", err)
		}

		var mtime int64
		if err := binary.Read(buf, binary.BigEndian, &mtime); err != nil {
			return nil, fmt.Errorf("failed to read mtime: %w", err)
		}
		entry.Mtime = time.Unix(mtime, 0)

		index.Entries = append(index.Entries, entry)
	}

	return index, nil
}

// Write writes the index to disk.
func (i *Index) Write() error {
	buf := new(bytes.Buffer)

	for _, entry := range i.Entries {
		if err := binary.Write(buf, binary.BigEndian, entry.Mode); err != nil {
			return fmt.Errorf("failed to write mode: %w", err)
		}

		if _, err := buf.WriteString(entry.Filename); err != nil {
			return fmt.Errorf("failed to write filename: %w", err)
		}
		if err := buf.WriteByte(0); err != nil {
			return fmt.Errorf("failed to write null terminator: %w", err)
		}

		if _, err := buf.WriteString(entry.SHA256); err != nil {
			return fmt.Errorf("failed to write SHA256: %w", err)
		}

		if err := binary.Write(buf, binary.BigEndian, entry.Size); err != nil {
			return fmt.Errorf("failed to write size: %w", err)
		}

		if err := binary.Write(buf, binary.BigEndian, entry.Mtime.Unix()); err != nil {
			return fmt.Errorf("failed to write mtime: %w", err)
		}
	}

	if err := os.WriteFile(i.Path, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}
	return nil
}

// Add adds or updates an entry in the index.
func (i *Index) Add(repoRoot, filePath string) error {
	fullPath := filepath.Join(repoRoot, filePath)    // Get absolute path for os operations
	relPath, err := filepath.Rel(repoRoot, fullPath) // Get *relative* path for storage
	if err != nil {
		return fmt.Errorf("failed to get relative path: %w", err)
	}

	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	hash, err := utils.HashFile(fullPath) // Hash the *full* file content.
	if err != nil {
		return err // HashFile already wraps the error
	}

	for j, entry := range i.Entries {
		if entry.Filename == relPath { // Compare *relative* paths
			i.Entries[j].SHA256 = hash
			i.Entries[j].Size = fileInfo.Size()
			i.Entries[j].Mtime = fileInfo.ModTime()
			return nil // Entry updated, we're done.
		}
	}

	newEntry := IndexEntry{
		Mode:     0644,    // Default to regular file.
		Filename: relPath, // Store the *relative* path.
		SHA256:   hash,
		Size:     fileInfo.Size(),
		Mtime:    fileInfo.ModTime(),
	}
	i.Entries = append(i.Entries, newEntry)
	return nil
}

// Remove removes an entry from the index.
func (i *Index) Remove(repoRoot, filePath string) error {

	relPath, err := filepath.Rel(repoRoot, filePath) // Get *relative* path for storage
	if err != nil {
		return fmt.Errorf("failed to get relative path: %w", err)
	}

	found := false
	for j, entry := range i.Entries {
		if entry.Filename == relPath { // Compare *relative* paths
			// Remove the entry by slicing.  This is efficient in Go.
			i.Entries = append(i.Entries[:j], i.Entries[j+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("did not match any files")
	}
	return nil
}

// GetStaged returns a list of staged files (relative paths).
func (i *Index) GetStaged() []string {
	stagedFiles := make([]string, 0, len(i.Entries))
	for _, entry := range i.Entries {
		stagedFiles = append(stagedFiles, entry.Filename)
	}
	return stagedFiles
}

// IsClean checks if the index is empty.
func (i *Index) IsClean() bool {
	return len(i.Entries) == 0
}

// GetIndexFilePath returns the absolute path to the index file.
func GetIndexFilePath(repoRoot string) string {
	return filepath.Join(repoRoot, ".vec", "index")
}

// GetAllEntries returns all entries in the index.
func (i *Index) GetAllEntries() []IndexEntry {
	return i.Entries
}
