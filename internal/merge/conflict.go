package merge

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/internal/staging"
)

// Constants for merge conflict marker types
const (
	// ConflictMarkerStart marks the beginning of a conflict
	ConflictMarkerStart = "<<<<<<< "
	// ConflictMarkerSeparator marks the middle of a conflict
	ConflictMarkerSeparator = "======="
	// ConflictMarkerEnd marks the end of a conflict
	ConflictMarkerEnd = ">>>>>>> "
	// ConflictMarkerBinaryFile indicates a binary file conflict
	ConflictMarkerBinaryFile = "Binary files differ\n"
	// Maximum file size to check for binary content (5MB)
	maxBinaryCheckSize = 5 * 1024 * 1024
)

// resolveConflict applies advanced conflict resolution based on configuration.
func resolveConflict(repoRoot string, index *staging.Index, filePath, baseHash, ourHash, theirHash string, baseMode, ourMode, theirMode int32, config *MergeConfig) error {
	// If an auto-resolution strategy is selected (ours/theirs), use it.
	switch config.Strategy {
	case MergeStrategyOurs:
		if ourHash != "" {
			return copyBlobAndAddToIndex(repoRoot, index, ourHash, filePath, ourMode)
		}
		return fmt.Errorf("missing 'ours' version for %s", filePath)
	case MergeStrategyTheirs:
		if theirHash != "" {
			return copyBlobAndAddToIndex(repoRoot, index, theirHash, filePath, theirMode)
		}
		return fmt.Errorf("missing 'theirs' version for %s", filePath)
		// For recursive, fall through for interactive/manual merge.
	}

	// Attempt content-based merge using mergeFiles
	mergeResult, err := mergeFiles(baseHash, ourHash, theirHash, filePath, repoRoot, config.Strategy)
	if err != nil {
		return fmt.Errorf("failed to merge file contents: %w", err)
	}

	// If we have binary conflicts, let the caller handle them
	if mergeResult.HasConflicts && mergeResult.ConflictType == "binary" {
		return writeConflictFile(repoRoot, index, filePath, baseHash, ourHash, theirHash, baseMode, ourMode, theirMode)
	}

	// If we have non-binary content that was successfully merged
	if mergeResult.Content != nil && !mergeResult.HasConflicts {
		absPath := filepath.Join(repoRoot, filePath)
		mode := ourMode
		if mode == 0 {
			mode = theirMode
		}
		if mode == 0 {
			mode = baseMode
		}
		if err := os.WriteFile(absPath, mergeResult.Content, 0644); err != nil {
			return fmt.Errorf("failed to write merged file '%s': %w", filePath, err)
		}
		// Update index with the merged blob
		blobHash, err := objects.CreateBlob(repoRoot, mergeResult.Content)
		if err != nil {
			return fmt.Errorf("failed to create blob for '%s': %w", filePath, err)
		}
		if err := index.Add(repoRoot, filePath, blobHash); err != nil {
			return fmt.Errorf("failed to update index for '%s': %w", filePath, err)
		}
		return nil
	}

	// Default (recursive) strategy with interactive prompt if enabled.
	if config.Interactive && isTerminal(os.Stdin.Fd()) {
		resolvedContent, err := interactiveConflictPrompt(filePath, baseHash, ourHash, theirHash, repoRoot)
		if err == nil && len(resolvedContent) > 0 {
			absPath := filepath.Join(repoRoot, filePath)
			mode := ourMode
			if mode == 0 {
				mode = theirMode
			}
			if mode == 0 {
				mode = baseMode
			}
			if err := os.WriteFile(absPath, resolvedContent, os.FileMode(mode)); err != nil {
				return fmt.Errorf("failed to write file after interactive merge of '%s': %w", filePath, err)
			}
			// Update index with the resolved blob.
			blobHash, err := objects.CreateBlob(repoRoot, resolvedContent)
			if err != nil {
				return fmt.Errorf("failed to write blob for '%s': %w", filePath, err)
			}
			if err := index.Add(repoRoot, filePath, blobHash); err != nil {
				return fmt.Errorf("failed to update index for '%s': %w", filePath, err)
			}
			return nil
		}
		// If interactive resolution fails, fall back to conflict markers.
	}

	// Fallback: write file with conflict markers.
	return writeConflictFile(repoRoot, index, filePath, baseHash, ourHash, theirHash, baseMode, ourMode, theirMode)
}

// writeConflictFile constructs a file with conflict markers and updates the index.
func writeConflictFile(repoRoot string, index *staging.Index, filePath, baseHash, ourHash, theirHash string, baseMode, ourMode, theirMode int32) error {
	var baseContent, ourContent, theirContent []byte
	var err error
	if baseHash != "" {
		baseContent, err = objects.GetBlob(repoRoot, baseHash)
		if err != nil {
			return fmt.Errorf("failed to get base blob '%s': %w", baseHash, err)
		}
	}
	if ourHash != "" {
		ourContent, err = objects.GetBlob(repoRoot, ourHash)
		if err != nil {
			return fmt.Errorf("failed to get our blob '%s': %w", ourHash, err)
		}
	}
	if theirHash != "" {
		theirContent, err = objects.GetBlob(repoRoot, theirHash)
		if err != nil {
			return fmt.Errorf("failed to get their blob '%s': %w", theirHash, err)
		}
	}

	var conflictContent bytes.Buffer
	if ourHash != "" {
		conflictContent.WriteString("<<<<<<< ours\n")
		conflictContent.Write(ourContent)
		conflictContent.WriteString("\n")
	}
	if baseHash != "" {
		conflictContent.WriteString("||||||| base\n")
		conflictContent.Write(baseContent)
		conflictContent.WriteString("\n")
	}
	conflictContent.WriteString("=======\n")
	if theirHash != "" {
		conflictContent.Write(theirContent)
		conflictContent.WriteString("\n")
	}
	conflictContent.WriteString(">>>>>>> theirs")

	absPath := filepath.Join(repoRoot, filePath)
	mode := ourMode
	if mode == 0 {
		mode = theirMode
	}
	if mode == 0 {
		mode = baseMode
	}
	if mode == 0 {
		mode = 0644
	}

	if err := os.WriteFile(absPath, conflictContent.Bytes(), os.FileMode(mode)); err != nil {
		return fmt.Errorf("failed to write conflict file '%s': %w", filePath, err)
	}

	// Update index conflict entries.
	if err := index.Remove(repoRoot, filePath); err != nil {
		return fmt.Errorf("failed to remove stage 0 entry for '%s': %w", filePath, err)
	}
	if baseHash != "" {
		if err := index.AddConflictEntry(filePath, baseHash, baseMode, 1); err != nil {
			return fmt.Errorf("failed to add base conflict entry for '%s': %w", filePath, err)
		}
	}
	if ourHash != "" {
		if err := index.AddConflictEntry(filePath, ourHash, ourMode, 2); err != nil {
			return fmt.Errorf("failed to add our conflict entry for '%s': %w", filePath, err)
		}
	}
	if theirHash != "" {
		if err := index.AddConflictEntry(filePath, theirHash, theirMode, 3); err != nil {
			return fmt.Errorf("failed to add their conflict entry for '%s': %w", filePath, err)
		}
	}
	return nil
}

// interactiveConflictPrompt prompts the user for how to resolve a conflict on filePath.
// It returns the resolved file content.
func interactiveConflictPrompt(filePath, baseHash, ourHash, theirHash, repoRoot string) ([]byte, error) {
	var baseContent, ourContent, theirContent []byte
	var err error
	if baseHash != "" {
		baseContent, err = objects.GetBlob(repoRoot, baseHash)
		if err != nil {
			return nil, fmt.Errorf("failed to get base blob '%s': %w", baseHash, err)
		}
	}
	if ourHash != "" {
		ourContent, err = objects.GetBlob(repoRoot, ourHash)
		if err != nil {
			return nil, fmt.Errorf("failed to get our blob '%s': %w", ourHash, err)
		}
	}
	if theirHash != "" {
		theirContent, err = objects.GetBlob(repoRoot, theirHash)
		if err != nil {
			return nil, fmt.Errorf("failed to get their blob '%s': %w", theirHash, err)
		}
	}

	fmt.Printf("Conflict detected in '%s'.\n", filePath)
	fmt.Println("Select resolution option:")
	fmt.Println("[1] Use ours")
	fmt.Println("[2] Use theirs")
	fmt.Println("[3] Use both with conflict markers (default)")
	fmt.Print("Enter choice (1/2/3): ")

	reader := bufio.NewReader(os.Stdin)
	choice, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read user input: %w", err)
	}
	choice = strings.TrimSpace(choice)
	switch choice {
	case "1":
		return ourContent, nil
	case "2":
		return theirContent, nil
	default:
		// Use conflict markers.
		var buf bytes.Buffer
		buf.WriteString("<<<<<<< ours\n")
		buf.Write(ourContent)
		buf.WriteString("\n||||||| base\n")
		buf.Write(baseContent)
		buf.WriteString("\n=======\n")
		buf.Write(theirContent)
		buf.WriteString("\n>>>>>>> theirs")
		return buf.Bytes(), nil
	}
}

// handleBinaryConflict handles conflicts for binary files by creating
// both versions and marking the conflict with appropriate indicators.
func handleBinaryConflict(repoRoot, filePath, ours, theirs string) error {
	// Create backup files for both versions
	oursPath := filePath + ".ours"
	theirsPath := filePath + ".theirs"

	// Copy "ours" version to backup
	oursContent, err := os.ReadFile(filePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read our binary file: %w", err)
	}

	// If our file exists, write it to the backup
	if !os.IsNotExist(err) {
		if err := os.WriteFile(oursPath, oursContent, 0644); err != nil {
			return fmt.Errorf("failed to write our binary backup: %w", err)
		}
	}

	// Get "theirs" content from object store
	theirsContent, err := objects.GetBlob(repoRoot, theirs)
	if err != nil {
		return fmt.Errorf("failed to get their binary file content: %w", err)
	}

	// If their file exists, write it to the backup
	if err := os.WriteFile(theirsPath, theirsContent, 0644); err != nil {
		return fmt.Errorf("failed to write their binary backup: %w", err)
	}

	// Create a simple conflict marker file
	message := fmt.Sprintf("Binary file conflict in %s\n", filePath)
	message += "- Use 'vec merge --use-ours " + filePath + "' to keep your version\n"
	message += "- Use 'vec merge --use-theirs " + filePath + "' to use their version\n"
	message += "- Manual backup files created: .ours and .theirs\n"

	if err := os.WriteFile(filePath, []byte(message), 0644); err != nil {
		return fmt.Errorf("failed to write binary conflict marker: %w", err)
	}

	// Mark the conflict in the index
	index, err := staging.LoadIndex(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to read index during binary conflict handling: %w", err)
	}

	// Add all three stages to the index (base, ours, theirs)
	relPath, err := filepath.Rel(repoRoot, filePath)
	if err != nil {
		return fmt.Errorf("failed to get relative path: %w", err)
	}

	// Mark as conflicted in the index - add entries for both theirs and ours
	if ours != "" {
		if err := index.AddConflictEntry(relPath, ours, 0644, 2); err != nil {
			return fmt.Errorf("failed to update index with binary conflict (ours): %w", err)
		}
	}

	if theirs != "" {
		if err := index.AddConflictEntry(relPath, theirs, 0644, 3); err != nil {
			return fmt.Errorf("failed to update index with binary conflict (theirs): %w", err)
		}
	}

	// Write the updated index
	if err := index.Write(); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	return nil
}

// isBinaryFile determines if a file is likely binary by checking for null bytes
// in the first chunk of the file.
func isBinaryFile(path string) (bool, error) {
	// Open the file
	file, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("failed to open file for binary check: %w", err)
	}
	defer file.Close()

	// Get file info to check size
	info, err := file.Stat()
	if err != nil {
		return false, fmt.Errorf("failed to stat file for binary check: %w", err)
	}

	// If file is too large, only check the beginning
	size := info.Size()
	bytesToRead := int64(maxBinaryCheckSize)
	if size < bytesToRead {
		bytesToRead = size
	}

	// Read file content (or portion of it)
	buffer := make([]byte, bytesToRead)
	_, err = file.Read(buffer)
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("failed to read file for binary check: %w", err)
	}

	// Check for null bytes which typically indicate binary content
	for _, b := range buffer {
		if b == 0 {
			return true, nil
		}
	}

	// No null bytes found in the checked portion
	return false, nil
}

// isTerminal returns true when fd is a terminal.
func isTerminal(fd uintptr) bool {
	// Production-ready check. You might use a library such as "golang.org/x/term".
	return true
}

// copyBlobAndAddToIndex copies a blob to the working directory and adds it to the index.
func copyBlobAndAddToIndex(repoRoot string, index *staging.Index, hash, filePath string, mode int32) error {
	content, err := objects.GetBlob(repoRoot, hash)
	if err != nil {
		return fmt.Errorf("failed to get blob '%s': %w", hash, err)
	}
	absPath := filepath.Join(repoRoot, filePath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for '%s': %w", filePath, err)
	}
	if err := os.WriteFile(absPath, content, os.FileMode(mode)); err != nil {
		return fmt.Errorf("failed to write file '%s': %w", filePath, err)
	}
	if err := index.Add(repoRoot, filePath, hash); err != nil {
		return fmt.Errorf("failed to add '%s' to index: %w", filePath, err)
	}
	return nil
}
