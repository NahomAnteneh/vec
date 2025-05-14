package maintenance

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/utils"
)

// GarbageCollectOptions defines options for garbage collection
type GarbageCollectOptions struct {
	// Root path of the repository
	RepoRoot string
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
	// Space saved in bytes
	SpaceSaved int64
}

// DefaultGCOptions returns default garbage collection options
func DefaultGCOptions() GarbageCollectOptions {
	return GarbageCollectOptions{
		DryRun:  false,
		Verbose: false,
	}
}

// GarbageCollect performs garbage collection on the repository
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
				fmt.Printf("Would remove object: %s (%d bytes)\n", obj.Hash, obj.Size)
			}
		}
		stats.ObjectsRemoved = len(unreferenced)
		stats.SpaceSaved = totalSize
		return stats, nil
	}

	// Remove unreferenced objects
	if len(unreferenced) > 0 {
		if err := removeUnreferencedObjectsRepo(repo, unreferenced, options.Verbose); err != nil {
			return stats, fmt.Errorf("failed to remove unreferenced objects: %w", err)
		}
		stats.ObjectsRemoved = len(unreferenced)
		stats.SpaceSaved = totalSize
	}

	return stats, nil
}

// ObjectInfo stores information about an object
type ObjectInfo struct {
	Hash string
	Path string
	Size int64
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

// markReachableFromObjectRepo recursively marks an object and its referenced objects as reachable
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
	}

	return nil
}

// markReachableFromTreeRepo marks all objects referenced by a tree as reachable
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

// findAllObjectsRepo finds all objects in the repository
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

// removeUnreferencedObjectsRepo removes unreferenced objects from the repository
func removeUnreferencedObjectsRepo(repo *core.Repository, unreferenced []ObjectInfo, verbose bool) error {
	for _, obj := range unreferenced {
		if verbose {
			fmt.Printf("Removing unreferenced object: %s\n", obj.Hash)
		}

		if err := os.Remove(obj.Path); err != nil {
			return fmt.Errorf("failed to remove object %s: %w", obj.Hash, err)
		}

		// Try to remove empty directory
		dirPath := filepath.Join(repo.VecDir, "objects", obj.Hash[:2])
		removeEmptyDir(dirPath)
	}

	return nil
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
