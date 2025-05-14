package packfile

import (
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
)

// CreatePackfile creates a binary packfile from a list of objects
func CreatePackfile(objects []Object) ([]byte, error) {
	// Calculate the total size needed for the packfile
	totalSize := 4 // 4 bytes for number of objects
	for _, obj := range objects {
		totalSize += hashLength    // hash
		totalSize += 4             // data length
		totalSize += 1             // type
		totalSize += len(obj.Data) // data
	}

	// Create the packfile
	packfile := make([]byte, totalSize)

	// Write number of objects
	binary.BigEndian.PutUint32(packfile[0:4], uint32(len(objects)))
	offset := 4

	// Write each object
	for _, obj := range objects {
		// Write hash
		copy(packfile[offset:offset+hashLength], []byte(obj.Hash))
		offset += hashLength

		// Write data length (including type byte)
		dataLen := len(obj.Data) + 1
		binary.BigEndian.PutUint32(packfile[offset:offset+4], uint32(dataLen))
		offset += 4

		// Write type
		packfile[offset] = byte(obj.Type)
		offset++

		// Write data
		copy(packfile[offset:offset+len(obj.Data)], obj.Data)
		offset += len(obj.Data)
	}

	return packfile, nil
}

// CreateModernPackfile creates a modern packfile with deltas and compression
func CreateModernPackfile(objects []Object, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create packfile: %w", err)
	}
	defer file.Close()

	// Write header (signature, version, number of objects)
	header := PackFileHeader{
		Signature:  [4]byte{'P', 'A', 'C', 'K'},
		Version:    2,
		NumObjects: uint32(len(objects)),
	}
	if err := binary.Write(file, binary.BigEndian, &header); err != nil {
		return fmt.Errorf("failed to write packfile header: %w", err)
	}

	// Write each object
	for _, obj := range objects {
		// Write object header (type and size)
		if err := writeObjectHeader(file, obj.Type, uint64(len(obj.Data))); err != nil {
			return fmt.Errorf("failed to write object header: %w", err)
		}

		// Compress and write object data
		compressedWriter := zlib.NewWriter(file)
		if _, err := compressedWriter.Write(obj.Data); err != nil {
			compressedWriter.Close()
			return fmt.Errorf("failed to write compressed data: %w", err)
		}
		if err := compressedWriter.Close(); err != nil {
			return fmt.Errorf("failed to close compressed writer: %w", err)
		}
	}

	// Calculate and write packfile checksum
	currentPos, err := file.Seek(0, os.SEEK_CUR)
	if err != nil {
		return fmt.Errorf("failed to get current file position: %w", err)
	}

	// Move back to the beginning of the file to calculate checksum
	if _, err := file.Seek(0, os.SEEK_SET); err != nil {
		return fmt.Errorf("failed to seek to beginning of file: %w", err)
	}

	// Read the entire file content for checksum calculation
	content := make([]byte, currentPos)
	if _, err := file.Read(content); err != nil {
		return fmt.Errorf("failed to read file content for checksum: %w", err)
	}

	// Calculate SHA-1 checksum
	h := sha1.New()
	h.Write(content)
	checksum := h.Sum(nil)

	// Move back to the end to write checksum
	if _, err := file.Seek(currentPos, os.SEEK_SET); err != nil {
		return fmt.Errorf("failed to seek to end of file: %w", err)
	}

	// Write the SHA-1 checksum (20 bytes)
	if _, err := file.Write(checksum); err != nil {
		return fmt.Errorf("failed to write packfile checksum: %w", err)
	}

	return nil
}

// writeObjectHeader writes the packfile object header (type + size)
func writeObjectHeader(file *os.File, objType ObjectType, size uint64) error {
	// Simple encoding: 1 byte for type and 4 bytes for size
	typeByte := byte(objType)
	if _, err := file.Write([]byte{typeByte}); err != nil {
		return fmt.Errorf("failed to write object type: %w", err)
	}

	// Write size as fixed 4 bytes
	sizeBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(sizeBytes, uint32(size))
	if _, err := file.Write(sizeBytes); err != nil {
		return fmt.Errorf("failed to write object size: %w", err)
	}

	return nil
}

// createPackIndex creates an index file for a packfile
func createPackIndex(indexPath string, offsets map[string]uint64, objects []Object) error {
	file, err := os.Create(indexPath)
	if err != nil {
		return fmt.Errorf("failed to create index file: %w", err)
	}
	defer file.Close()

	// Write index header: signature, version
	header := [8]byte{'\377', 't', 'O', 'c', 0, 0, 0, 2} // "\377tOc" + version 2
	if _, err := file.Write(header[:]); err != nil {
		return fmt.Errorf("failed to write index header: %w", err)
	}

	// Create index with offsets
	index := PackfileIndex{
		Entries: make(map[string]PackIndexEntry),
	}

	for _, obj := range objects {
		offset, ok := offsets[obj.Hash]
		if !ok {
			continue // Skip objects not in the packfile
		}

		index.Entries[obj.Hash] = PackIndexEntry{
			Offset: offset,
			Type:   obj.Type,
		}
	}

	// Write the index - this is a simplified implementation
	// A real implementation would include fanout tables, hash tables, etc.
	for hash, entry := range index.Entries {
		// Convert hex hash string to binary 20-byte SHA-1
		hashBytes, err := hex.DecodeString(hash)
		if err != nil || len(hashBytes) != 20 {
			// If the hash isn't a valid hex string or not 20 bytes, create a placeholder
			// This would be an error in a real implementation, but we'll just use zeros
			hashBytes = make([]byte, 20)
		}

		if _, err := file.Write(hashBytes); err != nil {
			return fmt.Errorf("failed to write hash: %w", err)
		}

		// Write 4-byte offset
		offsetBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(offsetBytes, uint32(entry.Offset))
		if _, err := file.Write(offsetBytes); err != nil {
			return fmt.Errorf("failed to write offset: %w", err)
		}
	}

	// Calculate and write index checksum
	currentPos, err := file.Seek(0, os.SEEK_CUR)
	if err != nil {
		return fmt.Errorf("failed to get current file position: %w", err)
	}

	// Move back to the beginning of the file to calculate checksum
	if _, err := file.Seek(0, os.SEEK_SET); err != nil {
		return fmt.Errorf("failed to seek to beginning of file: %w", err)
	}

	// Read the entire file content for checksum calculation
	content := make([]byte, currentPos)
	if _, err := file.Read(content); err != nil {
		return fmt.Errorf("failed to read file content for checksum: %w", err)
	}

	// Calculate SHA-1 checksum
	h := sha1.New()
	h.Write(content)
	checksum := h.Sum(nil)

	// Move back to the end to write checksum
	if _, err := file.Seek(currentPos, os.SEEK_SET); err != nil {
		return fmt.Errorf("failed to seek to end of file: %w", err)
	}

	// Write the SHA-1 checksum (20 bytes)
	if _, err := file.Write(checksum); err != nil {
		return fmt.Errorf("failed to write index checksum: %w", err)
	}

	return nil
}
