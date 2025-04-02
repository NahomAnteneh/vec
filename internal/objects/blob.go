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

// CreateBlob creates a new blob object, including the header (legacy function).
func CreateBlob(repoRoot string, content []byte) (string, error) {
	repo := core.NewRepository(repoRoot)
	return CreateBlobRepo(repo, content)
}

// CreateBlobRepo creates a new blob object using Repository context.
func CreateBlobRepo(repo *core.Repository, content []byte) (string, error) {
	header := fmt.Sprintf("blob %d\x00", len(content))
	var buf bytes.Buffer
	buf.WriteString(header)
	buf.Write(content)

	hash := utils.HashBytes("blob", content) // Hash includes header
	objectPath := GetObjectPathRepo(repo, hash)
	objectDir := filepath.Dir(objectPath)

	// Ensure the object directory exists.
	if err := utils.EnsureDirExists(objectDir); err != nil {
		return "", err
	}

	// Create and write to the blob file.
	file, err := os.Create(objectPath)
	if err != nil {
		return "", fmt.Errorf("failed to create blob file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, &buf); err != nil { // Write the combined header + content
		return "", fmt.Errorf("failed to write to blob file: %w", err)
	}

	return hash, nil
}

// GetBlob retrieves a blob object by its hash (legacy function).
func GetBlob(repoRoot string, hash string) ([]byte, error) {
	repo := core.NewRepository(repoRoot)
	return GetBlobRepo(repo, hash)
}

// GetBlobRepo retrieves a blob object by its hash using Repository context.
func GetBlobRepo(repo *core.Repository, hash string) ([]byte, error) {
	objectPath := GetObjectPathRepo(repo, hash)

	file, err := os.Open(objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open blob file: %w", err)
	}
	defer file.Close()

	//Read All content of the file
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read blob file: %w", err)
	}

	// Separate header and content
	headerEnd := bytes.IndexByte(content, '\x00')
	if headerEnd == -1 {
		return nil, fmt.Errorf("invalid blob format: missing header")
	}

	// Return only the file content
	return content[headerEnd+1:], nil
}
