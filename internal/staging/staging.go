package staging

import (
	"compress/zlib"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/NahomAnteneh/vec/utils"
)

// Constants for the subdirectory names within .vec
const (
	objectsDirName = "objects"
	indexFileName  = "index"
)

// StagingArea represents the staging area (index).
type StagingArea struct {
	IndexFile string            // Path to the index file.
	repoRoot  string            // Path to repository root.
	entries   map[string]string // Map of file paths to hashes.
}

// NewStagingArea creates a new StagingArea instance AND reads the index.
func NewStagingArea(repoRoot string) (*StagingArea, error) {
	indexFile := filepath.Join(repoRoot, utils.VecDirName, indexFileName)
	sa := &StagingArea{
		IndexFile: indexFile,
		repoRoot:  repoRoot,
		entries:   make(map[string]string),
	}

	if err := sa.readIndex(); err != nil { // Read the index here!
		return nil, err
	}
	return sa, nil
}

// GetEntries allows access to the entries for status and other commands.
func (sa *StagingArea) GetEntries() map[string]string {
	return sa.entries
}

// AddFile adds a file to the staging area.
func (sa *StagingArea) AddFile(relativePath string) error {
	absoluteFilePath := filepath.Join(sa.repoRoot, relativePath)
	fileInfo, err := os.Stat(absoluteFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", relativePath)
		}
		return fmt.Errorf("failed to stat file '%s': %w", relativePath, err)
	}

	if fileInfo.IsDir() {
		return sa.addDirectory(relativePath)
	}

	file, err := os.Open(absoluteFilePath)
	if err != nil {
		return fmt.Errorf("failed to open file '%s': %w", relativePath, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("failed to calculate hash for '%s': %w", relativePath, err)
	}
	hashString := fmt.Sprintf("%x", hash.Sum(nil))

	// Check if staged and up-to-date
	if existingHash, ok := sa.entries[relativePath]; ok {
		if existingHash == hashString {
			fmt.Printf("up to date: %s\n", relativePath)
			return nil // Already staged and unchanged.
		}
	}

	// --- Object Storage Changes ---
	objectPath, err := getObjectPath(sa.repoRoot, hashString)
	if err != nil {
		return err
	}
	if err := compressAndCopyFile(absoluteFilePath, objectPath); err != nil {
		return err
	}

	sa.entries[relativePath] = hashString
	fmt.Printf("added: %s\n", relativePath)
	return nil // writeIndex will be handled in add.go
}

// getObjectPath calculates the path to the object file within .vec/objects.
func getObjectPath(repoRoot, hash string) (string, error) {
	if len(hash) < 2 {
		return "", fmt.Errorf("invalid hash: %s", hash) // Should never happen with SHA256.
	}
	objectDir := filepath.Join(repoRoot, utils.VecDirName, objectsDirName, hash[:2])
	objectPath := filepath.Join(objectDir, hash[2:])
	return objectPath, nil
}

// compressAndCopyFile compresses the file content and copies it to the destination.
func compressAndCopyFile(src, dst string) error {
	//Create the directory if it doesn't exist
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0755); err != nil { // Use 0755 permission
		return fmt.Errorf("failed to create object directory '%s' : %w", dir, err)
	}
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file '%s': %w", src, err)
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file '%s': %w", dst, err)
	}
	defer destFile.Close()

	// Use zlib for compression.
	zlibWriter := zlib.NewWriter(destFile)
	defer zlibWriter.Close() // Important to close the zlib writer to flush data.

	if _, err := io.Copy(zlibWriter, sourceFile); err != nil {
		return fmt.Errorf("failed to compress and copy file content from '%s' to '%s': %w", src, dst, err)
	}

	return nil
}

// addDirectory adds all files in a directory recursively.
func (sa *StagingArea) addDirectory(dirPath string) error {
	absoluteDirPath := filepath.Join(sa.repoRoot, dirPath)
	err := filepath.Walk(absoluteDirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("prevent panic by handling failure accessing a path %q: %v", dirPath, err)
		}
		// Skip the .vec directory
		if info.IsDir() && info.Name() == utils.VecDirName { // Use const
			return filepath.SkipDir
		}
		if !info.IsDir() {
			relPath, err := filepath.Rel(sa.repoRoot, path)
			if err != nil {
				return fmt.Errorf("could not get relative path: %w", err)
			}
			err = sa.AddFile(relPath) // Use AddFile for consistency.
			if err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// WriteIndex writes the staging area to the index file (byte format).
func (sa *StagingArea) WriteIndex(repoRoot string) error {
	indexFile := filepath.Join(repoRoot, utils.VecDirName, indexFileName)
	var data []byte

	for filePath, hash := range sa.entries {
		// Write file path length.
		filePathBytes := []byte(filePath)
		filePathLength := uint32(len(filePathBytes))
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, filePathLength)
		data = append(data, buf...)

		// Write file path.
		data = append(data, filePathBytes...)

		// Write hash length.
		hashBytes := []byte(hash)
		hashLength := uint32(len(hashBytes))
		buf = make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, hashLength)
		data = append(data, buf...)

		// Write hash.
		data = append(data, hashBytes...)
	}

	if err := os.WriteFile(indexFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}
	return nil
}

// readIndex reads the index file (byte format).
func (sa *StagingArea) readIndex() error {
	indexFile := filepath.Join(sa.repoRoot, utils.VecDirName, indexFileName)
	if _, err := os.Stat(indexFile); os.IsNotExist(err) {
		return nil // Index file doesn't exist yet, that's OK.
	}

	content, err := os.ReadFile(indexFile)
	if err != nil {
		return fmt.Errorf("failed to read index file: %w", err)
	}

	offset := 0
	for offset < len(content) {
		// Read file path length.
		if offset+4 > len(content) {
			return fmt.Errorf("invalid index file format (file path length)")
		}
		filePathLength := binary.LittleEndian.Uint32(content[offset : offset+4])
		offset += 4

		// Read file path.
		if offset+int(filePathLength) > len(content) {
			return fmt.Errorf("invalid index file format (file path)")
		}
		filePath := string(content[offset : offset+int(filePathLength)])
		offset += int(filePathLength)

		// Read hash length.
		if offset+4 > len(content) {
			return fmt.Errorf("invalid index file format (hash length)")
		}
		hashLength := binary.LittleEndian.Uint32(content[offset : offset+4])
		offset += 4

		// Read hash.
		if offset+int(hashLength) > len(content) {
			return fmt.Errorf("invalid index file format (hash)")
		}
		hash := string(content[offset : offset+int(hashLength)])
		offset += int(hashLength)

		sa.entries[filePath] = hash
	}

	return nil
}
