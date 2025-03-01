package core

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NahomAnteneh/vec/utils"
)

// Index represents the staging area (index).
type Index struct {
	Entries []IndexEntry
	path    string // Path to the index file
}

// IndexEntry represents a single entry in the index.
type IndexEntry struct {
	Mode     int32     // File mode (as int32 for binary encoding)
	Filename string    // File path *absolute* file path
	SHA256   string    // SHA-256 hash of the blob
	Size     int64     // File size
	Mtime    time.Time // Last modification time
}

// ReadIndex is replaced by DeserializeIndex, below.

// Write is replaced by Serialize, below.

// Add adds or updates an entry in the index. filePath is *absolute*.
func (i *Index) Add(repoRoot, absPath, hash string) error {
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Get actual file mode
	mode := int32(fileInfo.Mode().Perm())
	if mode == 0 {
		mode = 0644 // Default if mode cannot be determined
	}

	// Create new entry with current file info
	entry := IndexEntry{
		Mode:     mode,
		Filename: absPath,
		SHA256:   hash,
		Size:     fileInfo.Size(),
		Mtime:    fileInfo.ModTime(),
	}

	// Update existing entry or append new one
	for j, existing := range i.Entries {
		if existing.Filename == absPath {
			// Only update if content actually changed
			if existing.SHA256 != hash ||
				existing.Size != fileInfo.Size() ||
				existing.Mtime != fileInfo.ModTime() {
				i.Entries[j] = entry
			}
			return nil
		}
	}

	// Append new entry
	i.Entries = append(i.Entries, entry)
	return nil
}

// Remove removes an entry from the index. filePath is *absolute*.
func (i *Index) Remove(repoRoot, absPath string) error {
	found := false
	for j, entry := range i.Entries {
		if entry.Filename == absPath { // Compare *absolute* paths.
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

// GetStaged returns a list of staged files (*absolute* paths).
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

// GetAllEntries returns all entries from index
func (i *Index) GetAllEntries() []IndexEntry {
	return i.Entries
}

// SerializeIndex serializes the index to a byte slice.
func (i *Index) SerializeIndex() ([]byte, error) {
	buf := new(bytes.Buffer)

	// Write the number of entries.
	numEntries := uint32(len(i.Entries))
	if err := binary.Write(buf, binary.BigEndian, numEntries); err != nil {
		return nil, fmt.Errorf("failed to write entry count: %w", err)
	}

	for _, entry := range i.Entries {
		// Write Mode
		if err := binary.Write(buf, binary.BigEndian, entry.Mode); err != nil {
			return nil, fmt.Errorf("failed to write mode: %w", err)
		}

		// Write Filename (length-prefixed string)
		filenameBytes := []byte(entry.Filename)
		filenameLen := uint32(len(filenameBytes))
		if err := binary.Write(buf, binary.BigEndian, filenameLen); err != nil {
			return nil, fmt.Errorf("failed to write filename length: %w", err)
		}
		if _, err := buf.Write(filenameBytes); err != nil {
			return nil, fmt.Errorf("failed to write filename: %w", err)
		}

		// Write SHA256 (fixed-size byte array)
		shaBytes, err := hex.DecodeString(entry.SHA256) // Convert hex string to bytes.
		if err != nil {
			return nil, fmt.Errorf("invalid SHA256 hash: %w", err)
		}
		if len(shaBytes) != 32 { // SHA256 should be 32 bytes (256 bits).
			return nil, fmt.Errorf("invalid SHA256 hash length: %d", len(shaBytes))
		}
		if _, err := buf.Write(shaBytes); err != nil {
			return nil, fmt.Errorf("failed to write SHA256: %w", err)
		}

		// Write Size
		if err := binary.Write(buf, binary.BigEndian, entry.Size); err != nil {
			return nil, fmt.Errorf("failed to write size: %w", err)
		}

		// Write Mtime (as Unix timestamp, int64)
		if err := binary.Write(buf, binary.BigEndian, entry.Mtime.Unix()); err != nil {
			return nil, fmt.Errorf("failed to write mtime: %w", err)
		}
	}

	return buf.Bytes(), nil
}

// DeserializeIndex deserializes a byte slice into an Index struct.
func DeserializeIndex(repoRoot string, data []byte) (*Index, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("invalid index data: too short")
	}

	buf := bytes.NewReader(data)
	index := &Index{
		path:    GetIndexFilePath(repoRoot),
		Entries: make([]IndexEntry, 0), // Pre-allocate slice
	}

	var numEntries uint32
	if err := binary.Read(buf, binary.BigEndian, &numEntries); err != nil {
		return nil, fmt.Errorf("failed to read entry count: %w", err)
	}

	// Add size limit check
	if numEntries > 1000000 { // arbitrary limit
		return nil, fmt.Errorf("index too large: %d entries", numEntries)
	}

	index.Entries = make([]IndexEntry, 0, numEntries) // Pre-allocate with capacity
	for i := uint32(0); i < numEntries; i++ {
		var entry IndexEntry

		// Read Mode
		if err := binary.Read(buf, binary.BigEndian, &entry.Mode); err != nil {
			return nil, fmt.Errorf("failed to read mode: %w", err)
		}

		// Read Filename (length-prefixed string)
		var filenameLen uint32
		if err := binary.Read(buf, binary.BigEndian, &filenameLen); err != nil {
			return nil, fmt.Errorf("failed to read filename length: %w", err)
		}
		filenameBytes := make([]byte, filenameLen)
		if _, err := buf.Read(filenameBytes); err != nil {
			return nil, fmt.Errorf("failed to read filename: %w", err)
		}
		entry.Filename = string(filenameBytes)

		// Read SHA256 (fixed-size byte array)
		shaBytes := make([]byte, 32) // SHA-256 is 32 bytes.
		if _, err := buf.Read(shaBytes); err != nil {
			return nil, fmt.Errorf("failed to read SHA256: %w", err)
		}
		entry.SHA256 = hex.EncodeToString(shaBytes) // Convert bytes to hex string.

		// Read Size
		if err := binary.Read(buf, binary.BigEndian, &entry.Size); err != nil {
			return nil, fmt.Errorf("failed to read size: %w", err)
		}

		// Read Mtime (Unix timestamp)
		var mtime int64
		if err := binary.Read(buf, binary.BigEndian, &mtime); err != nil {
			return nil, fmt.Errorf("failed to read mtime: %w", err)
		}
		entry.Mtime = time.Unix(mtime, 0)

		index.Entries = append(index.Entries, entry)
	}

	return index, nil
}

// Added ReadIndex and WriteIndex functions to call serialize and deserialize
func ReadIndex(repoRoot string) (*Index, error) {
	indexPath := GetIndexFilePath(repoRoot)
	if !utils.FileExists(indexPath) {
		return &Index{path: indexPath}, nil // Return empty index.
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read index file: %w", err)
	}

	return DeserializeIndex(repoRoot, data)
}

func (i *Index) Write() error {
	data, err := i.SerializeIndex()
	if err != nil {
		return err
	}
	if err := os.WriteFile(i.path, data, 0644); err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}
	return nil
}
