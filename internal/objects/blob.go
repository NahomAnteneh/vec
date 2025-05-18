package objects

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/utils"
)

// CreateBlobRepo creates a new blob object using Repository context.
func CreateBlobRepo(repo *core.Repository, content []byte) (string, error) {
	// Format the blob content with header
	header := fmt.Sprintf("blob %d\x00", len(content))
	var buf bytes.Buffer
	buf.WriteString(header)
	buf.Write(content)
	
	// Calculate hash of the entire object (header + content)
	fullContent := buf.Bytes() 
	hash := utils.HashBytes(fullContent)
	
	// Determine file path
	objectPath := GetObjectPathRepo(repo, hash)
	objectDir := filepath.Dir(objectPath)

	// Ensure the object directory exists (two-letter subdirectory)
	if err := utils.EnsureDirExists(objectDir); err != nil {
		return "", err
	}

	// Skip writing if the object already exists (deduplication)
	if utils.FileExists(objectPath) {
		return hash, nil
	}

	// Create temporary file to ensure atomic write
	tempFile := objectPath + ".tmp"
	file, err := os.Create(tempFile)
	if err != nil {
		return "", fmt.Errorf("failed to create blob file: %w", err)
	}
	
	// Write content and handle any errors
	if _, err := file.Write(fullContent); err != nil {
		file.Close()
		os.Remove(tempFile)
		return "", fmt.Errorf("failed to write to blob file: %w", err)
	}
	
	// Close file before renaming
	if err := file.Close(); err != nil {
		os.Remove(tempFile)
		return "", fmt.Errorf("failed to close blob file: %w", err)
	}
	
	// Atomic rename
	if err := os.Rename(tempFile, objectPath); err != nil {
		os.Remove(tempFile)
		return "", fmt.Errorf("failed to finalize blob file: %w", err)
	}

	return hash, nil
}

// GetBlobRepo retrieves a blob object by its hash using Repository context.
func GetBlobRepo(repo *core.Repository, hash string) ([]byte, error) {
	objectPath := GetObjectPathRepo(repo, hash)

	// Verify object exists
	if !utils.FileExists(objectPath) {
		return nil, fmt.Errorf("blob %s not found", hash)
	}

	// Open and read the file
	file, err := os.Open(objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open blob file: %w", err)
	}
	defer file.Close()

	// Read all content
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read blob file: %w", err)
	}

	// Extract content from the object, skipping the header
	headerEnd := bytes.IndexByte(content, '\x00')
	if headerEnd == -1 {
		return nil, fmt.Errorf("invalid blob format: missing header")
	}

	// Verify header format
	header := string(content[:headerEnd])
	if header != fmt.Sprintf("blob %d", len(content)-headerEnd-1) {
		return nil, fmt.Errorf("invalid blob header: %s", header)
	}

	// Return only the file content
	return content[headerEnd+1:], nil
}
