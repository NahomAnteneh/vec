package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FindObjectByPartialHash looks up a full hash by a partial hash prefix
// by searching the objects directory. Returns the full hash if found,
// or an error if no match or multiple matches are found.
func FindObjectByPartialHash(repoRoot, partialHash string) (string, error) {
	if len(partialHash) < 4 {
		return "", fmt.Errorf("hash prefix too short (minimum 4 characters)")
	}

	// Ensure the hash is lowercase
	partialHash = strings.ToLower(partialHash)

	// Normalize the hash format
	partialHash = strings.TrimSpace(partialHash)

	// Objects directory path
	objectsDir := filepath.Join(repoRoot, ".vec", "objects")

	// Search loose objects first
	matchingObjects := []string{}

	// Check if the first two characters of the partial hash exist as a directory
	if len(partialHash) >= 2 {
		prefix := partialHash[:2]
		suffix := partialHash[2:]
		prefixDir := filepath.Join(objectsDir, prefix)

		if _, err := os.Stat(prefixDir); err == nil {
			// Read the directory
			entries, err := os.ReadDir(prefixDir)
			if err != nil {
				return "", fmt.Errorf("failed to read objects directory: %w", err)
			}

			// Look for files starting with the suffix
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasPrefix(entry.Name(), suffix) {
					fullHash := prefix + entry.Name()
					matchingObjects = append(matchingObjects, fullHash)
				}
			}
		}
	}

	// Also search packfiles if no match found or if we need more matches
	if len(matchingObjects) == 0 {
		// TODO: Implement packfile search once packfile reading is fully implemented
		// This would involve reading packfile indexes and checking for objects that match
		// the partial hash
	}

	// Check results
	switch len(matchingObjects) {
	case 0:
		return "", fmt.Errorf("no object found with hash prefix '%s'", partialHash)
	case 1:
		return matchingObjects[0], nil
	default:
		// Multiple matches found, provide details in the error
		matches := strings.Join(matchingObjects, ", ")
		return "", fmt.Errorf("multiple objects found with prefix '%s': %s", partialHash, matches)
	}
}
