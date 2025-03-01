// go code in internal/objects/tree.go
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
	Mode int32 // File mode (e.g., 0644 for regular file, 040000 for directory)
	// Name now holds the full relative path.
	Name string
	Hash string // SHA-256 hash of the blob or tree
	Type string // "blob" or "tree"
}

type TreeObject struct {
	TreeID  string      // SHA-256 hash of the *serialized* tree data. Calculated, not stored.
	Entries []TreeEntry // List of TreeEntry structs.
}

// NewTreeObject creates a new, empty TreeObject.
func NewTreeObject() *TreeObject {
	return &TreeObject{
		Entries: []TreeEntry{},
	}
}

// Serialize serializes the TreeObject into a byte slice.
func (t *TreeObject) Serialize() ([]byte, error) {
	var buf bytes.Buffer
	sort.Slice(t.Entries, func(i, j int) bool {
		if t.Entries[i].Type != t.Entries[j].Type {
			return t.Entries[i].Type < t.Entries[j].Type
		}
		// Compare by Name (which in the serialized tree should be just the basename)
		return t.Entries[i].Name < t.Entries[j].Name
	})

	entriesCount := uint32(len(t.Entries))
	if err := binary.Write(&buf, binary.LittleEndian, entriesCount); err != nil {
		return nil, fmt.Errorf("failed to write entries count: %w", err)
	}

	for _, entry := range t.Entries {
		// Write name.
		nameBytes := []byte(entry.Name)
		nameLength := uint32(len(nameBytes))
		if err := binary.Write(&buf, binary.LittleEndian, nameLength); err != nil {
			return nil, fmt.Errorf("failed to write name length: %w", err)
		}
		if _, err := buf.Write(nameBytes); err != nil {
			return nil, fmt.Errorf("failed to write name: %w", err)
		}
		// Write type.
		typeBytes := []byte(entry.Type)
		typeLength := uint32(len(typeBytes))
		if err := binary.Write(&buf, binary.LittleEndian, typeLength); err != nil {
			return nil, fmt.Errorf("failed to write type length: %w", err)
		}
		if _, err := buf.Write(typeBytes); err != nil {
			return nil, fmt.Errorf("failed to write type: %w", err)
		}
		// Write hash.
		hashBytes := []byte(entry.Hash)
		hashLength := uint32(len(hashBytes))
		if err := binary.Write(&buf, binary.LittleEndian, hashLength); err != nil {
			return nil, fmt.Errorf("failed to write hash length: %w", err)
		}
		if _, err := buf.Write(hashBytes); err != nil {
			return nil, fmt.Errorf("failed to write hash: %w", err)
		}
		// Write mode.
		if err := binary.Write(&buf, binary.LittleEndian, entry.Mode); err != nil {
			return nil, fmt.Errorf("failed to write mode: %w", err)
		}
	}

	return buf.Bytes(), nil
}

// DeserializeTreeObject deserializes a byte slice into a TreeObject.
func DeserializeTreeObject(data []byte) (*TreeObject, error) {
	buf := bytes.NewReader(data)
	tree := NewTreeObject()

	var entriesCount uint32
	if err := binary.Read(buf, binary.LittleEndian, &entriesCount); err != nil {
		return nil, fmt.Errorf("failed to read entries count: %w", err)
	}

	for i := uint32(0); i < entriesCount; i++ {
		var entry TreeEntry

		var nameLength uint32
		if err := binary.Read(buf, binary.LittleEndian, &nameLength); err != nil {
			return nil, fmt.Errorf("failed to read name length: %w", err)
		}
		nameBytes := make([]byte, nameLength)
		if _, err := buf.Read(nameBytes); err != nil {
			return nil, fmt.Errorf("failed to read name: %w", err)
		}
		entry.Name = string(nameBytes)

		var typeLength uint32
		if err := binary.Read(buf, binary.LittleEndian, &typeLength); err != nil {
			return nil, fmt.Errorf("failed to read type length: %w", err)
		}
		typeBytes := make([]byte, typeLength)
		if _, err := buf.Read(typeBytes); err != nil {
			return nil, fmt.Errorf("failed to read type: %w", err)
		}
		entry.Type = string(typeBytes)

		var hashLength uint32
		if err := binary.Read(buf, binary.LittleEndian, &hashLength); err != nil {
			return nil, fmt.Errorf("failed to read hash length: %w", err)
		}
		hashBytes := make([]byte, hashLength)
		if _, err := buf.Read(hashBytes); err != nil {
			return nil, fmt.Errorf("failed to read hash: %w", err)
		}
		entry.Hash = string(hashBytes)

		if err := binary.Read(buf, binary.LittleEndian, &entry.Mode); err != nil {
			return nil, fmt.Errorf("failed to read mode: %w", err)
		}
		tree.Entries = append(tree.Entries, entry)
	}

	return tree, nil
}

// SetTreeID calculates and sets the TreeID (SHA-256 hash).
func (t *TreeObject) SetTreeID() (string, error) {
	data, err := t.Serialize()
	if err != nil {
		return "", fmt.Errorf("failed to serialize tree: %w", err)
	}
	header := fmt.Sprintf("tree %d\n", len(data))
	var buf bytes.Buffer
	buf.WriteString(header)
	buf.Write(data)
	hash := utils.HashBytes("tree", buf.Bytes())
	t.TreeID = hash
	return hash, nil
}

// CreateTree creates a tree object from the index.
func CreateTree(repoRoot string, index *core.Index) (string, error) {
	entries, err := buildTreeEntries(repoRoot, index)
	if err != nil {
		return "", err
	}
	tree := TreeObject{Entries: entries}
	// Use empty string as basePath for the root tree.
	treeHash, err := tree.createTreeHelper(repoRoot, "")
	if err != nil {
		return "", err
	}
	return treeHash, nil
}

// buildTreeEntries converts index entries to tree entries.
// It now stores the full relative path in the Name field.
func buildTreeEntries(repoRoot string, index *core.Index) ([]TreeEntry, error) {
	entriesMap := make(map[string]TreeEntry)
	for _, idxEntry := range index.Entries {
		relPath, err := filepath.Rel(repoRoot, idxEntry.Filename)
		if err != nil {
			return nil, fmt.Errorf("failed to get relative path: %w", err)
		}
		relPath = filepath.ToSlash(filepath.Clean(relPath))
		entriesMap[relPath] = TreeEntry{
			Mode: idxEntry.Mode,
			Name: relPath,
			Hash: idxEntry.SHA256,
			Type: "blob",
		}
		// Also add directory entries for each parent directory.
		parts := strings.Split(relPath, "/")
		for i := 1; i < len(parts); i++ {
			dirPath := strings.Join(parts[:i], "/")
			if _, ok := entriesMap[dirPath]; !ok {
				entriesMap[dirPath] = TreeEntry{
					Mode: 040000,
					Name: dirPath,
					Type: "tree",
					Hash: "",
				}
			}
		}
	}
	// Return a sorted list of entries.
	keys := make([]string, 0, len(entriesMap))
	for k := range entriesMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sortedEntries := make([]TreeEntry, len(keys))
	for i, k := range keys {
		sortedEntries[i] = entriesMap[k]
	}
	return sortedEntries, nil
}

// createTreeHelper builds the tree recursively for the given basePath.
// For basePath == "" (root), it gathers entries whose full path has no "/".
// For a subdirectory, it gathers entries that live directly under that directory.
// When recursing into a subdirectory, we reset the base to "" for the child tree.
func (t *TreeObject) createTreeHelper(repoRoot, basePath string) (string, error) {
	var immediate []TreeEntry
	// Gather immediate children based on the current base.
	if basePath == "" {
		// Immediate entries are those whose Name does not contain "/"
		for _, e := range t.Entries {
			if !strings.Contains(e.Name, "/") {
				immediate = append(immediate, e)
			}
		}
	} else {
		// Immediate children of basePath: their full name starts with basePath+"/"
		// and after stripping that prefix they contain no "/".
		prefix := basePath + "/"
		for _, e := range t.Entries {
			if strings.HasPrefix(e.Name, prefix) {
				remainder := strings.TrimPrefix(e.Name, prefix)
				if !strings.Contains(remainder, "/") {
					// Store with stripped name.
					copyE := e
					copyE.Name = remainder
					immediate = append(immediate, copyE)
				}
			}
		}
	}

	// Build a new tree for the current level.
	currentTree := NewTreeObject()
	for _, e := range immediate {
		if e.Type == "tree" {
			// Compute the full directory name from the current context.
			var dirName string
			if basePath == "" {
				dirName = e.Name
			} else {
				dirName = basePath + "/" + e.Name
			}
			// Gather all entries from the parent's t.Entries that lie under this directory.
			var children []TreeEntry
			prefix := dirName + "/"
			for _, f := range t.Entries {
				if strings.HasPrefix(f.Name, prefix) {
					child := f
					// Strip the directory prefix so that children become relative to the sub-tree.
					child.Name = strings.TrimPrefix(f.Name, prefix)
					children = append(children, child)
				}
			}
			// Build the sub-tree using the gathered children.
			subTree := &TreeObject{Entries: children}
			subHash, err := subTree.createTreeHelper(repoRoot, "")
			if err != nil {
				return "", err
			}
			// Set the directory entry's hash to the sub-tree's hash.
			e.Hash = subHash
			currentTree.Entries = append(currentTree.Entries, e)
		} else {
			currentTree.Entries = append(currentTree.Entries, e)
		}
	}

	// Serialize the current tree and compute its hash.
	data, err := currentTree.Serialize()
	if err != nil {
		return "", err
	}
	header := fmt.Sprintf("tree %d\n", len(data))
	var buf bytes.Buffer
	buf.WriteString(header)
	buf.Write(data)
	content := buf.Bytes()
	hash := utils.HashBytes("tree", content)
	currentTree.TreeID = hash

	// Store the tree object.
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
//     return filepath.Join(repoRoot, ".vec", "objects", hash[:2], hash[2:])
// }
