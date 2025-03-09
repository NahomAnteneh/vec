// // filepath: /home/nahom/Dev/vec/internal/objects/tree.go
package objects

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	// for index types (adjust import path as needed)
	"github.com/NahomAnteneh/vec/utils" // for utility functions (e.g., HashBytes)
)

// TreeEntry represents a single entry in a tree—either a blob (file) or a subtree.
type TreeEntry struct {
	Mode     int32  // File mode (e.g., 100644 for files, 040000 for trees)
	Name     string // Basename (e.g., "file.txt" or directory name)
	Hash     string // SHA-256 hash (hex string) of the blob or subtree.
	Type     string // "blob" or "tree"
	FullPath string // Full relative path (only used when building the map)
}

// TreeObject represents a Git-style tree object.
type TreeObject struct {
	TreeID  string
	Entries []TreeEntry
}

// NewTreeObject creates a new, empty TreeObject.
func NewTreeObject() *TreeObject {
	return &TreeObject{
		Entries: make([]TreeEntry, 0),
	}
}

// Serialize converts the TreeObject into a Git-compatible byte slice.
// Format: <mode> <name>\x00<hash> for each entry, sorted by name for deterministic hashing.
func (t *TreeObject) Serialize() ([]byte, error) {
	if t == nil {
		return nil, fmt.Errorf("cannot serialize nil TreeObject")
	}

	var buf bytes.Buffer

	// Sort entries by name to ensure consistent serialization (Git standard)
	sort.Slice(t.Entries, func(i, j int) bool {
		return t.Entries[i].Name < t.Entries[j].Name
	})

	// Serialize each entry
	for _, entry := range t.Entries {
		// Validate entry fields
		if entry.Name == "" {
			return nil, fmt.Errorf("tree entry has empty name")
		}
		if len(entry.Hash) != 64 { // SHA-256 hash length
			return nil, fmt.Errorf("invalid hash length for entry '%s': expected 64, got %d", entry.Name, len(entry.Hash))
		}

		// Mode as 6-digit octal string
		modeStr := fmt.Sprintf("%06o", entry.Mode)
		hashBytes, err := hex.DecodeString(entry.Hash)
		if err != nil {
			return nil, fmt.Errorf("invalid hash '%s' for entry '%s': %w", entry.Hash, entry.Name, err)
		}

		// Write "<mode> <name>\x00<hash>"
		fmt.Fprintf(&buf, "%s %s\x00", modeStr, entry.Name)
		if _, err := buf.Write(hashBytes); err != nil {
			return nil, fmt.Errorf("failed to write hash for entry '%s': %w", entry.Name, err)
		}
	}

	return buf.Bytes(), nil
}

// DeserializeTreeObject parses a Git-formatted byte slice into a TreeObject.
func DeserializeTreeObject(data []byte) (*TreeObject, error) {
	if data == nil {
		return nil, fmt.Errorf("cannot deserialize nil data")
	}

	tree := NewTreeObject()
	pos := 0

	// Parse entries until the end of the data
	for pos < len(data) {
		// Find space between mode and name
		spaceIdx := bytes.IndexByte(data[pos:], ' ')
		if spaceIdx == -1 {
			return nil, fmt.Errorf("invalid tree entry: missing space at position %d", pos)
		}
		modeStr := string(data[pos : pos+spaceIdx])
		pos += spaceIdx + 1

		// Find null byte separating name and hash
		nullIdx := bytes.IndexByte(data[pos:], '\x00')
		if nullIdx == -1 {
			return nil, fmt.Errorf("invalid tree entry: missing null byte at position %d", pos)
		}
		name := string(data[pos : pos+nullIdx])
		if name == "" {
			return nil, fmt.Errorf("invalid tree entry: empty name at position %d", pos)
		}
		pos += nullIdx + 1

		// Extract 32-byte SHA-256 hash
		if pos+32 > len(data) {
			return nil, fmt.Errorf("invalid tree entry: incomplete hash at position %d", pos)
		}
		hashBytes := data[pos : pos+32]
		pos += 32

		// Parse mode and infer type
		mode, err := strconv.ParseInt(modeStr, 8, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid mode '%s' for entry '%s': %w", modeStr, name, err)
		}
		entryType := "blob"
		if mode == 040000 {
			entryType = "tree"
		}

		tree.Entries = append(tree.Entries, TreeEntry{
			Mode: int32(mode),
			Name: name,
			Hash: hex.EncodeToString(hashBytes),
			Type: entryType,
		})
	}

	return tree, nil
}

// SetTreeID calculates the SHA-256 hash of the TreeObject and sets its TreeID.
func (t *TreeObject) SetTreeID() (string, error) {
	if t == nil {
		return "", fmt.Errorf("cannot set TreeID on nil TreeObject")
	}

	data, err := t.Serialize()
	if err != nil {
		return "", fmt.Errorf("failed to serialize tree: %w", err)
	}

	// Git-style header: "tree <size>\x00"
	header := fmt.Sprintf("tree %d\x00", len(data))
	fullContent := append([]byte(header), data...)

	hash := utils.HashBytes("tree", fullContent)
	t.TreeID = hash
	return hash, nil
}

// // CreateTreeFromEntries constructs a tree object from a list of entries, handling subtrees.
// // Returns the hash of the root tree.
// func CreateTreeFromEntries(repoRoot string, entries []TreeEntry) (string, error) {
// 	if repoRoot == "" {
// 		return "", fmt.Errorf("repository root path cannot be empty")
// 	}
// 	if len(entries) == 0 {
// 		return "", fmt.Errorf("no entries provided to create tree")
// 	}

// 	// Build a map of directory path -> entries.
// 	// Also, ensure that every intermediate directory exists as a key.
// 	treeMap := make(map[string][]TreeEntry)
// 	for _, entry := range entries {
// 		if entry.FullPath == "" {
// 			return "", fmt.Errorf("entry with hash '%s' has empty FullPath", entry.Hash)
// 		}
// 		// Split the full path and add every intermediate directory
// 		parts := strings.Split(entry.FullPath, string(filepath.Separator))
// 		// For "a/b/c.txt", add keys for "a" and "a/b"
// 		for i := 1; i < len(parts); i++ {
// 			dir := filepath.Join(parts[:i]...)
// 			if _, exists := treeMap[dir]; !exists {
// 				treeMap[dir] = []TreeEntry{}
// 			}
// 		}
// 		// Add the file into its parent directory key.
// 		dirPath := filepath.Dir(entry.FullPath)
// 		if dirPath == "." {
// 			dirPath = ""
// 		}
// 		treeMap[dirPath] = append(treeMap[dirPath], entry)
// 	}

// 	// Build the tree hierarchy for the root directory ("").
// 	rootEntries, err := buildTree(repoRoot, "", treeMap)
// 	if err != nil {
// 		return "", fmt.Errorf("failed to build root tree: %w", err)
// 	}
// 	return createTreeObject(repoRoot, rootEntries)
// }

// // buildTree recursively constructs tree entries for a given directory path.
// func buildTree(repoRoot, dirPath string, treeMap map[string][]TreeEntry) ([]TreeEntry, error) {
// 	var treeEntries []TreeEntry

// 	// Add files directly in this directory.
// 	if files, exists := treeMap[dirPath]; exists {
// 		for _, file := range files {
// 			// Only add files that are immediate children.
// 			fileDir := filepath.Dir(file.FullPath)
// 			if fileDir == dirPath || (dirPath == "" && fileDir == ".") {
// 				// Files have mode 100644 (or other valid non-dir modes).
// 				treeEntries = append(treeEntries, TreeEntry{
// 					Mode: file.Mode,
// 					Name: filepath.Base(file.FullPath),
// 					Hash: file.Hash,
// 					Type: "blob",
// 				})
// 			}
// 		}
// 	}

// 	// Identify immediate subdirectories.
// 	subDirs := make(map[string]struct{})
// 	for path := range treeMap {
// 		// Skip the current directory key.
// 		if path == dirPath {
// 			continue
// 		}
// 		// For root, consider all keys that are not empty.
// 		if dirPath == "" {
// 			parts := strings.Split(path, string(filepath.Separator))
// 			if parts[0] != "" {
// 				subDirs[parts[0]] = struct{}{}
// 			}
// 		} else if strings.HasPrefix(path, dirPath+string(filepath.Separator)) {
// 			relative := strings.TrimPrefix(path, dirPath+string(filepath.Separator))
// 			parts := strings.SplitN(relative, string(filepath.Separator), 2)
// 			if len(parts) > 0 && parts[0] != "" {
// 				subDirs[parts[0]] = struct{}{}
// 			}
// 		}
// 	}

// 	// Recursively build subtrees.
// 	for subDir := range subDirs {
// 		fullSubDir := subDir
// 		if dirPath != "" {
// 			fullSubDir = filepath.Join(dirPath, subDir)
// 		}
// 		subEntries, err := buildTree(repoRoot, fullSubDir, treeMap)
// 		if err != nil {
// 			return nil, err
// 		}
// 		subTreeHash, err := createTreeObject(repoRoot, subEntries)
// 		if err != nil {
// 			return nil, fmt.Errorf("failed to create subtree for '%s': %w", fullSubDir, err)
// 		}
// 		treeEntries = append(treeEntries, TreeEntry{
// 			Mode: 040000,
// 			Name: subDir,
// 			Hash: subTreeHash,
// 			Type: "tree",
// 		})
// 	}

// 	// Sort entries by name for consistency.
// 	sort.Slice(treeEntries, func(i, j int) bool {
// 		return treeEntries[i].Name < treeEntries[j].Name
// 	})

// 	return treeEntries, nil
// }

// createTreeObject serializes entries into a tree object, stores it on disk, and returns its hash.
func CreateTreeObject(repoRoot string, entries []TreeEntry) (string, error) {
	if repoRoot == "" {
		return "", fmt.Errorf("repository root path cannot be empty")
	}

	var content bytes.Buffer
	for _, entry := range entries {
		if entry.Name == "" {
			return "", fmt.Errorf("entry with hash '%s' has empty name", entry.Hash)
		}
		if len(entry.Hash) != 64 {
			return "", fmt.Errorf("invalid hash length for entry '%s': expected 64, got %d", entry.Name, len(entry.Hash))
		}
		modeStr := fmt.Sprintf("%06o", entry.Mode)
		hashBytes, err := hex.DecodeString(entry.Hash)
		if err != nil {
			return "", fmt.Errorf("invalid hash '%s' for entry '%s': %w", entry.Hash, entry.Name, err)
		}
		fmt.Fprintf(&content, "%s %s\x00", modeStr, entry.Name)
		content.Write(hashBytes)
	}

	treeContent := content.Bytes()
	header := fmt.Sprintf("tree %d\x00", len(treeContent))
	fullContent := append([]byte(header), treeContent...)

	hash := utils.HashBytes("tree", fullContent)
	objectPath := GetObjectPath(repoRoot, hash)

	// Ensure the directory exists.
	if err := utils.EnsureDirExists(filepath.Dir(objectPath)); err != nil {
		return "", fmt.Errorf("failed to create directory for object '%s': %w", hash, err)
	}

	// Write the object to disk.
	if err := os.WriteFile(objectPath, fullContent, 0644); err != nil {
		return "", fmt.Errorf("failed to write tree object '%s': %w", hash, err)
	}

	return hash, nil
}

// GetTree retrieves and deserializes a TreeObject from disk given its hash.
func GetTree(repoRoot string, hash string) (*TreeObject, error) {
	if repoRoot == "" {
		return nil, fmt.Errorf("repository root path cannot be empty")
	}
	if len(hash) != 64 {
		return nil, fmt.Errorf("invalid hash length: expected 64, got %d", len(hash))
	}

	objectPath := GetObjectPath(repoRoot, hash)
	content, err := os.ReadFile(objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read tree file '%s': %w", objectPath, err)
	}

	// Extract content after the header.
	headerEnd := bytes.IndexByte(content, '\x00')
	if headerEnd == -1 {
		return nil, fmt.Errorf("invalid tree format for hash '%s': missing header delimiter", hash)
	}
	treeContent := content[headerEnd+1:]

	// Verify header format: "tree <size>\x00"
	header := string(content[:headerEnd])
	if !strings.HasPrefix(header, "tree ") {
		return nil, fmt.Errorf("invalid tree header for hash '%s': expected 'tree <size>', got '%s'", hash, header)
	}
	sizeStr := strings.TrimPrefix(header, "tree ")
	size, err := strconv.Atoi(sizeStr)
	if err != nil || size < 0 || size != len(treeContent) {
		return nil, fmt.Errorf("invalid tree size for hash '%s': expected %d, got %s", hash, len(treeContent), sizeStr)
	}

	// Deserialize the tree content.
	tree, err := DeserializeTreeObject(treeContent)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize tree '%s': %w", hash, err)
	}
	tree.TreeID = hash
	return tree, nil
}

// // GetObjectPath constructs the filesystem path for an object given its hash.
// // Format: <repoRoot>/.vec/objects/<hash[:2]>/<hash[2:]>.
// func GetObjectPath(repoRoot, hash string) string {
// 	if len(hash) < 2 {
// 		return filepath.Join(repoRoot, ".vec", "objects", "invalid_hash")
// 	}
// 	return filepath.Join(repoRoot, ".vec", "objects", hash[:2], hash[2:])
// }

// buildTreeRecursively constructs tree entries for a given directory key in the map.
// It adds files and then finds immediate subdirectories, recursively building subtrees.
func BuildTreeRecursively(dirPath string, treeMap map[string][]TreeEntry, repoRoot string) ([]TreeEntry, error) {
	var entries []TreeEntry

	// Add files directly in this directory.
	if files, exists := treeMap[dirPath]; exists {
		entries = append(entries, files...)
	}

	// Find immediate subdirectories.
	subDirs := make(map[string]struct{})
	for key := range treeMap {
		// Skip the current directory key.
		if key == dirPath {
			continue
		}

		var rel string
		if dirPath == "" {
			// For root, the immediate subdirectory is the first component.
			parts := strings.Split(key, string(filepath.Separator))
			if len(parts) > 0 && parts[0] != "" {
				rel = parts[0]
			}
		} else {
			// For non-root directories, check keys with the prefix "dirPath/".
			prefix := dirPath + string(filepath.Separator)
			if strings.HasPrefix(key, prefix) {
				remain := strings.TrimPrefix(key, prefix)
				parts := strings.SplitN(remain, string(filepath.Separator), 2)
				if len(parts) > 0 && parts[0] != "" {
					rel = parts[0]
				}
			}
		}
		if rel != "" {
			subDirs[rel] = struct{}{}
		}
	}

	// For each immediate subdirectory, recursively build its subtree.
	for subDir := range subDirs {
		var fullSubDir string
		if dirPath == "" {
			fullSubDir = subDir
		} else {
			fullSubDir = filepath.Join(dirPath, subDir)
		}
		subEntries, err := BuildTreeRecursively(fullSubDir, treeMap, repoRoot)
		if err != nil {
			return nil, err
		}
		subTreeHash, err := CreateTreeObject(repoRoot, subEntries)
		if err != nil {
			return nil, fmt.Errorf("failed to create subtree for '%s': %w", fullSubDir, err)
		}
		// Append the subtree as a tree entry.
		entries = append(entries, TreeEntry{
			Mode: 040000,
			Name: subDir,
			Hash: subTreeHash,
			Type: "tree",
		})
	}

	// Sort entries by name for consistency.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}
