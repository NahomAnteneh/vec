package core

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/utils"
)

// Index represents the staging area (index) in the repository.
type Index struct {
	Entries []IndexEntry // List of entries in the index
	Path    string       // Path to the index file (e.g., .vec/index)
}

// IndexEntry represents a single entry in the index.
type IndexEntry struct {
	Mode     int32     // File mode (e.g., 100644 for regular file)
	FilePath string    // Relative file path (e.g., "dir/file.txt")
	SHA256   string    // SHA-256 hash of the file content
	Size     int64     // File size in bytes
	Mtime    time.Time // Last modification time
	Stage    int       // Conflict stage: 0 = merged, 1 = base, 2 = ours, 3 = theirs
	BaseSHA  string    // SHA of the base version (for conflicts)
	OurSHA   string    // SHA of our version (for conflicts)
	TheirSHA string    // SHA of their version (for conflicts)
}

// NewIndex creates a new, empty Index.
func NewIndex(repoRoot string) *Index {
	return &Index{
		Entries: []IndexEntry{},
		Path:    filepath.Join(repoRoot, ".vec", "index"),
	}
}

// LoadIndex reads the index from disk or returns a new one if it doesn't exist.
func LoadIndex(repoRoot string) (*Index, error) {
	indexPath := filepath.Join(repoRoot, ".vec", "index")
	if !utils.FileExists(indexPath) {
		return NewIndex(repoRoot), nil
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read index file: %w", err)
	}

	return DeserializeIndex(repoRoot, data)
}

// Write serializes and writes the index to disk.
func (i *Index) Write() error {
	data, err := i.Serialize()
	if err != nil {
		return fmt.Errorf("failed to serialize index: %w", err)
	}
	if err := os.WriteFile(i.Path, data, 0644); err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}
	return nil
}

// Add adds or updates a stage 0 entry in the index for a file.
func (i *Index) Add(repoRoot, relPath, hash string) error {
	absPath := filepath.Join(repoRoot, relPath)
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Determine file mode
	mode := int32(100644) // Default to regular file
	if fileInfo.Mode()&0111 != 0 {
		mode = 100755 // Executable
	}

	// Check for existing entry
	for j, entry := range i.Entries {
		if entry.FilePath == relPath && entry.Stage == 0 {
			// Update existing stage 0 entry
			i.Entries[j].Mode = mode
			i.Entries[j].SHA256 = hash
			i.Entries[j].Size = fileInfo.Size()
			i.Entries[j].Mtime = fileInfo.ModTime()
			i.Entries[j].BaseSHA = ""
			i.Entries[j].OurSHA = ""
			i.Entries[j].TheirSHA = ""
			return nil
		}
	}

	// Add new stage 0 entry
	newEntry := IndexEntry{
		Mode:     mode,
		FilePath: relPath,
		SHA256:   hash,
		Size:     fileInfo.Size(),
		Mtime:    fileInfo.ModTime(),
		Stage:    0,
	}
	i.Entries = append(i.Entries, newEntry)
	return nil
}

// Remove removes a stage 0 entry from the index.
func (i *Index) Remove(repoRoot, relPath string) error {
	for j, entry := range i.Entries {
		if entry.FilePath == relPath && entry.Stage == 0 {
			i.Entries = append(i.Entries[:j], i.Entries[j+1:]...)
			return nil
		}
	}
	return nil // Idempotent: no error if not found
}

// AddConflictEntry adds a conflict entry to the index with a specific stage (1, 2, or 3).
func (i *Index) AddConflictEntry(relPath, hash string, mode int32, stage int) error {
	if stage < 1 || stage > 3 {
		return fmt.Errorf("invalid stage: %d", stage)
	}
	entry := IndexEntry{
		Mode:     mode,
		FilePath: relPath,
		SHA256:   hash,
		Stage:    stage,
	}
	i.Entries = append(i.Entries, entry)
	return nil
}

// GetStagedFiles returns a list of relative file paths for stage 0 entries.
func (i *Index) GetStagedFiles() []string {
	var staged []string
	for _, entry := range i.Entries {
		if entry.Stage == 0 {
			staged = append(staged, entry.FilePath)
		}
	}
	return staged
}

// HasUncommittedChanges checks for uncommitted changes in the working directory or index.
func (i *Index) HasUncommittedChanges(repoRoot string) bool {
	// Get the HEAD commit to compare against
	headCommitID, err := ReadHEAD(repoRoot)
	if err != nil {
		return true // Conservatively assume changes if HEAD can't be read
	}
	var headTree *objects.TreeObject
	if headCommitID != "" {
		headCommit, err := objects.GetCommit(repoRoot, headCommitID)
		if err != nil {
			return true // Conservatively assume changes if commit can't be loaded
		}
		headTree, err = objects.GetTree(repoRoot, headCommit.Tree)
		if err != nil {
			return true // Conservatively assume changes if tree can't be loaded
		}
	}

	// Build a map of HEAD tree entries for comparison
	headTreeMap := make(map[string]string) // filepath -> hash
	if headTree != nil {
		for _, entry := range headTree.Entries {
			if entry.Type == "blob" {
				headTreeMap[entry.FullPath] = entry.Hash
			}
		}
	}

	// Build a map of stage 0 index entries
	indexMap := make(map[string]string) // filepath -> hash
	for _, entry := range i.Entries {
		if entry.Stage == 0 {
			indexMap[entry.FilePath] = entry.SHA256
		}
	}

	// Check for staged changes (index differs from HEAD)
	for path, headHash := range headTreeMap {
		indexHash, exists := indexMap[path]
		if !exists || indexHash != headHash {
			return true // File deleted or modified in index
		}
	}
	for path := range indexMap {
		if _, exists := headTreeMap[path]; !exists {
			return true // File added in index
		}
	}

	// Check for unstaged changes (working directory differs from index)
	for _, entry := range i.Entries {
		if entry.Stage != 0 {
			continue // Skip conflict entries
		}
		absPath := filepath.Join(repoRoot, entry.FilePath)
		fileInfo, err := os.Stat(absPath)
		if os.IsNotExist(err) {
			return true // File in index but missing in working directory
		}
		if err != nil {
			return true // Conservatively assume changes on stat error
		}
		// Check if file has been modified since last indexed
		if fileInfo.ModTime().After(entry.Mtime) {
			content, err := os.ReadFile(absPath)
			if err != nil {
				return true // Assume changes if file can't be read
			}
			currentHash := utils.HashBytes("blob", content) // Compute current file hash
			if currentHash != entry.SHA256 {
				return true // Content differs from index
			}
		}
	}

	// Check for untracked files in the working directory
	err = filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path == filepath.Join(repoRoot, ".vec") {
				return filepath.SkipDir // Skip .vec directory
			}
			return nil
		}
		relPath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		if _, exists := indexMap[relPath]; !exists {
			if _, exists := headTreeMap[relPath]; !exists {
				return fmt.Errorf("untracked file detected") // Break walk with an error
			}
		}
		return nil
	})
	if err != nil && err.Error() == "untracked file detected" {
		return true
	}

	return false // No uncommitted changes found
}

// IsClean returns true if there are no uncommitted changes in the working directory or index.
func (i *Index) IsClean(repoRoot string) bool {
	return !i.HasUncommittedChanges(repoRoot)
}

// Serialize serializes the index to a byte slice.
func (i *Index) Serialize() ([]byte, error) {
	buf := new(bytes.Buffer)

	// Sort entries by FilePath and Stage for consistency
	sort.Slice(i.Entries, func(a, b int) bool {
		if i.Entries[a].FilePath != i.Entries[b].FilePath {
			return i.Entries[a].FilePath < i.Entries[b].FilePath
		}
		return i.Entries[a].Stage < i.Entries[b].Stage
	})

	// Write the number of entries
	numEntries := uint32(len(i.Entries))
	if err := binary.Write(buf, binary.BigEndian, numEntries); err != nil {
		return nil, fmt.Errorf("failed to write entry count: %w", err)
	}

	for _, entry := range i.Entries {
		// Write Mode
		if err := binary.Write(buf, binary.BigEndian, entry.Mode); err != nil {
			return nil, fmt.Errorf("failed to write mode: %w", err)
		}

		// Write FilePath (length-prefixed string)
		filePathBytes := []byte(entry.FilePath)
		filePathLen := uint32(len(filePathBytes))
		if err := binary.Write(buf, binary.BigEndian, filePathLen); err != nil {
			return nil, fmt.Errorf("failed to write file path length: %w", err)
		}
		if _, err := buf.Write(filePathBytes); err != nil {
			return nil, fmt.Errorf("failed to write file path: %w", err)
		}

		// Write SHA256 (fixed 32 bytes)
		shaBytes, err := hex.DecodeString(entry.SHA256)
		if err != nil || len(shaBytes) != 32 {
			return nil, fmt.Errorf("invalid SHA256 hash: %s", entry.SHA256)
		}
		if _, err := buf.Write(shaBytes); err != nil {
			return nil, fmt.Errorf("failed to write SHA256: %w", err)
		}

		// Write Size
		if err := binary.Write(buf, binary.BigEndian, entry.Size); err != nil {
			return nil, fmt.Errorf("failed to write size: %w", err)
		}

		// Write Mtime (Unix timestamp)
		if err := binary.Write(buf, binary.BigEndian, entry.Mtime.Unix()); err != nil {
			return nil, fmt.Errorf("failed to write mtime: %w", err)
		}

		// Write Stage
		if err := binary.Write(buf, binary.BigEndian, int32(entry.Stage)); err != nil {
			return nil, fmt.Errorf("failed to write stage: %w", err)
		}

		// Write conflict SHAs (length-prefixed strings)
		for _, sha := range []string{entry.BaseSHA, entry.OurSHA, entry.TheirSHA} {
			shaBytes := []byte(sha)
			shaLen := uint32(len(shaBytes))
			if err := binary.Write(buf, binary.BigEndian, shaLen); err != nil {
				return nil, fmt.Errorf("failed to write SHA length: %w", err)
			}
			if shaLen > 0 {
				if _, err := buf.Write(shaBytes); err != nil {
					return nil, fmt.Errorf("failed to write SHA: %w", err)
				}
			}
		}
	}

	return buf.Bytes(), nil
}

// DeserializeIndex deserializes a byte slice into an Index.
func DeserializeIndex(repoRoot string, data []byte) (*Index, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("invalid index data: too short")
	}

	buf := bytes.NewReader(data)
	index := NewIndex(repoRoot)

	// Read the number of entries
	var numEntries uint32
	if err := binary.Read(buf, binary.BigEndian, &numEntries); err != nil {
		return nil, fmt.Errorf("failed to read entry count: %w", err)
	}

	for i := uint32(0); i < numEntries; i++ {
		var entry IndexEntry

		// Read Mode
		if err := binary.Read(buf, binary.BigEndian, &entry.Mode); err != nil {
			return nil, fmt.Errorf("failed to read mode: %w", err)
		}

		// Read FilePath
		var filePathLen uint32
		if err := binary.Read(buf, binary.BigEndian, &filePathLen); err != nil {
			return nil, fmt.Errorf("failed to read file path length: %w", err)
		}
		filePathBytes := make([]byte, filePathLen)
		if _, err := buf.Read(filePathBytes); err != nil {
			return nil, fmt.Errorf("failed to read file path: %w", err)
		}
		entry.FilePath = string(filePathBytes)

		// Read SHA256 (32 bytes)
		shaBytes := make([]byte, 32)
		if _, err := buf.Read(shaBytes); err != nil {
			return nil, fmt.Errorf("failed to read SHA256: %w", err)
		}
		entry.SHA256 = hex.EncodeToString(shaBytes)

		// Read Size
		if err := binary.Read(buf, binary.BigEndian, &entry.Size); err != nil {
			return nil, fmt.Errorf("failed to read size: %w", err)
		}

		// Read Mtime
		var mtime int64
		if err := binary.Read(buf, binary.BigEndian, &mtime); err != nil {
			return nil, fmt.Errorf("failed to read mtime: %w", err)
		}
		entry.Mtime = time.Unix(mtime, 0)

		// Read Stage
		var stage int32
		if err := binary.Read(buf, binary.BigEndian, &stage); err != nil {
			return nil, fmt.Errorf("failed to read stage: %w", err)
		}
		entry.Stage = int(stage)

		// Read conflict SHAs
		for _, shaPtr := range []*string{&entry.BaseSHA, &entry.OurSHA, &entry.TheirSHA} {
			var shaLen uint32
			if err := binary.Read(buf, binary.BigEndian, &shaLen); err != nil {
				return nil, fmt.Errorf("failed to read SHA length: %w", err)
			}
			if shaLen == 0 {
				*shaPtr = ""
				continue
			}
			shaBytes := make([]byte, shaLen)
			if _, err := buf.Read(shaBytes); err != nil {
				return nil, fmt.Errorf("failed to read SHA: %w", err)
			}
			*shaPtr = string(shaBytes)
		}

		index.Entries = append(index.Entries, entry)
	}

	return index, nil
}

// // Write serializes the index entries and writes them to the index file.
// func (i *Index) Write() error {
// 	// Serialize the index entries into a binary format
// 	data, err := i.serialize()
// 	if err != nil {
// 		return fmt.Errorf("failed to serialize index: %w", err)
// 	}

// 	// Write the serialized data to the index file
// 	if err := os.WriteFile(i.Path, data, 0644); err != nil {
// 		return fmt.Errorf("failed to write index file at %s: %w", i.Path, err)
// 	}
// 	return nil
// }
