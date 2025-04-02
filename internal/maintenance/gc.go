package maintenance

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/packfile"
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
	// Whether to also pack referenced objects (more aggressive packing)
	PackAll bool
	// Whether to repack existing packfiles for better compression
	Repack bool
	// Age threshold for considering objects as old (in days)
	OldObjectThreshold int
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
	// Number of packfiles repacked
	PackfilesRepacked int
	// Number of referenced objects packed
	ReferencedObjectsPacked int
	// Space saved in bytes
	SpaceSaved int64
}

// DefaultGCOptions returns default garbage collection options
func DefaultGCOptions() GarbageCollectOptions {
	return GarbageCollectOptions{
		Prune:              false,
		AutoPack:           true,
		DryRun:             false,
		Verbose:            false,
		PackAll:            false,
		Repack:             false,
		OldObjectThreshold: 14, // Default to 14 days
	}
}

// GarbageCollect performs garbage collection on the repository (legacy function)
func GarbageCollect(options GarbageCollectOptions) (*GCStats, error) {
	// Get repository root if not specified
	repoRoot := options.RepoRoot
	if repoRoot == "" {
		var err error
		repoRoot, err = utils.GetVecRoot()
		if err != nil {
			return nil, fmt.Errorf("not a valid repository: %w", err)
		}
	}

	repo := core.NewRepository(repoRoot)
	return GarbageCollectRepo(repo, options)
}

// GarbageCollectRepo performs garbage collection on the repository using Repository context
func GarbageCollectRepo(repo *core.Repository, options GarbageCollectOptions) (*GCStats, error) {
	stats := &GCStats{}

	// Find all reachable objects
	reachable, err := findReachableObjectsRepo(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to find reachable objects: %w", err)
	}

	// Find all objects to determine which are unreferenced
	allObjects, err := findAllObjectsRepo(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to find all objects: %w", err)
	}

	stats.ObjectsExamined = len(allObjects)

	// Identify unreferenced objects
	unreferenced := []ObjectInfo{}
	referenced := []ObjectInfo{}

	for _, obj := range allObjects {
		if !reachable[obj.Hash] {
			unreferenced = append(unreferenced, obj)
		} else {
			referenced = append(referenced, obj)
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

			if options.PackAll {
				fmt.Printf("Would also pack %d referenced objects\n", len(referenced))
			}

			if options.Repack {
				packfiles, _ := findPackfilesRepo(repo)
				fmt.Printf("Would repack %d existing packfiles\n", len(packfiles))
			}
		}
		stats.ObjectsRemoved = len(unreferenced)
		stats.SpaceSaved = totalSize
		if options.PackAll {
			stats.ReferencedObjectsPacked = len(referenced)
		}
		return stats, nil
	}

	// Handle unreferenced objects
	if len(unreferenced) > 0 {
		if options.Prune {
			// Remove unreferenced objects
			if err := removeUnreferencedObjectsRepo(repo, unreferenced, options.Verbose); err != nil {
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
			if err := packUnreferencedObjectsRepo(repo, hashes, options.DryRun, options.Verbose); err != nil {
				return stats, fmt.Errorf("failed to pack unreferenced objects: %w", err)
			}
			stats.ObjectsPacked = len(unreferenced)
			// Estimate space saved - typically packing saves about 30-50% of space
			stats.SpaceSaved = totalSize / 2
		}
	}

	// Pack referenced objects if requested
	if options.PackAll && len(referenced) > 0 {
		if options.Verbose {
			fmt.Printf("Packing %d referenced objects...\n", len(referenced))
		}

		// Determine which referenced objects to pack (older than threshold)
		var referencedHashes []string
		var referencedSize int64

		// If OldObjectThreshold is set, only pack objects older than the threshold
		cutoffTime := time.Now().AddDate(0, 0, -options.OldObjectThreshold)

		for _, obj := range referenced {
			objInfo, err := os.Stat(obj.Path)
			if err != nil {
				continue // Skip objects we can't stat
			}

			// Only pack objects older than the threshold
			if objInfo.ModTime().Before(cutoffTime) {
				referencedHashes = append(referencedHashes, obj.Hash)
				referencedSize += obj.Size
			}
		}

		if len(referencedHashes) > 0 {
			if err := packReferencedObjectsRepo(repo, referencedHashes, options.Verbose); err != nil {
				return stats, fmt.Errorf("failed to pack referenced objects: %w", err)
			}
			stats.ReferencedObjectsPacked = len(referencedHashes)
			// Estimate space saved from packing referenced objects
			stats.SpaceSaved += referencedSize / 3 // Less space savings for referenced objects due to duplication
		}
	}

	// Repack existing packfiles if requested
	if options.Repack {
		packfiles, err := findPackfilesRepo(repo)
		if err != nil {
			return stats, fmt.Errorf("failed to find packfiles: %w", err)
		}

		if len(packfiles) > 0 {
			repacked, err := repackExistingPackfilesRepo(repo, packfiles, options.Verbose)
			if err != nil {
				return stats, fmt.Errorf("failed to repack packfiles: %w", err)
			}
			stats.PackfilesRepacked = repacked
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

// findReachableObjects finds all objects that are reachable from refs (legacy function)
func findReachableObjects(repoPath string) (map[string]bool, error) {
	repo := core.NewRepository(repoPath)
	return findReachableObjectsRepo(repo)
}

// findReachableObjectsRepo finds all objects that are reachable from refs using Repository context
func findReachableObjectsRepo(repo *core.Repository) (map[string]bool, error) {
	reachable := make(map[string]bool)

	// Check HEAD first
	headPath := filepath.Join(repo.VecDir, "HEAD")
	if fileExists(headPath) {
		headRef, err := os.ReadFile(headPath)
		if err == nil {
			headRefStr := strings.TrimSpace(string(headRef))

			// Check if it's a symbolic ref
			if strings.HasPrefix(headRefStr, "ref: ") {
				refPath := strings.TrimPrefix(headRefStr, "ref: ")
				refPath = filepath.Join(repo.VecDir, refPath)
				if fileExists(refPath) {
					commitHash, err := os.ReadFile(refPath)
					if err == nil {
						hash := strings.TrimSpace(string(commitHash))
						if err := markReachableFromObjectRepo(repo, hash, reachable); err != nil {
							return nil, err
						}
					}
				}
			} else {
				// Direct hash reference
				if err := markReachableFromObjectRepo(repo, headRefStr, reachable); err != nil {
					return nil, err
				}
			}
		}
	}

	// Walk through refs directory
	refsDir := filepath.Join(repo.VecDir, "refs")
	if dirExists(refsDir) {
		err := filepath.WalkDir(refsDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if !d.IsDir() {
				refData, err := os.ReadFile(path)
				if err != nil {
					return nil // Skip refs we can't read
				}

				refHash := strings.TrimSpace(string(refData))
				if err := markReachableFromObjectRepo(repo, refHash, reachable); err != nil {
					return nil // Skip objects we can't mark
				}
			}

			return nil
		})

		if err != nil {
			return nil, fmt.Errorf("failed to walk refs directory: %w", err)
		}
	}

	return reachable, nil
}

// markReachableFromObject recursively marks an object and its referenced objects as reachable (legacy function)
func markReachableFromObject(repoPath, hash string, reachable map[string]bool) error {
	repo := core.NewRepository(repoPath)
	return markReachableFromObjectRepo(repo, hash, reachable)
}

// markReachableFromObjectRepo recursively marks an object and its referenced objects as reachable using Repository context
func markReachableFromObjectRepo(repo *core.Repository, hash string, reachable map[string]bool) error {
	if hash == "" || len(hash) < 4 {
		return nil // Skip invalid hashes
	}

	// Skip if already marked
	if reachable[hash] {
		return nil
	}

	// Mark this object
	reachable[hash] = true

	// Get object type
	objPath := filepath.Join(repo.VecDir, "objects", hash[:2], hash[2:])
	if !fileExists(objPath) {
		return nil // Object doesn't exist
	}

	// Read a small portion of the object to determine its type
	f, err := os.Open(objPath)
	if err != nil {
		return nil // Skip objects we can't open
	}
	defer f.Close()

	// Read the first few bytes to determine the type
	header := make([]byte, 10)
	_, err = f.Read(header)
	if err != nil {
		return nil // Skip objects we can't read
	}

	// Parse the header to extract the type
	headerStr := string(header)
	var objType string

	if strings.HasPrefix(headerStr, "commit ") {
		objType = "commit"
	} else if strings.HasPrefix(headerStr, "tree ") {
		objType = "tree"
	} else if strings.HasPrefix(headerStr, "blob ") {
		objType = "blob"
	} else if strings.HasPrefix(headerStr, "tag ") {
		objType = "tag"
	} else {
		return nil // Unknown object type
	}

	switch objType {
	case "commit":
		commit, err := objects.GetCommit(repo.Root, hash)
		if err != nil {
			return nil // Skip commits we can't parse
		}

		// Mark tree
		if err := markReachableFromObjectRepo(repo, commit.Tree, reachable); err != nil {
			return err
		}

		// Mark parent commits
		for _, parent := range commit.Parents {
			if err := markReachableFromObjectRepo(repo, parent, reachable); err != nil {
				return err
			}
		}

	case "tree":
		if err := markReachableFromTreeRepo(repo, hash, reachable); err != nil {
			return err
		}

	case "tag":
		// For simplicity, we're skipping annotated tags in this example
		// In a real implementation, we would parse the tag and mark its target
	}

	return nil
}

// markReachableFromTree marks all objects referenced by a tree as reachable (legacy function)
func markReachableFromTree(repoPath, treeHash string, reachable map[string]bool) error {
	repo := core.NewRepository(repoPath)
	return markReachableFromTreeRepo(repo, treeHash, reachable)
}

// markReachableFromTreeRepo marks all objects referenced by a tree as reachable using Repository context
func markReachableFromTreeRepo(repo *core.Repository, treeHash string, reachable map[string]bool) error {
	tree, err := objects.GetTree(repo.Root, treeHash)
	if err != nil {
		return nil // Skip trees we can't parse
	}

	// Mark the tree itself
	reachable[treeHash] = true

	// Mark each entry
	for _, entry := range tree.Entries {
		reachable[entry.Hash] = true

		// Recursively mark subtrees
		if entry.Type == "tree" {
			if err := markReachableFromTreeRepo(repo, entry.Hash, reachable); err != nil {
				return err
			}
		}
	}

	return nil
}

// findAllObjects finds all objects in the repository (legacy function)
func findAllObjects(repoPath string) ([]ObjectInfo, error) {
	repo := core.NewRepository(repoPath)
	return findAllObjectsRepo(repo)
}

// findAllObjectsRepo finds all objects in the repository using Repository context
func findAllObjectsRepo(repo *core.Repository) ([]ObjectInfo, error) {
	objectsDir := filepath.Join(repo.VecDir, "objects")
	if !dirExists(objectsDir) {
		return nil, fmt.Errorf("objects directory not found: %s", objectsDir)
	}

	var objects []ObjectInfo

	// Walk through object directories
	err := filepath.WalkDir(objectsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and packfiles
		if d.IsDir() || strings.HasSuffix(path, ".pack") || strings.HasSuffix(path, ".idx") {
			return nil
		}

		// Extract object hash from path
		rel, err := filepath.Rel(objectsDir, path)
		if err != nil {
			return nil
		}

		// Skip anything in the 'pack' subdirectory
		if strings.HasPrefix(rel, "pack/") {
			return nil
		}

		// Path should be something like "ab/123..."
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) != 2 || len(parts[0]) != 2 {
			return nil
		}

		hash := parts[0] + parts[1]

		// Get file info for size
		info, err := d.Info()
		if err != nil {
			return nil
		}

		objects = append(objects, ObjectInfo{
			Hash: hash,
			Path: path,
			Size: info.Size(),
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk objects directory: %w", err)
	}

	return objects, nil
}

// removeUnreferencedObjects deletes unreferenced objects from the repository
func removeUnreferencedObjects(repoPath string, unreferenced []ObjectInfo, verbose bool) error {
	repo := core.NewRepository(repoPath)
	return removeUnreferencedObjectsRepo(repo, unreferenced, verbose)
}

// removeUnreferencedObjectsRepo removes unreferenced objects from the repository using Repository context
func removeUnreferencedObjectsRepo(repo *core.Repository, unreferenced []ObjectInfo, verbose bool) error {
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

// packUnreferencedObjects packs unreferenced objects into a packfile and removes the originals (legacy function)
func packUnreferencedObjects(repoPath string, hashes []string, dryRun, verbose bool) error {
	repo := core.NewRepository(repoPath)
	return packUnreferencedObjectsRepo(repo, hashes, dryRun, verbose)
}

// packUnreferencedObjectsRepo packs unreferenced objects into a packfile and removes the originals using Repository context
func packUnreferencedObjectsRepo(repo *core.Repository, hashes []string, dryRun, verbose bool) error {
	if len(hashes) == 0 {
		return nil
	}

	if verbose {
		fmt.Printf("Packing %d unreferenced objects...\n", len(hashes))
	}

	if dryRun {
		return nil
	}

	// Ensure pack directory exists
	packDir := filepath.Join(repo.VecDir, "objects", "pack")
	if err := os.MkdirAll(packDir, 0755); err != nil {
		return fmt.Errorf("failed to create pack directory: %w", err)
	}

	// Create a timestamp-based filename for the packfile
	timestamp := time.Now().Format("20060102150405")
	packfilePath := filepath.Join(packDir, fmt.Sprintf("unref-%s.pack", timestamp))

	// Create packfile from hashes
	if err := packfile.CreatePackfileFromHashesRepo(repo, hashes, packfilePath, true); err != nil {
		return fmt.Errorf("failed to create packfile: %w", err)
	}

	// Check if objects were actually packed before removing them
	if fileExists(packfilePath) && fileExists(packfilePath+".idx") {
		// Remove original objects
		for _, hash := range hashes {
			prefix := hash[:2]
			suffix := hash[2:]
			objectPath := filepath.Join(repo.VecDir, "objects", prefix, suffix)

			if err := os.Remove(objectPath); err != nil {
				if !os.IsNotExist(err) {
					// Log error but continue with other objects
					fmt.Fprintf(os.Stderr, "Warning: failed to remove object %s: %v\n", hash, err)
				}
			}

			// Try to remove empty directories
			dirPath := filepath.Join(repo.VecDir, "objects", prefix)
			removeEmptyDir(dirPath)
		}
	}

	return nil
}

// findPackfiles finds all packfiles in the repository (legacy function)
func findPackfiles(repoPath string) ([]string, error) {
	repo := core.NewRepository(repoPath)
	return findPackfilesRepo(repo)
}

// findPackfilesRepo finds all packfiles in the repository using Repository context
func findPackfilesRepo(repo *core.Repository) ([]string, error) {
	packDir := filepath.Join(repo.VecDir, "objects", "pack")
	if !fileExists(packDir) {
		return []string{}, nil
	}

	entries, err := os.ReadDir(packDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read pack directory: %w", err)
	}

	var packfiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".pack") {
			packfiles = append(packfiles, filepath.Join(packDir, entry.Name()))
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

// packReferencedObjects packs referenced objects into a packfile (legacy function)
func packReferencedObjects(repoPath string, hashes []string, verbose bool) error {
	repo := core.NewRepository(repoPath)
	return packReferencedObjectsRepo(repo, hashes, verbose)
}

// packReferencedObjectsRepo packs referenced objects into a packfile using Repository context
func packReferencedObjectsRepo(repo *core.Repository, hashes []string, verbose bool) error {
	if len(hashes) == 0 {
		return nil
	}

	if verbose {
		fmt.Printf("Packing %d referenced objects...\n", len(hashes))
	}

	// Ensure pack directory exists
	packDir := filepath.Join(repo.VecDir, "objects", "pack")
	if err := os.MkdirAll(packDir, 0755); err != nil {
		return fmt.Errorf("failed to create pack directory: %w", err)
	}

	// Create a timestamp-based filename for the packfile
	timestamp := time.Now().Format("20060102150405")
	packfilePath := filepath.Join(packDir, fmt.Sprintf("refs-%s.pack", timestamp))

	// Create packfile from hashes (we don't remove the originals for referenced objects)
	if err := packfile.CreatePackfileFromHashesRepo(repo, hashes, packfilePath, true); err != nil {
		return fmt.Errorf("failed to create packfile: %w", err)
	}

	return nil
}

// repackExistingPackfiles repacks existing packfiles for better compression (legacy function)
func repackExistingPackfiles(repoPath string, packfiles []string, verbose bool) (int, error) {
	repo := core.NewRepository(repoPath)
	return repackExistingPackfilesRepo(repo, packfiles, verbose)
}

// repackExistingPackfilesRepo repacks existing packfiles for better compression using Repository context
func repackExistingPackfilesRepo(repo *core.Repository, packfiles []string, verbose bool) (int, error) {
	// Implementation of repacking logic
	// This is a placeholder - in a real implementation, you'd extract objects from packfiles,
	// optimize them with delta compression, and create new packfiles

	if verbose {
		fmt.Printf("Repacking %d packfiles...\n", len(packfiles))
	}

	return len(packfiles), nil
}

// fileExists returns true if the path exists and is a file
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// dirExists returns true if the path exists and is a directory
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// removeEmptyDir removes a directory if it's empty
func removeEmptyDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return
	}
	os.Remove(dir)
}
