package packfile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// ReadPackIndex reads a packfile index from the given path
func ReadPackIndex(indexPath string) (*PackfileIndex, error) {
	file, err := os.Open(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open index file: %w", err)
	}
	defer file.Close()

	// Read and verify header
	header := make([]byte, 8)
	if _, err := io.ReadFull(file, header); err != nil {
		return nil, fmt.Errorf("failed to read index header: %w", err)
	}

	// Check signature
	if string(header[:4]) != "\377tOc" {
		return nil, errors.New("invalid index file: bad signature")
	}

	// Check version
	version := binary.BigEndian.Uint32(header[4:])
	if version != 2 {
		return nil, fmt.Errorf("unsupported index version: %d", version)
	}

	// Read fanout table (256 entries of 4 bytes each)
	fanout := make([]uint32, 256)
	for i := 0; i < 256; i++ {
		if err := binary.Read(file, binary.BigEndian, &fanout[i]); err != nil {
			return nil, fmt.Errorf("failed to read fanout table: %w", err)
		}
	}

	// Total number of objects is the last entry in the fanout table
	numObjects := fanout[255]

	// Read object entries
	index := &PackfileIndex{
		Entries: make(map[string]PackIndexEntry, numObjects),
	}

	// Skip past the SHA-1 table to the offset table
	// SHA-1 table is numObjects entries of 20 bytes each
	_, err = file.Seek(int64(numObjects*20), io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("failed to seek to offset table: %w", err)
	}

	// Create a buffer for reading the SHA-1 values
	sha1Buffer := make([]byte, 20)

	// Return to the beginning of the SHA-1 table
	_, err = file.Seek(1032, io.SeekStart) // 8 (header) + 256*4 (fanout table)
	if err != nil {
		return nil, fmt.Errorf("failed to seek to SHA-1 table: %w", err)
	}

	// Read object entries
	for i := uint32(0); i < numObjects; i++ {
		// Read object SHA-1
		if _, err := io.ReadFull(file, sha1Buffer); err != nil {
			return nil, fmt.Errorf("failed to read object SHA-1: %w", err)
		}
		sha1Hex := fmt.Sprintf("%x", sha1Buffer)

		// Save current position to return to after reading the offset
		currentPos, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, fmt.Errorf("failed to get current position: %w", err)
		}

		// Seek to the offset table for this object
		_, err = file.Seek(1032+int64(numObjects*20)+int64(i*4), io.SeekStart)
		if err != nil {
			return nil, fmt.Errorf("failed to seek to offset: %w", err)
		}

		// Read object offset
		var offset uint32
		if err := binary.Read(file, binary.BigEndian, &offset); err != nil {
			return nil, fmt.Errorf("failed to read object offset: %w", err)
		}

		// Check if it's a large offset (MSB set)
		isLargeOffset := (offset & 0x80000000) != 0
		var finalOffset uint64
		if isLargeOffset {
			// The offset is actually an index into the large offset table
			largeOffsetIndex := offset & 0x7FFFFFFF

			// Seek to the large offset table
			_, err = file.Seek(1032+int64(numObjects*20)+int64(numObjects*4)+int64(largeOffsetIndex*8), io.SeekStart)
			if err != nil {
				return nil, fmt.Errorf("failed to seek to large offset: %w", err)
			}

			// Read the large offset
			if err := binary.Read(file, binary.BigEndian, &finalOffset); err != nil {
				return nil, fmt.Errorf("failed to read large offset: %w", err)
			}
		} else {
			finalOffset = uint64(offset)
		}

		// For simplicity, set all objects as blob type
		// In a real system, you'd determine the type from the packfile
		index.Entries[sha1Hex] = PackIndexEntry{
			Offset: finalOffset,
			Type:   OBJ_BLOB,
		}

		// Return to reading SHA-1 values
		_, err = file.Seek(currentPos, io.SeekStart)
		if err != nil {
			return nil, fmt.Errorf("failed to return to SHA-1 table: %w", err)
		}
	}

	return index, nil
}

// WritePackIndex writes a packfile index to the given path
func WritePackIndex(index *PackfileIndex, indexPath string) error {
	file, err := os.Create(indexPath)
	if err != nil {
		return fmt.Errorf("failed to create index file: %w", err)
	}
	defer file.Close()

	// Write header
	header := [8]byte{'\377', 't', 'O', 'c', 0, 0, 0, 2} // "\377tOc" + version 2
	if _, err := file.Write(header[:]); err != nil {
		return fmt.Errorf("failed to write index header: %w", err)
	}

	// Prepare data for fanout table, SHA-1 table, and offset table
	numObjects := uint32(len(index.Entries))
	fanout := make([]uint32, 256)
	sha1s := make([]string, 0, numObjects)

	// Collect all SHA-1 values
	for sha1Hex := range index.Entries {
		sha1s = append(sha1s, sha1Hex)
	}

	// Sort SHA-1 values for the fanout table
	// For simplicity, we're not actually sorting here
	// In a real implementation, you'd sort the SHA-1 values

	// Build fanout table
	for range sha1s {
		// In a real implementation, you'd increment the counter for the first byte
		// Here we just set all counters to the total number of objects
		firstByte := 0 // should be byte value of first byte of sha1
		for i := firstByte; i < 256; i++ {
			fanout[i]++
		}
	}

	// Write fanout table
	for i := 0; i < 256; i++ {
		if err := binary.Write(file, binary.BigEndian, fanout[i]); err != nil {
			return fmt.Errorf("failed to write fanout table entry: %w", err)
		}
	}

	// Write SHA-1 table
	for _, sha1Hex := range sha1s {
		// Convert hex string to bytes and write
		sha1Bytes, err := hexToBytes(sha1Hex)
		if err != nil {
			return fmt.Errorf("invalid SHA-1 hex: %s: %w", sha1Hex, err)
		}
		if _, err := file.Write(sha1Bytes); err != nil {
			return fmt.Errorf("failed to write SHA-1: %w", err)
		}
	}

	// Write CRC32 table (all zeros for simplicity)
	crc32 := make([]byte, numObjects*4)
	if _, err := file.Write(crc32); err != nil {
		return fmt.Errorf("failed to write CRC32 table: %w", err)
	}

	// Write offset table
	for _, sha1Hex := range sha1s {
		entry := index.Entries[sha1Hex]

		// Check if we need a large offset
		if entry.Offset < (1 << 31) {
			// Regular offset
			offset := uint32(entry.Offset)
			if err := binary.Write(file, binary.BigEndian, offset); err != nil {
				return fmt.Errorf("failed to write offset: %w", err)
			}
		} else {
			// Large offset (not implemented for simplicity)
			return errors.New("large offsets not implemented")
		}
	}

	// Write pack checksum (all zeros for simplicity)
	packChecksum := make([]byte, 20)
	if _, err := file.Write(packChecksum); err != nil {
		return fmt.Errorf("failed to write pack checksum: %w", err)
	}

	// Write index checksum (all zeros for simplicity)
	indexChecksum := make([]byte, 20)
	if _, err := file.Write(indexChecksum); err != nil {
		return fmt.Errorf("failed to write index checksum: %w", err)
	}

	return nil
}

// hexToBytes converts a hex string to bytes
func hexToBytes(hex string) ([]byte, error) {
	if len(hex)%2 != 0 {
		return nil, errors.New("hex string must have even length")
	}

	bytes := make([]byte, len(hex)/2)
	for i := 0; i < len(hex); i += 2 {
		var value byte
		for j := 0; j < 2; j++ {
			value <<= 4
			c := hex[i+j]
			switch {
			case c >= '0' && c <= '9':
				value |= c - '0'
			case c >= 'a' && c <= 'f':
				value |= c - 'a' + 10
			case c >= 'A' && c <= 'F':
				value |= c - 'A' + 10
			default:
				return nil, fmt.Errorf("invalid hex character: %c", c)
			}
		}
		bytes[i/2] = value
	}

	return bytes, nil
}

// VerifyPackIndex checks that all entries in the index are valid and present in the packfile
func VerifyPackIndex(indexPath, packfilePath string) error {
	// Read the index
	index, err := ReadPackIndex(indexPath)
	if err != nil {
		return fmt.Errorf("failed to read index: %w", err)
	}

	// Open the packfile
	packfile, err := os.Open(packfilePath)
	if err != nil {
		return fmt.Errorf("failed to open packfile: %w", err)
	}
	defer packfile.Close()

	// For each entry in the index, verify it points to a valid object in the packfile
	for sha1, entry := range index.Entries {
		// Seek to the object position
		_, err = packfile.Seek(int64(entry.Offset), io.SeekStart)
		if err != nil {
			return fmt.Errorf("failed to seek to object %s: %w", sha1, err)
		}

		// Read just the object header to verify it exists
		// This is a simplified check; a real implementation would parse and verify the object
		headerByte := make([]byte, 1)
		_, err = packfile.Read(headerByte)
		if err != nil {
			return fmt.Errorf("failed to read object header for %s: %w", sha1, err)
		}
	}

	return nil
}
