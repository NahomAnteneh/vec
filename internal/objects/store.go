package objects

// import (
// 	"bytes"
// 	"compress/zlib"
// 	"crypto/sha256"
// 	"encoding/binary"
// 	"fmt"
// 	"io"
// 	"os"
// 	"path/filepath"

// 	"github.com/NahomAnteneh/vec/utils"
// )

// type ObjectType string

// const (
// 	TypeBlob       ObjectType = "blob"
// 	TypeTree       ObjectType = "tree"
// 	TypeCommit     ObjectType = "commit"
// 	objectsDirName string     = "objects"
// )

// type ObjectHeader struct {
// 	Type ObjectType
// 	Size int64
// }

// // --- Functions Related to Trees ---

// // GetObjectPath calculates the path to the object file within .vec/objects.
// func GetObjectPath(repoRoot, hash string) (string, error) {
// 	if len(hash) < 2 {
// 		return "", fmt.Errorf("invalid hash: %s", hash) // Should never happen with SHA256.
// 	}
// 	objectDir := filepath.Join(repoRoot, utils.VecDirName, objectsDirName, hash[:2])
// 	objectPath := filepath.Join(objectDir, hash[2:])
// 	return objectPath, nil
// }

// // StoreObject stores a byte slice as an object in .vec/objects, handling compression and directory creation.
// func StoreObject(repoRoot string, objectType ObjectType, data []byte) (string, error) {
// 	// Create header
// 	header := ObjectHeader{
// 		Type: objectType,
// 		Size: int64(len(data)),
// 	}

// 	// Serialize header
// 	var buf bytes.Buffer
// 	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(string(header.Type)))); err != nil {
// 		return "", fmt.Errorf("failed to write type length: %w", err)
// 	}
// 	if _, err := buf.WriteString(string(header.Type)); err != nil {
// 		return "", fmt.Errorf("failed to write type: %w", err)
// 	}
// 	if err := binary.Write(&buf, binary.LittleEndian, header.Size); err != nil {
// 		return "", fmt.Errorf("failed to write size: %w", err)
// 	}

// 	// Write actual data after header
// 	if _, err := buf.Write(data); err != nil {
// 		return "", fmt.Errorf("failed to write data: %w", err)
// 	}

// 	// Calculate the SHA-256 hash of the complete object (header + data)
// 	hash := sha256.Sum256(buf.Bytes())
// 	hashString := fmt.Sprintf("%x", hash)

// 	objectPath, err := GetObjectPath(repoRoot, hashString)
// 	if err != nil {
// 		return "", err
// 	}

// 	// Create the object directory if it doesn't exist
// 	objectDir := filepath.Dir(objectPath)
// 	if err := os.MkdirAll(objectDir, 0755); err != nil {
// 		return "", fmt.Errorf("failed to create object directory '%s': %w", objectDir, err)
// 	}

// 	// Compress the complete object
// 	var compressedData bytes.Buffer
// 	zlibWriter := zlib.NewWriter(&compressedData)
// 	if _, err := zlibWriter.Write(buf.Bytes()); err != nil {
// 		return "", fmt.Errorf("failed to compress object data: %w", err)
// 	}
// 	if err := zlibWriter.Close(); err != nil {
// 		return "", fmt.Errorf("failed to close zlib writer: %w", err)
// 	}

// 	// Write the compressed data to the object file
// 	if err := os.WriteFile(objectPath, compressedData.Bytes(), 0644); err != nil {
// 		return "", fmt.Errorf("failed to write object file '%s': %w", objectPath, err)
// 	}

// 	return hashString, nil
// }

// func CalculateObjectHash(objectType ObjectType, data []byte) (string, error) {
// 	// Create header
// 	header := ObjectHeader{
// 		Type: objectType,
// 		Size: int64(len(data)),
// 	}

// 	// Serialize header
// 	var buf bytes.Buffer
// 	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(string(header.Type)))); err != nil {
// 		return "", fmt.Errorf("failed to write type length: %w", err)
// 	}
// 	if _, err := buf.WriteString(string(header.Type)); err != nil {
// 		return "", fmt.Errorf("failed to write type: %w", err)
// 	}
// 	if err := binary.Write(&buf, binary.LittleEndian, header.Size); err != nil {
// 		return "", fmt.Errorf("failed to write size: %w", err)
// 	}

// 	// Write actual data after header
// 	if _, err := buf.Write(data); err != nil {
// 		return "", fmt.Errorf("failed to write data: %w", err)
// 	}

// 	// Calculate the SHA-256 hash of the complete object (header + data)
// 	hash := sha256.Sum256(buf.Bytes())
// 	hashString := fmt.Sprintf("%x", hash)

// 	return hashString, nil
// }

// // ReadObjectHeader reads just the header of an object without loading its full content
// func ReadObjectHeader(repoRoot, hash string) (*ObjectHeader, error) {
// 	data, err := RetrieveObject(repoRoot, hash)
// 	if err != nil {
// 		return nil, err
// 	}

// 	buf := bytes.NewReader(data)

// 	// Read type length
// 	var typeLen uint32
// 	if err := binary.Read(buf, binary.LittleEndian, &typeLen); err != nil {
// 		return nil, fmt.Errorf("failed to read type length: %w", err)
// 	}

// 	// Read type
// 	typeBytes := make([]byte, typeLen)
// 	if _, err := buf.Read(typeBytes); err != nil {
// 		return nil, fmt.Errorf("failed to read type: %w", err)
// 	}

// 	// Read size
// 	var size int64
// 	if err := binary.Read(buf, binary.LittleEndian, &size); err != nil {
// 		return nil, fmt.Errorf("failed to read size: %w", err)
// 	}

// 	return &ObjectHeader{
// 		Type: ObjectType(typeBytes),
// 		Size: size,
// 	}, nil
// }

// // ReadObjectData reads the actual data of an object (excluding header)
// func ReadObjectData(repoRoot, hash string) ([]byte, error) {
// 	data, err := RetrieveObject(repoRoot, hash)
// 	if err != nil {
// 		return nil, err
// 	}

// 	buf := bytes.NewReader(data)

// 	// Skip type length
// 	var typeLen uint32
// 	if err := binary.Read(buf, binary.LittleEndian, &typeLen); err != nil {
// 		return nil, fmt.Errorf("failed to read type length: %w", err)
// 	}

// 	// Skip type
// 	if _, err := buf.Seek(int64(typeLen), io.SeekCurrent); err != nil {
// 		return nil, fmt.Errorf("failed to skip type: %w", err)
// 	}

// 	// Skip size
// 	if _, err := buf.Seek(8, io.SeekCurrent); err != nil { // int64 = 8 bytes
// 		return nil, fmt.Errorf("failed to skip size: %w", err)
// 	}

// 	// Read remaining data
// 	return io.ReadAll(buf)
// }

// // RetrieveObject retrieves and decompresses an object from .vec/objects.
// func RetrieveObject(repoRoot, hash string) ([]byte, error) {
// 	objectPath, err := GetObjectPath(repoRoot, hash)
// 	if err != nil {
// 		return nil, err
// 	}

// 	compressedData, err := os.ReadFile(objectPath)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to read object file '%s': %w", objectPath, err)
// 	}

// 	// Decompress the data.
// 	zlibReader, err := zlib.NewReader(bytes.NewReader(compressedData))
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to create zlib reader: %w", err)
// 	}
// 	defer zlibReader.Close()

// 	decompressedData, err := io.ReadAll(zlibReader)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to decompress object data: %w", err)
// 	}

// 	return decompressedData, nil
// }
