package packfile

import (
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
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

	// Track each object's offset for the index
	offsets := make(map[string]uint64)

	// Write each object
	for _, obj := range objects {
		// Record current position for the index
		pos, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return fmt.Errorf("failed to get file position: %w", err)
		}
		offsets[obj.Hash] = uint64(pos)

		// Write object header (type and size)
		if err := writeObjectHeader(file, obj.Type, uint64(len(obj.Data))); err != nil {
			return fmt.Errorf("failed to write object header: %w", err)
		}

		// Compress and write object data
		compressedWriter, err := zlib.NewWriterLevel(file, zlib.BestCompression)
		if err != nil {
			return fmt.Errorf("failed to create compressed writer: %w", err)
		}

		if _, err := compressedWriter.Write(obj.Data); err != nil {
			compressedWriter.Close()
			return fmt.Errorf("failed to write compressed data: %w", err)
		}

		if err := compressedWriter.Close(); err != nil {
			return fmt.Errorf("failed to close compressed writer: %w", err)
		}
	}

	// Calculate and write packfile checksum
	// This is a placeholder - in a real implementation you would calculate a checksum
	checksum := make([]byte, 20)
	if _, err := file.Write(checksum); err != nil {
		return fmt.Errorf("failed to write packfile checksum: %w", err)
	}

	// Create and write the index file
	return createPackIndex(outputPath+".idx", offsets, objects)
}

// writeObjectHeader writes the packfile object header (type + size)
func writeObjectHeader(file *os.File, objType ObjectType, size uint64) error {
	// First byte: bits 1-3 are type, bit 4-7 are size lower bits, bit 8 is continuation
	firstByte := byte(objType) << 4
	firstByte |= byte(size & 0x0F)

	// Check if we need continuation bytes
	moreBytes := size >= 16 // 16 = 2^4, we only have 4 bits in the first byte

	if moreBytes {
		firstByte |= 0x80 // Set continuation bit
	}

	if err := writeByte(file, firstByte); err != nil {
		return err
	}

	// Write continuation bytes if needed
	remaining := size >> 4
	for remaining > 0 {
		currentByte := byte(remaining & 0x7F)
		remaining >>= 7

		if remaining > 0 {
			currentByte |= 0x80 // Set continuation bit
		}

		if err := writeByte(file, currentByte); err != nil {
			return err
		}
	}

	return nil
}

// writeByte writes a single byte to the file
func writeByte(file *os.File, b byte) error {
	_, err := file.Write([]byte{b})
	return err
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
		// Write 20-byte hash
		hashBytes := []byte(hash)
		if len(hashBytes) != 20 {
			// In a real implementation, the hash would be a proper 20-byte SHA-1
			// Here we're assuming the hash string is hex-encoded, so we'd convert it
			hashBytes = make([]byte, 20)
			// Parse the hex string into bytes
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

	// Write index checksum - placeholder
	checksum := make([]byte, 20)
	if _, err := file.Write(checksum); err != nil {
		return fmt.Errorf("failed to write index checksum: %w", err)
	}

	return nil
}
