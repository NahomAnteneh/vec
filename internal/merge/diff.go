package merge

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// mergeFiles merges the content of two files against a common base version.
func mergeFiles(baseSha, oursSha, theirsSha, path string, repoRoot string, strategy MergeStrategy) (MergeResult, error) {
	// Initialize result with known file identifiers
	result := MergeResult{
		Path:      path,
		BaseSha:   baseSha,
		OursSha:   oursSha,
		TheirsSha: theirsSha,
	}

	// If both sides deleted the file, it's deleted in the result
	if oursSha == "" && theirsSha == "" {
		return result, nil
	}

	// Get file contents
	var baseContent, oursContent, theirsContent []byte
	var err error

	if baseSha != "" {
		baseContent, err = objects.GetBlob(repoRoot, baseSha)
		if err != nil {
			return result, fmt.Errorf("error getting base content: %w", err)
		}
	}

	if oursSha != "" {
		oursContent, err = objects.GetBlob(repoRoot, oursSha)
		if err != nil {
			return result, fmt.Errorf("error getting our content: %w", err)
		}
	} else {
		// Our side deleted the file - use empty content
		oursContent = []byte{}
	}

	if theirsSha != "" {
		theirsContent, err = objects.GetBlob(repoRoot, theirsSha)
		if err != nil {
			return result, fmt.Errorf("error getting their content: %w", err)
		}
	} else {
		// Their side deleted the file - use empty content
		theirsContent = []byte{}
	}

	// Check for binary content
	isBinary := isBinaryContent(baseContent) || isBinaryContent(oursContent) || isBinaryContent(theirsContent)

	// Handle binary files differently
	if isBinary {
		// For binary files, we can't really merge - mark as conflict
		result.HasConflicts = true
		result.ConflictType = "binary"
		// Don't set content - the caller should use handleBinaryConflict
		return result, nil
	}

	// Apply strategy for auto-resolution if specified
	if strategy == MergeStrategyOurs {
		result.Content = oursContent
		return result, nil
	} else if strategy == MergeStrategyTheirs {
		result.Content = theirsContent
		return result, nil
	}

	// Use diffmatchpatch library for text merging
	dmp := diffmatchpatch.New()

	// Convert byte arrays to strings
	baseText := string(baseContent)
	oursText := string(oursContent)
	theirsText := string(theirsContent)

	// Perform the merge using the diffmatchpatch library
	mergedText, hasConflicts := performThreeWayMerge(dmp, baseText, oursText, theirsText)

	if hasConflicts {
		result.HasConflicts = true
	}

	result.Content = []byte(mergedText)
	return result, nil
}

// isBinaryContent checks if content contains null bytes (indicating binary data)
func isBinaryContent(content []byte) bool {
	if len(content) == 0 {
		return false
	}

	// Check for null bytes which typically indicate binary content
	for _, b := range content {
		if b == 0 {
			return true
		}
	}
	return false
}

// performThreeWayMerge uses the diffmatchpatch library to perform a three-way merge
func performThreeWayMerge(dmp *diffmatchpatch.DiffMatchPatch, baseText, oursText, theirsText string) (string, bool) {
	hasConflicts := false

	// Calculate diffs between base and ours for the patch
	diffBase2Ours := dmp.DiffMain(baseText, oursText, false)

	// We'll use a line-by-line approach for better merge results
	baseLines := strings.Split(baseText, "\n")
	oursLines := strings.Split(oursText, "\n")
	theirsLines := strings.Split(theirsText, "\n")

	// Create patch objects
	patchOurs := dmp.PatchMake(baseText, diffBase2Ours)

	// Try to apply the patch
	mergedText, appliedOurs := dmp.PatchApply(patchOurs, theirsText)

	// Check if there were any conflicts (patches that couldn't be applied)
	for _, applied := range appliedOurs {
		if !applied {
			hasConflicts = true
			break
		}
	}

	// If we have conflicts, generate a conflict-marked version with conflict markers
	if hasConflicts {
		return generateConflictMarkedText(baseLines, oursLines, theirsLines), true
	}

	return mergedText, false
}

// generateConflictMarkedText creates text with conflict markers for manual resolution
func generateConflictMarkedText(baseLines, oursLines, theirsLines []string) string {
	var result bytes.Buffer

	// Analyze lines to find conflicting regions
	linesProcessed := 0
	inConflict := false

	// Get the total number of lines to process
	totalLines := max(len(baseLines), max(len(oursLines), len(theirsLines)))

	for linesProcessed < totalLines {
		// Check if current line has conflicts
		hasOursChange := linesProcessed < len(oursLines) &&
			(linesProcessed >= len(baseLines) ||
				baseLines[linesProcessed] != oursLines[linesProcessed])

		hasTheirsChange := linesProcessed < len(theirsLines) &&
			(linesProcessed >= len(baseLines) ||
				baseLines[linesProcessed] != theirsLines[linesProcessed])

		// Both sides changed the same line(s)
		if hasOursChange && hasTheirsChange {
			// Start a conflict block if we're not already in one
			if !inConflict {
				result.WriteString(ConflictMarkerStart + "ours\n")
				inConflict = true
			}

			// Write our version
			if linesProcessed < len(oursLines) {
				result.WriteString(oursLines[linesProcessed] + "\n")
			}

			// Close our side and start their side only if we're at the end of our changes
			if linesProcessed+1 >= len(oursLines) ||
				(linesProcessed+1 < len(baseLines) &&
					linesProcessed+1 < len(oursLines) &&
					baseLines[linesProcessed+1] == oursLines[linesProcessed+1]) {

				result.WriteString(ConflictMarkerSeparator + "\n")

				// Write their version
				if linesProcessed < len(theirsLines) {
					result.WriteString(theirsLines[linesProcessed] + "\n")
				}

				// End the conflict block
				result.WriteString(ConflictMarkerEnd + "theirs\n")
				inConflict = false
			}
		} else if hasOursChange {
			// Only our side changed this line
			if linesProcessed < len(oursLines) {
				result.WriteString(oursLines[linesProcessed] + "\n")
			}
		} else if hasTheirsChange {
			// Only their side changed this line
			if linesProcessed < len(theirsLines) {
				result.WriteString(theirsLines[linesProcessed] + "\n")
			}
		} else {
			// No changes or identical changes
			if linesProcessed < len(baseLines) {
				result.WriteString(baseLines[linesProcessed] + "\n")
			}
		}

		linesProcessed++
	}

	return result.String()
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
