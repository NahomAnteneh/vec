// Package packfile provides functionality for working with packfiles,
// which are used to efficiently store and transfer multiple objects.
package packfile

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/NahomAnteneh/vec/core"
)

// FinalizePackfile prepares a packfile for transmission by adding checksums and other metadata
func FinalizePackfile(packfile []byte) []byte {
	// This is just a placeholder function that would add any final processing to the packfile
	// In a real implementation, you might add checksums, verify content, etc.
	return packfile
}

// CalculatePackfileChecksum calculates a checksum for the packfile
func CalculatePackfileChecksum(packfile []byte) []byte {
	// Calculate SHA-1 checksum of the packfile
	h := sha1.New()
	h.Write(packfile)
	return h.Sum(nil)
}

// FormatHash formats a binary hash as a hex string
func FormatHash(hash []byte) string {
	return hex.EncodeToString(hash)
}

// ParseHash parses a hex string into a binary hash
func ParseHash(hashStr string) ([]byte, error) {
	return hex.DecodeString(hashStr)
}

// PrintPackfileStats prints statistics about a packfile
func PrintPackfileStats(objects []Object) {
	fmt.Printf("Packfile contains %d objects\n", len(objects))

	// Count objects by type
	typeCounts := make(map[ObjectType]int)
	for _, obj := range objects {
		typeCounts[obj.Type]++
	}

	// Print counts by type
	for t, count := range typeCounts {
		fmt.Printf("  %s: %d objects\n", typeToString(t), count)
	}

	// Calculate and print total data size
	var totalSize int
	for _, obj := range objects {
		totalSize += len(obj.Data)
	}
	fmt.Printf("  Total data size: %d bytes\n", totalSize)
}

// CreatePackfileFromHashes creates a packfile from a list of object hashes in a repository (legacy function).
// This function is used by the maintenance code.
func CreatePackfileFromHashes(repoPath string, objectHashes []string, outputPath string, withDeltaCompression bool) error {
	repo := core.NewRepository(repoPath)
	return CreatePackfileFromHashesRepo(repo, objectHashes, outputPath, withDeltaCompression)
}

// CreatePackfileFromHashesRepo creates a packfile from a list of object hashes in a repository using Repository context.
// This function is used by the maintenance code.
func CreatePackfileFromHashesRepo(repo *core.Repository, objectHashes []string, outputPath string, withDeltaCompression bool) error {
	// Load objects from the repository
	objects := make([]Object, 0, len(objectHashes))
	for _, hash := range objectHashes {
		// For simplicity, assume all objects are blobs
		// In a real implementation, you'd determine the type from the object
		prefix := hash[:2]
		suffix := hash[2:]
		objectPath := filepath.Join(repo.VecDir, "objects", prefix, suffix)

		data, err := os.ReadFile(objectPath)
		if err != nil {
			continue // Skip objects that can't be read
		}

		objects = append(objects, Object{
			Hash: hash,
			Type: OBJ_BLOB, // Simplification - real code would determine type
			Data: data,
		})
	}

	// Apply delta compression if requested
	if withDeltaCompression {
		var err error
		objects, err = OptimizeObjects(objects)
		if err != nil {
			return err
		}
	}

	// Create the packfile
	return CreateModernPackfile(objects, outputPath)
}
