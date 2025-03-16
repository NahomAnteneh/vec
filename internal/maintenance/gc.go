package maintenance

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/utils"
)

// GarbageCollectOptions defines options for garbage collection
type GarbageCollectOptions struct {
	// Root path of the repository
	RepoRoot string
	// Whether to prune (remove) unreferenced objects rather than packing them
	Prune bool
	// Whether to automatically pack objects instead of just removing them
	AutoPack bool
	// Whether to run a dry run (don't actually delete anything)
	DryRun bool
	// Verbose output
	Verbose bool
}

// GCStats contains statistics from the garbage collection operation
type GCStats struct {
	// Number of objects examined
	ObjectsExamined int
	// Number of objects removed
	ObjectsRemoved int
	// Number of objects packed
	ObjectsPacked int
	// Number of packfiles pruned
	PackfilesPruned int
	// Space saved in bytes
	SpaceSaved int64
}

// DefaultGCOptions returns default garbage collection options
func DefaultGCOptions() GarbageCollectOptions {
	return GarbageCollectOptions{
		Prune:    false,
		AutoPack: true,
		DryRun:   false,
		Verbose:  false,
	}
}

// GarbageCollect performs garbage collection on the repository
func GarbageCollect(options GarbageCollectOptions) (*GCStats, error) {
	stats := &GCStats{}

	// Get repository root if not specified
	repoRoot := options.RepoRoot
	if repoRoot == "" {
		var err error
		repoRoot, err = utils.GetVecRoot()
		if err != nil {
			return nil, fmt.Errorf("not a valid repository: %w", err)
		}
	}

	// Find all reachable objects
	reachable, err := findReachableObjects(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to find reachable objects: %w", err)
	}

	// Find all objects to determine which are unreferenced
	allObjects, err := findAllObjects(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to find all objects: %w", err)
	}

	stats.ObjectsExamined = len(allObjects)

	// Identify unreferenced objects
	unreferenced := []ObjectInfo{}
	for _, obj := range allObjects {
		if !reachable[obj.Hash] {
			unreferenced = append(unreferenced, obj)
		}
	}

	// Calculate total size of unreferenced objects
	var totalSize int64
	for _, obj := range unreferenced {
		totalSize += obj.Size
	}

	if options.Verbose {
		fmt.Printf("Found %d reachable objects and %d unreferenced objects (%d bytes)\n",
			len(reachable), len(unreferenced), totalSize)
	}

	// If it's a dry run, just report what would be done
	if options.DryRun {
		if options.Verbose {
			fmt.Println("Dry run - no changes will be made")
			for _, obj := range unreferenced {
				action := "remove"
				if options.AutoPack && !options.Prune {
					action = "pack"
				}
				fmt.Printf("Would %s object: %s (%d bytes)\n", action, obj.Hash, obj.Size)
			}
		}
		stats.ObjectsRemoved = len(unreferenced)
		stats.SpaceSaved = totalSize
		return stats, nil
	}

	// Handle unreferenced objects
	if len(unreferenced) > 0 {
		if options.Prune {
			// Remove unreferenced objects
			if err := removeUnreferencedObjects(repoRoot, unreferenced, options.Verbose); err != nil {
				return stats, fmt.Errorf("failed to remove unreferenced objects: %w", err)
			}
			stats.ObjectsRemoved = len(unreferenced)
			stats.SpaceSaved = totalSize
		} else if options.AutoPack {
			// Extract just the hash strings from ObjectInfo for packing
			hashes := make([]string, len(unreferenced))
			for i, obj := range unreferenced {
				hashes[i] = obj.Hash
			}

			// Pack unreferenced objects
			if err := packUnreferencedObjects(repoRoot, hashes, options.DryRun, options.Verbose); err != nil {
				return stats, fmt.Errorf("failed to pack unreferenced objects: %w", err)
			}
			stats.ObjectsPacked = len(unreferenced)
			// Estimate space saved - typically packing saves about 30-50% of space
			stats.SpaceSaved = totalSize / 2
		}
	}

	// Prune old packfiles
	packfilesBefore, err := findPackfiles(repoRoot)
	if err == nil && len(packfilesBefore) > 1 {
		err = pruneOldPackfiles(repoRoot, 14, options.DryRun, options.Verbose)
		if err != nil {
			if options.Verbose {
				fmt.Printf("Warning: failed to prune old packfiles: %v\n", err)
			}
		} else {
			// Calculate additional space saved from pruning packfiles
			packfilesAfter, _ := findPackfiles(repoRoot)
			prunedCount := len(packfilesBefore) - len(packfilesAfter)
			if prunedCount > 0 && options.Verbose {
				fmt.Printf("Pruned %d old packfiles\n", prunedCount)
			}
			stats.PackfilesPruned = prunedCount
		}
	}

	return stats, nil
}

// ObjectInfo stores information about an object
type ObjectInfo struct {
	Hash string
	Path string
	Size int64
}

// findReachableObjects finds all objects that are reachable from any ref
func findReachableObjects(repoPath string) (map[string]bool, error) {
	reachable := make(map[string]bool)

	// Get all refs
	refsPath := filepath.Join(repoPath, ".vec", "refs")
	err := filepath.WalkDir(refsPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			// Read the commit hash from the ref file
			content, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read ref file %s: %w", path, err)
			}

			commitHash := strings.TrimSpace(string(content))
			if len(commitHash) != 64 {
				// Skip invalid hashes
				return nil
			}

			// Mark the commit and all objects it references as reachable
			if err := markReachableFromObject(repoPath, commitHash, reachable); err != nil {
				// Log error but continue processing other refs
				fmt.Fprintf(os.Stderr, "Warning: error traversing commit %s: %v\n", commitHash, err)
			}
		}

		return nil
	})

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to walk refs directory: %w", err)
	}

	// Check HEAD reference as well
	headPath := filepath.Join(repoPath, ".vec", "HEAD")
	if utils.FileExists(headPath) {
		content, err := os.ReadFile(headPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read HEAD: %w", err)
		}

		head := strings.TrimSpace(string(content))
		if strings.HasPrefix(head, "ref: ") {
			// HEAD points to a ref, which we've already processed
		} else if len(head) == 64 {
			// Detached HEAD, points directly to a commit
			if err := markReachableFromObject(repoPath, head, reachable); err != nil {
				// Log error but continue
				fmt.Fprintf(os.Stderr, "Warning: error traversing HEAD commit %s: %v\n", head, err)
			}
		}
	}

	return reachable, nil
}

// markReachableFromObject marks an object and all objects it references as reachable
func markReachableFromObject(repoPath, hash string, reachable map[string]bool) error {
	// Skip if already marked
	if reachable[hash] {
		return nil
	}

	// Mark this object
	reachable[hash] = true

	// Try to determine the object type
	commit, err := objects.GetCommit(repoPath, hash)
	if err == nil {
		// It's a commit, mark its tree and parents

		// Mark the tree and all its contents
		if err := markReachableFromTree(repoPath, commit.Tree, reachable); err != nil {
			return err
		}

		// Mark all parent commits
		for _, parent := range commit.Parents {
			if err := markReachableFromObject(repoPath, parent, reachable); err != nil {
				return err
			}
		}

		return nil
	}

	// Try if it's a tree
	_, err = objects.GetTree(repoPath, hash)
	if err == nil {
		// Mark the tree and its entries
		return markReachableFromTree(repoPath, hash, reachable)
	}

	// If not a commit or tree, assume it's a blob
	// Blobs don't reference other objects, so just mark it and return
	reachable[hash] = true
	return nil
}

// markReachableFromTree marks a tree and all objects it references as reachable
func markReachableFromTree(repoPath, treeHash string, reachable map[string]bool) error {
	// Skip if already processed
	if reachable[treeHash] {
		return nil
	}

	// Mark this tree
	reachable[treeHash] = true

	// Get the tree object
	tree, err := objects.GetTree(repoPath, treeHash)
	if err != nil {
		return err
	}

	// Mark all entries in the tree
	for _, entry := range tree.Entries {
		if entry.Type == "tree" {
			// Recursive call for subtrees
			if err := markReachableFromTree(repoPath, entry.Hash, reachable); err != nil {
				return err
			}
		} else {
			// Mark blobs
			reachable[entry.Hash] = true
		}
	}

	return nil
}

// findAllObjects finds all objects in the repository
func findAllObjects(repoPath string) ([]ObjectInfo, error) {
	var allObjects []ObjectInfo
	objectsDir := filepath.Join(repoPath, ".vec", "objects")

	// Check if objects directory exists
	if !utils.FileExists(objectsDir) {
		return nil, fmt.Errorf("objects directory not found at %s", objectsDir)
	}

	// Walk the objects directory
	err := filepath.WalkDir(objectsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories, packfiles and packfile indices
		if d.IsDir() ||
			strings.HasSuffix(path, ".pack") ||
			strings.HasSuffix(path, ".idx") ||
			strings.HasSuffix(path, ".info") {
			return nil
		}

		// Get the relative path from the objects directory
		relPath, err := filepath.Rel(objectsDir, path)
		if err != nil {
			return err
		}

		// Check for the expected 2-level directory structure (XX/YYYYYYY...)
		parts := strings.Split(relPath, string(filepath.Separator))
		if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 62 {
			// Skip non-standard files
			return nil
		}

		// Construct the full hash
		hash := parts[0] + parts[1]

		// Get file info
		info, err := d.Info()
		if err != nil {
			return err
		}

		allObjects = append(allObjects, ObjectInfo{
			Hash: hash,
			Path: path,
			Size: info.Size(),
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk objects directory: %w", err)
	}

	return allObjects, nil
}

// removeUnreferencedObjects deletes unreferenced objects from the repository
func removeUnreferencedObjects(repoPath string, unreferenced []ObjectInfo, verbose bool) error {
	for _, obj := range unreferenced {
		if verbose {
			fmt.Printf("Removing unreferenced object: %s\n", obj.Hash)
		}

		if err := os.Remove(obj.Path); err != nil {
			return fmt.Errorf("failed to remove object %s: %w", obj.Hash, err)
		}
	}

	return nil
}

// packUnreferencedObjects packs unreferenced objects into packfiles
func packUnreferencedObjects(repoPath string, hashes []string, dryRun, verbose bool) error {
	if len(hashes) == 0 {
		return nil // Nothing to do
	}

	packDir := filepath.Join(repoPath, ".vec", "objects", "pack")
	if err := utils.EnsureDirExists(packDir); err != nil {
		return fmt.Errorf("failed to create pack directory: %w", err)
	}

	// Create temporary packfile
	timestamp := time.Now().Unix()
	packfileName := fmt.Sprintf("pack-%d.pack", timestamp)
	indexName := fmt.Sprintf("pack-%d.idx", timestamp)
	packfilePath := filepath.Join(packDir, packfileName)
	indexPath := filepath.Join(packDir, indexName)

	if verbose {
		fmt.Printf("Packing %d objects into %s\n", len(hashes), packfileName)
	}

	if dryRun {
		return nil // Skip actual packing in dry run
	}

	// In a real implementation, this would create the packfile and index
	// For simplicity, we'll just create empty files
	if err := os.WriteFile(packfilePath, []byte{}, 0644); err != nil {
		return fmt.Errorf("failed to create packfile: %w", err)
	}

	if err := os.WriteFile(indexPath, []byte{}, 0644); err != nil {
		return fmt.Errorf("failed to create packfile index: %w", err)
	}

	// In a real implementation, we would then:
	// 1. Serialize each object into the packfile
	// 2. Create an index for the packfile
	// 3. Remove the original loose objects

	// Simulating removal of loose objects after packing
	for _, hash := range hashes {
		prefix := hash[:2]
		suffix := hash[2:]
		objectPath := filepath.Join(repoPath, ".vec", "objects", prefix, suffix)

		if utils.FileExists(objectPath) {
			if err := os.Remove(objectPath); err != nil {
				return fmt.Errorf("failed to remove packed object %s: %w", hash, err)
			}
		}
	}

	return nil
}

// findPackfiles finds all packfiles in the repository
func findPackfiles(repoPath string) ([]string, error) {
	packDir := filepath.Join(repoPath, ".vec", "objects", "pack")
	if !utils.FileExists(packDir) {
		return nil, nil // No pack directory
	}

	entries, err := os.ReadDir(packDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read pack directory: %w", err)
	}

	var packfiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".pack") {
			packfiles = append(packfiles, entry.Name())
		}
	}

	return packfiles, nil
}

// packGroup represents a packfile and its associated index file
type packGroup struct {
	packfile string
	index    string
	modTime  time.Time
}

// pruneOldPackfiles removes packfiles older than the specified age in days
func pruneOldPackfiles(repoPath string, maxAgeDays int, dryRun, verbose bool) error {
	packDir := filepath.Join(repoPath, ".vec", "objects", "pack")
	if !utils.FileExists(packDir) {
		return nil // No pack directory
	}

	entries, err := os.ReadDir(packDir)
	if err != nil {
		return fmt.Errorf("failed to read pack directory: %w", err)
	}

	// Group packfiles with their indices
	packGroups := make(map[string]*packGroup)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".pack") && !strings.HasSuffix(name, ".idx") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Extract the base name (without extension)
		baseName := strings.TrimSuffix(strings.TrimSuffix(name, ".pack"), ".idx")

		group, exists := packGroups[baseName]
		if !exists {
			group = &packGroup{modTime: info.ModTime()}
			packGroups[baseName] = group
		}

		if strings.HasSuffix(name, ".pack") {
			group.packfile = name
		} else if strings.HasSuffix(name, ".idx") {
			group.index = name
		}
	}

	// Sort groups by modification time
	var groups []*packGroup
	for _, group := range packGroups {
		// Only include complete groups (both .pack and .idx)
		if group.packfile != "" && group.index != "" {
			groups = append(groups, group)
		}
	}

	// Sort by modification time (oldest first)
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].modTime.Before(groups[j].modTime)
	})

	// Keep at least one packfile
	if len(groups) <= 1 {
		return nil
	}

	// Calculate cutoff time
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)

	// Remove old packfiles
	for _, group := range groups[:len(groups)-1] { // Keep the newest one
		if group.modTime.After(cutoff) {
			continue // Skip packfiles newer than the cutoff
		}

		if verbose {
			fmt.Printf("Pruning old packfile: %s (modified %s)\n", group.packfile, group.modTime)
		}

		if !dryRun {
			// Delete the packfile and its index
			packPath := filepath.Join(packDir, group.packfile)
			indexPath := filepath.Join(packDir, group.index)
			if err := os.Remove(packPath); err != nil {
				return err
			}
			if err := os.Remove(indexPath); err != nil {
				return err
			}
		}
	}

	return nil
}
