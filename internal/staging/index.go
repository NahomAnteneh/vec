package staging

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"slices"

	"maps"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/utils"
)

// Index represents the staging area (index) in the repository.
type Index struct {
	Entries  []IndexEntry // List of entries in the index
	Path     string       // Path to the index file (e.g., .vec/index)
	entryMap map[string]*IndexEntry
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

// NewIndex creates a new, empty Index using Repository context.
func NewIndex(repo *core.Repository) *Index {
	return &Index{
		Entries: []IndexEntry{},
		Path:    filepath.Join(repo.VecDir, "index"),
	}
}

// LoadIndex reads the index from disk or returns a new one if it doesn't exist using Repository context.
func LoadIndex(repo *core.Repository) (*Index, error) {
	indexPath := filepath.Join(repo.VecDir, "index")
	if !utils.FileExists(indexPath) {
		return NewIndex(repo), nil
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read index file: %w", err)
	}

	return DeserializeIndex(repo, data)
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

// Add adds or updates a stage 0 entry in the index for a file using Repository context.
func (i *Index) Add(repo *core.Repository, relPath, hash string) error {
	absPath := filepath.Join(repo.Root, relPath)
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Check for existing entry
	for j, entry := range i.Entries {
		if entry.FilePath == relPath && entry.Stage == 0 {
			// Update existing stage 0 entry
			i.Entries[j].Mode = int32(100644)
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
		Mode:     int32(100644),
		FilePath: relPath,
		SHA256:   hash,
		Size:     fileInfo.Size(),
		Mtime:    fileInfo.ModTime(),
		Stage:    0,
	}
	i.Entries = append(i.Entries, newEntry)
	return nil
}

// Remove removes a stage 0 entry from the index using Repository context.
func (i *Index) Remove(repo *core.Repository, relPath string) error {
	for j, entry := range i.Entries {
		if entry.FilePath == relPath && entry.Stage == 0 {
			i.Entries = slices.Delete(i.Entries, j, j+1)
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

// buildFileMapFromTree returns a map from relative file paths to blob hashes by traversing the tree recursively using Repository context.
func buildFileMapFromTree(repo *core.Repository, tree *objects.TreeObject, basePath string) (map[string]string, error) {
	fileMap := make(map[string]string)
	for _, entry := range tree.Entries {
		currentPath := filepath.Join(basePath, entry.Name)
		if entry.Type == "blob" {
			fileMap[currentPath] = entry.Hash
		} else if entry.Type == "tree" {
			subTree, err := objects.GetTree(repo.Root, entry.Hash)
			if err != nil {
				return nil, fmt.Errorf("failed to get subtree '%s': %w", entry.Hash, err)
			}
			subMap, err := buildFileMapFromTree(repo, subTree, currentPath)
			if err != nil {
				return nil, err
			}
			// Merge subMap into fileMap
			maps.Copy(fileMap, subMap)
		}
	}
	return fileMap, nil
}



// HasUncommittedChanges checks for uncommitted changes for tracked files using Repository context
// by comparing the index (stage 0) with the HEAD tree (built recursively) and the working directory.
func (i *Index) HasUncommittedChanges(repo *core.Repository) bool {
	// Retrieve the HEAD commit and its tree
	headCommitID, err := utils.ReadHEAD(repo.Root)
	if err != nil {
		return true // Assume changes if HEAD can't be read
	}
	var headTree *objects.TreeObject
	if headCommitID != "" {
		headCommit, err := objects.GetCommit(repo.Root, headCommitID)
		if err != nil {
			return true // Assume changes if commit can't be loaded
		}
		headTree, err = objects.GetTree(repo.Root, headCommit.Tree)
		if err != nil {
			return true // Assume changes if tree can't be loaded
		}
	}

	// Build maps for comparison using the recursive helper
	headTreeMap := make(map[string]string) // filepath -> hash
	if headTree != nil {
		m, err := buildFileMapFromTree(repo, headTree, "")
		if err != nil {
			return true // Assume changes if unable to build file map
		}
		headTreeMap = m
	}

	indexMap := make(map[string]string) // filepath -> hash
	for _, entry := range i.Entries {
		if entry.Stage == 0 {
			indexMap[entry.FilePath] = entry.SHA256
		}
	}

	// Check for staged changes (index vs. HEAD)
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

	// Check for unstaged changes (working directory vs. index)
	for _, entry := range i.Entries {
		if entry.Stage != 0 {
			continue // Skip conflict entries
		}
		absPath := filepath.Join(repo.Root, entry.FilePath)
		fileInfo, err := os.Stat(absPath)
		if os.IsNotExist(err) {
			return true // File in index but missing in working directory
		}
		if err != nil {
			return true // Assume changes on stat error
		}
		// Check if file has been modified since last indexed
		if fileInfo.ModTime().After(entry.Mtime) {
			content, err := os.ReadFile(absPath)
			if err != nil {
				return true // Assume changes if file can't be read
			}
			currentHash := utils.HashBytes("blob", content)
			if currentHash != entry.SHA256 {
				return true // Content differs from index
			}
		}
	}

	// No uncommitted changes found
	return false
}

// IsClean returns true if there are no uncommitted changes in the working directory or index using Repository context.
func (i *Index) IsClean(repo *core.Repository) bool {
	return !i.HasUncommittedChanges(repo)
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

// DeserializeIndex deserializes a byte slice into an Index using Repository context.
func DeserializeIndex(repo *core.Repository, data []byte) (*Index, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("invalid index data: too short")
	}

	buf := bytes.NewReader(data)
	index := NewIndex(repo)

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

// CreateTreeFromIndex builds a Git-style tree object directly from the index using Repository context.
// It walks over stage-0 index entries, groups files into the proper directory structure
// (ensuring every intermediate directory is present), and returns the hash of the root tree.
func CreateTreeFromIndex(repo *core.Repository, index *Index) (string, error) {
	if repo.Root == "" {
		return "", fmt.Errorf("repository root cannot be empty")
	}

	if index == nil {
		return "", fmt.Errorf("index cannot be nil")
	}

	// Build a map: directory path -> list of TreeEntry
	// Keys are full relative paths for directories.
	treeMap, err := buildTreeMapFromIndex(index)
	if err != nil {
		return "", fmt.Errorf("failed to build tree map from index: %w", err)
	}

	// If the index is empty, create an empty tree
	if len(treeMap) == 0 {
		emptyTreeEntries := []objects.TreeEntry{}
		return objects.CreateTreeObject(emptyTreeEntries)
	}

	// Build the hierarchical tree starting at the root ("").
	rootEntries, err := objects.BuildTreeRecursively("", treeMap, repo.Root)
	if err != nil {
		return "", fmt.Errorf("failed to build trees recursively: %w", err)
	}

	// Create and write the root tree object
	rootHash, err := objects.CreateTreeObject(rootEntries)
	if err != nil {
		return "", fmt.Errorf("failed to create root tree object: %w", err)
	}

	return rootHash, nil
}



// buildTreeMapFromIndex constructs a mapping of directory paths to TreeEntry objects
// from the index entries. This is a helper function for CreateTreeFromIndex.
// This optimized version pre-allocates memory for the map based on index size
// and uses a more efficient algorithm for path normalization.
func buildTreeMapFromIndex(index *Index) (map[string][]objects.TreeEntry, error) {
	// Pre-allocate the map based on the size of the index
	// with a reasonable size estimate to avoid frequent resizing
	estimatedSize := len(index.Entries) / 2
	if estimatedSize == 0 {
		estimatedSize = 10 // Reasonable minimum to avoid empty map allocations
	}
	treeMap := make(map[string][]objects.TreeEntry, estimatedSize)

	// Track seen directories to avoid redundant operations
	seenDirs := make(map[string]struct{}, estimatedSize)

	// Process each stage-0 index entry.
	for _, ie := range index.Entries {
		if ie.Stage != 0 {
			continue
		}

		// Validate the entry's FilePath
		if ie.FilePath == "" {
			return nil, fmt.Errorf("index entry for file with hash '%s' has empty FilePath", ie.SHA256)
		}

		// Normalize the file path to use standard separators - more efficiently
		normalizedPath := strings.ReplaceAll(ie.FilePath, string(filepath.Separator), "/")

		// Get the parent directory and file name
		parentDir, fileName := splitPath(normalizedPath)

		// Add the file entry to its parent directory
		// Use slice pre-allocation when possible

		fileEntry := objects.TreeEntry{
			Mode: ie.Mode,
			Name: fileName,
			Hash: ie.SHA256,
			Type: "blob",
		}

		treeMap[parentDir] = append(treeMap[parentDir], fileEntry)

		// Ensure all parent directories exist in the tree map
		// but only if we haven't processed this directory before
		if _, exists := seenDirs[parentDir]; !exists {
			ensureParentDirectories(treeMap, parentDir)
			seenDirs[parentDir] = struct{}{}
		}
	}

	// Sort entries in each directory for consistency (Git sorts entries by name)
	// Use an optimized version of the sorting function
	sortTreeMapEntries(treeMap)

	return treeMap, nil
}

// splitPath splits a file path into its parent directory and file name.
// For root-level files, the parent directory is an empty string.
// Optimized version that avoids creating substrings when possible.
func splitPath(path string) (string, string) {
	if path == "" {
		return "", ""
	}

	lastSlashIndex := strings.LastIndex(path, "/")
	if lastSlashIndex == -1 {
		// File is at the root level
		return "", path
	}

	parentDir := path[:lastSlashIndex]
	fileName := path[lastSlashIndex+1:]

	return parentDir, fileName
}

// ensureParentDirectories makes sure all parent directories of a path exist in the tree map.
// This optimized version reduces path splitting operations and uses a more efficient algorithm.
func ensureParentDirectories(treeMap map[string][]objects.TreeEntry, path string) {
	if path == "" {
		return // We've reached the root
	}

	// More efficient path processing to avoid repeated splits
	parts := strings.Split(path, "/")

	// Build up paths and ensure directories exist
	var currentPath string
	for i := 0; i < len(parts); i++ {
		if i > 0 {
			currentPath += "/"
		}
		currentPath += parts[i]

		// Only create an entry if it doesn't already exist
		if _, exists := treeMap[currentPath]; !exists {
			treeMap[currentPath] = make([]objects.TreeEntry, 0, 4) // Pre-allocate with small capacity
		}
	}
}

// sortTreeMapEntries sorts all entries in the tree map by name for consistent output.
// This optimized version avoids unnecessary re-sorting of already sorted entries.
func sortTreeMapEntries(treeMap map[string][]objects.TreeEntry) {
	for dir, entries := range treeMap {
		if len(entries) > 1 {
			// Check if entries are already sorted to avoid unnecessary sort operations
			needsSort := false
			for i := 1; i < len(entries); i++ {
				if entries[i-1].Name > entries[i].Name {
					needsSort = true
					break
				}
			}

			if needsSort {
				sort.Slice(entries, func(i, j int) bool {
					return entries[i].Name < entries[j].Name
				})
				treeMap[dir] = entries
			}
		}
	}
}

// GetEntry returns the index entry for a given file path and stage.
// If the entry doesn't exist, it returns nil and false.
// This optimized version uses a map lookup when possible.
func (i *Index) GetEntry(filePath string, stage int) (*IndexEntry, bool) {
	// First try to find the entry using the cached map if available
	if i.entryMap != nil {
		key := fmt.Sprintf("%s:%d", filePath, stage)
		if entry, ok := i.entryMap[key]; ok {
			return entry, true
		}
		return nil, false
	}

	// Fall back to linear search if no map is available
	for j := range i.Entries {
		if i.Entries[j].FilePath == filePath && i.Entries[j].Stage == stage {
			return &i.Entries[j], true
		}
	}
	return nil, false
}

// HasConflicts returns true if the index contains entries with stages 1, 2, or 3,
// indicating unresolved merge conflicts.
// This optimized version uses early returns for better performance.
func (i *Index) HasConflicts() bool {
	for _, entry := range i.Entries {
		if entry.Stage > 0 {
			return true
		}
	}
	return false
}

// GetConflicts returns all files that have conflicts (entries with stages 1, 2, or 3)
// This is a new helper function to efficiently find conflicts
func (i *Index) GetConflicts() map[string]bool {
	conflicts := make(map[string]bool)
	for _, entry := range i.Entries {
		if entry.Stage > 0 {
			conflicts[entry.FilePath] = true
		}
	}
	return conflicts
}

// AddEntry adds or updates an entry in the index
// This is a new function for more advanced index manipulation
func (i *Index) AddEntry(entry IndexEntry) {
	// Check if the entry already exists (same path and stage)
	for j := range i.Entries {
		if i.Entries[j].FilePath == entry.FilePath && i.Entries[j].Stage == entry.Stage {
			// Update the existing entry
			i.Entries[j] = entry

			// Clear the entry map so it will be rebuilt on next access
			i.entryMap = nil
			return
		}
	}

	// Add as a new entry
	i.Entries = append(i.Entries, entry)

	// Update the entry map if it exists
	if i.entryMap != nil {
		key := fmt.Sprintf("%s:%d", entry.FilePath, entry.Stage)
		i.entryMap[key] = &i.Entries[len(i.Entries)-1]
	}
}

// RemoveEntry removes an entry from the index by path and stage
// This is a new function for more advanced index manipulation
func (i *Index) RemoveEntry(filePath string, stage int) bool {
	for j := range i.Entries {
		if i.Entries[j].FilePath == filePath && i.Entries[j].Stage == stage {
			// Remove the entry
			i.Entries = append(i.Entries[:j], i.Entries[j+1:]...)

			// Clear the entry map so it will be rebuilt on next access
			i.entryMap = nil
			return true
		}
	}
	return false
}

// FindPaths returns all file paths in the index that match a given pattern
// This is a new function for advanced path searching
func (i *Index) FindPaths(pattern string) []string {
	var matches []string
	seen := make(map[string]bool)

	for _, entry := range i.Entries {
		if seen[entry.FilePath] {
			continue
		}

		matched, err := filepath.Match(pattern, entry.FilePath)
		if err == nil && matched {
			matches = append(matches, entry.FilePath)
			seen[entry.FilePath] = true
		}
	}

	return matches
}
