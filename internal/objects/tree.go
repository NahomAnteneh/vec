package objects

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NahomAnteneh/vec/internal/core"
	"github.com/NahomAnteneh/vec/utils"
)

type TreeEntry struct {
	Mode int32  // File mode (e.g., 0644 for regular file, 040000 for directory)
	Name string // File or directory name (relative to the repository root)
	Hash string // SHA-256 hash of the blob or tree
	Type string // "blob" or "tree"
}

type TreeObject struct {
	TreeID  string      // SHA-256 hash of the *serialized* tree data.  Calculated, *not* stored.
	Entries []TreeEntry // List of TreeEntry structs.
}

// NewTreeObject creates a new, empty TreeObject.
func NewTreeObject() *TreeObject {
	return &TreeObject{
		Entries: []TreeEntry{},
	}
}

// Serialize serializes the TreeObject into a byte slice
func (t *TreeObject) Serialize() ([]byte, error) {
	var buf bytes.Buffer

	// Sort entries for consistent hashing
	sort.Slice(t.Entries, func(i, j int) bool {
		if t.Entries[i].Type != t.Entries[j].Type {
			return t.Entries[i].Type < t.Entries[j].Type
		}
		return t.Entries[i].Name < t.Entries[j].Name
	})

	// Write number of entries
	entriesCount := uint32(len(t.Entries))
	if err := binary.Write(&buf, binary.LittleEndian, entriesCount); err != nil {
		return nil, fmt.Errorf("failed to write entries count: %w", err)
	}

	// Write each entry
	for _, entry := range t.Entries {
		// Write name
		nameBytes := []byte(entry.Name)
		nameLength := uint32(len(nameBytes))
		if err := binary.Write(&buf, binary.LittleEndian, nameLength); err != nil {
			return nil, fmt.Errorf("failed to write name length: %w", err)
		}
		if _, err := buf.Write(nameBytes); err != nil {
			return nil, fmt.Errorf("failed to write name: %w", err)
		}

		// Write type
		typeBytes := []byte(entry.Type)
		typeLength := uint32(len(typeBytes))
		if err := binary.Write(&buf, binary.LittleEndian, typeLength); err != nil {
			return nil, fmt.Errorf("failed to write type length: %w", err)
		}
		if _, err := buf.Write(typeBytes); err != nil {
			return nil, fmt.Errorf("failed to write type: %w", err)
		}

		// Write hash
		hashBytes := []byte(entry.Hash)
		hashLength := uint32(len(hashBytes))
		if err := binary.Write(&buf, binary.LittleEndian, hashLength); err != nil {
			return nil, fmt.Errorf("failed to write hash length: %w", err)
		}
		if _, err := buf.Write(hashBytes); err != nil {
			return nil, fmt.Errorf("failed to write hash: %w", err)
		}

		// Write mode
		if err := binary.Write(&buf, binary.LittleEndian, entry.Mode); err != nil {
			return nil, fmt.Errorf("failed to write mode: %w", err)
		}
	}

	return buf.Bytes(), nil
}

// DeserializeTreeObject deserializes a byte slice into a TreeObject
func DeserializeTreeObject(data []byte) (*TreeObject, error) {
	buf := bytes.NewReader(data)
	tree := NewTreeObject()

	// Read number of entries
	var entriesCount uint32
	if err := binary.Read(buf, binary.LittleEndian, &entriesCount); err != nil {
		return nil, fmt.Errorf("failed to read entries count: %w", err)
	}

	// Read each entry
	for i := uint32(0); i < entriesCount; i++ {
		var entry TreeEntry

		// Read name
		var nameLength uint32
		if err := binary.Read(buf, binary.LittleEndian, &nameLength); err != nil {
			return nil, fmt.Errorf("failed to read name length: %w", err)
		}
		nameBytes := make([]byte, nameLength)
		if _, err := buf.Read(nameBytes); err != nil {
			return nil, fmt.Errorf("failed to read name: %w", err)
		}
		entry.Name = string(nameBytes)

		// Read type
		var typeLength uint32
		if err := binary.Read(buf, binary.LittleEndian, &typeLength); err != nil {
			return nil, fmt.Errorf("failed to read type length: %w", err)
		}
		typeBytes := make([]byte, typeLength)
		if _, err := buf.Read(typeBytes); err != nil {
			return nil, fmt.Errorf("failed to read type: %w", err)
		}
		entry.Type = string(typeBytes)

		// Read hash
		var hashLength uint32
		if err := binary.Read(buf, binary.LittleEndian, &hashLength); err != nil {
			return nil, fmt.Errorf("failed to read hash length: %w", err)
		}
		hashBytes := make([]byte, hashLength)
		if _, err := buf.Read(hashBytes); err != nil {
			return nil, fmt.Errorf("failed to read hash: %w", err)
		}
		entry.Hash = string(hashBytes)

		// Read mode
		if err := binary.Read(buf, binary.LittleEndian, &entry.Mode); err != nil {
			return nil, fmt.Errorf("failed to read mode: %w", err)
		}
		tree.Entries = append(tree.Entries, entry)
	}

	return tree, nil
}

// SetTreeID calculates and sets the TreeID (SHA-256 hash)
func (t *TreeObject) SetTreeID() (string, error) {
	data, err := t.Serialize()
	if err != nil {
		return "", fmt.Errorf("failed to serialize tree: %w", err)
	}
	header := fmt.Sprintf("tree %d\n", len(data))
	var buf bytes.Buffer
	buf.WriteString(header)
	buf.Write(data)
	hash := utils.HashBytes("tree", buf.Bytes()) // Use already defined function
	t.TreeID = hash
	return hash, nil
}

// CreateTree creates a tree object from the index.
func CreateTree(repoRoot string, index *core.Index) (string, error) {
	entries, err := buildTreeEntries(index)
	if err != nil {
		return "", err
	}
	tree := TreeObject{Entries: entries} // Use the new TreeObject
	treeHash, err := tree.createTreeHelper(repoRoot, "")
	if err != nil {
		return "", err
	}

	return treeHash, nil
}
func buildTreeEntries(index *core.Index) ([]TreeEntry, error) {
	entries := make(map[string]TreeEntry)

	// Convert index entries to tree entries
	for _, indexEntry := range index.Entries {
		parts := strings.Split(indexEntry.Filename, string(os.PathSeparator))
		currentPath := ""
		for i, part := range parts[:len(parts)-1] {
			currentPath = filepath.Join(currentPath, part)
			if _, ok := entries[currentPath]; !ok {
				entries[currentPath] = TreeEntry{
					Mode: 040000, // Directory
					Name: currentPath,
					Type: "tree",
					Hash: "", // Hash will be create recursively
				}
			}

			if i == len(parts)-2 { // If the parent of the file
				entries[indexEntry.Filename] = TreeEntry{
					Mode: indexEntry.Mode,
					Name: indexEntry.Filename, //file name
					Hash: indexEntry.SHA256,
					Type: "blob",
				}
			}
		}
		if len(parts) == 1 {
			entries[indexEntry.Filename] = TreeEntry{
				Mode: indexEntry.Mode,
				Name: indexEntry.Filename, //file name
				Hash: indexEntry.SHA256,
				Type: "blob",
			}
		}
	}

	// Sort the entries by name for consistent tree hashing.
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}

	sort.Strings(keys)
	var sortedEntries []TreeEntry
	for _, key := range keys {
		sortedEntries = append(sortedEntries, entries[key])
	}
	return sortedEntries, nil
}

func (t *TreeObject) createTreeHelper(repoRoot, basePath string) (string, error) {
	// Filter entries relevant for this level
	var currentLevelEntries []TreeEntry
	for _, entry := range t.Entries {
		dir, _ := filepath.Split(entry.Name)
		dir = strings.TrimSuffix(dir, string(os.PathSeparator))
		if dir == basePath { // Filter entries for this level
			currentLevelEntries = append(currentLevelEntries, entry)
		}
	}

	// Create new tree object for the current level
	currentTree := NewTreeObject()
	for _, entry := range currentLevelEntries {
		if entry.Type == "tree" { // Recursively create subtrees.
			subTree := TreeObject{Entries: t.Entries} // Pass down *all* entries.
			subHash, err := subTree.createTreeHelper(repoRoot, entry.Name)
			if err != nil {
				return "", err
			}
			entry.Hash = subHash
			currentTree.Entries = append(currentTree.Entries, entry)
		} else {
			currentTree.Entries = append(currentTree.Entries, entry)
		}
	}

	// Serialize and hash
	data, err := currentTree.Serialize()
	if err != nil {
		return "", err
	}
	header := fmt.Sprintf("tree %d\n", len(data))
	var buf bytes.Buffer
	buf.WriteString(header)
	buf.Write(data)
	content := buf.Bytes()
	hash := utils.HashBytes("tree", content) // Consistent hashing.
	currentTree.TreeID = hash

	// Store
	objectPath := GetObjectPath(repoRoot, hash)
	objectDir := filepath.Dir(objectPath)

	if err := utils.EnsureDirExists(objectDir); err != nil {
		return "", err
	}

	if err := os.WriteFile(objectPath, content, 0644); err != nil {
		return "", fmt.Errorf("failed to write tree file: %w", err)
	}

	return hash, nil
}

// GetTree reads a tree object from disk.
func GetTree(repoRoot string, hash string) (*TreeObject, error) {
	objectPath := GetObjectPath(repoRoot, hash)

	content, err := os.ReadFile(objectPath)
	if err != nil {
		return nil, err
	}
	headerEnd := bytes.IndexByte(content, '\n')
	if headerEnd == -1 {
		return nil, fmt.Errorf("invalid tree format: missing header")
	}

	treeContent := content[headerEnd+1:]
	tree, err := DeserializeTreeObject(treeContent)
	if err != nil {
		return nil, err
	}

	tree.TreeID = hash
	return tree, nil
}

// GetObjectPath returns the path to a tree object.
// func GetObjectPath(repoRoot string, hash string) string {
// 	return filepath.Join(repoRoot, ".vec", "objects", hash[:2], hash[2:])
// }
