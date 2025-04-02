package packfile

import (
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// ParsePackfile parses the binary packfile and returns a slice of objects.
// Maintains backward compatibility with the original function.
func ParsePackfile(packfile []byte) ([]Object, error) {
	if len(packfile) < 4 {
		return nil, errors.New("packfile too short: missing object count")
	}

	numObjects := binary.BigEndian.Uint32(packfile[:4])
	objects := make([]Object, 0, numObjects)
	offset := 4

	for i := 0; i < int(numObjects); i++ {
		if offset+hashLength+4 > len(packfile) {
			return nil, errors.New("packfile format error: incomplete object header")
		}

		hashBytes := packfile[offset : offset+hashLength]
		hash := string(hashBytes) // Assumes hash is stored as a hex string.
		offset += hashLength

		dataLen := int(binary.BigEndian.Uint32(packfile[offset : offset+4]))
		offset += 4

		// Read the object type (1 byte)
		objType := ObjectType(0)
		if offset < len(packfile) {
			objType = ObjectType(packfile[offset])
			offset++
		} else {
			return nil, errors.New("packfile format error: missing object type")
		}

		// Adjust dataLen to account for the object type byte
		dataLen--

		if offset+dataLen > len(packfile) {
			return nil, errors.New("packfile format error: incomplete object data")
		}

		data := packfile[offset : offset+dataLen]
		offset += dataLen

		objects = append(objects, Object{
			Hash: hash,
			Type: objType,
			Data: data,
		})
	}

	return objects, nil
}

// ParseModernPackfile parses a modern packfile (with compression and deltas) and returns objects
func ParseModernPackfile(packfilePath string, useIndex bool) ([]Object, error) {
	// Open the packfile
	file, err := os.Open(packfilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open packfile: %w", err)
	}
	defer file.Close()

	// Read and verify header
	header := PackFileHeader{}
	if err := binary.Read(file, binary.BigEndian, &header); err != nil {
		return nil, fmt.Errorf("failed to read packfile header: %w", err)
	}

	if string(header.Signature[:]) != "PACK" {
		return nil, errors.New("invalid packfile: bad signature")
	}

	if header.Version != 2 {
		return nil, fmt.Errorf("unsupported packfile version: %d", header.Version)
	}

	// If index is available, use it to speed up object lookup
	var index *PackfileIndex
	if useIndex {
		indexPath := packfilePath + ".idx"
		index, err = ReadPackIndex(indexPath)
		if err != nil {
			// If index can't be loaded, continue without it
			index = nil
		}
	}

	objects := make([]Object, 0, header.NumObjects)
	objectMap := make(map[string]*Object) // Cache for already processed objects

	// Without an index, we have to read the packfile sequentially
	if index == nil {
		// First pass: read all non-delta objects
		_, err = file.Seek(12, 0) // Skip header
		if err != nil {
			return nil, fmt.Errorf("failed to seek in packfile: %w", err)
		}

		// Track deltas for second pass
		deltaObjects := make([]*DeltaObject, 0)

		// First pass: read objects and collect deltas
		for i := uint32(0); i < header.NumObjects; i++ {
			_, _ = file.Seek(0, io.SeekCurrent) // Get current position but don't need the value
			obj, isDelta, baseHash, err := readPackObject(file)
			if err != nil {
				return nil, fmt.Errorf("failed to read object %d: %w", i, err)
			}

			if isDelta {
				// Store delta for second pass
				deltaObjects = append(deltaObjects, &DeltaObject{
					BaseHash: baseHash,
					Hash:     "", // Will be calculated after applying delta
					Delta:    obj.Data,
					Type:     obj.Type,
				})
			} else {
				// Add regular object to our collection
				objects = append(objects, *obj)
				objectMap[obj.Hash] = obj
			}
		}

		// Second pass: resolve deltas
		for _, delta := range deltaObjects {
			baseObj, ok := objectMap[delta.BaseHash]
			if !ok {
				return nil, fmt.Errorf("delta base not found: %s", delta.BaseHash)
			}

			// Apply delta to get the full object
			resultData, err := applyDelta(baseObj.Data, delta.Delta)
			if err != nil {
				return nil, fmt.Errorf("failed to apply delta: %w", err)
			}

			// Create the reconstructed object
			resultObj := Object{
				Type: baseObj.Type, // Inherit type from base
				Data: resultData,
				Hash: calculateObjectHash(baseObj.Type, resultData),
			}

			objects = append(objects, resultObj)
			objectMap[resultObj.Hash] = &resultObj
		}
	} else {
		// If we have an index, we can extract objects in any order
		for hash, entry := range index.Entries {
			// Seek to the object's position in the packfile
			_, err = file.Seek(int64(entry.Offset), 0)
			if err != nil {
				return nil, fmt.Errorf("failed to seek to object %s: %w", hash, err)
			}

			// Read the object
			obj, isDelta, _, err := readPackObject(file) // Ignore baseHash as it's not needed here
			if err != nil {
				return nil, fmt.Errorf("failed to read object %s: %w", hash, err)
			}

			obj.Hash = hash
			if !isDelta {
				objects = append(objects, *obj)
				objectMap[hash] = obj
			}
		}

		// Resolve deltas (some objects may reference others)
		// This is a simplified approach; a real implementation would
		// handle delta chains (deltas based on deltas) properly
		var unresolvedDeltas []*DeltaObject
		for hash, entry := range index.Entries {
			_, err = file.Seek(int64(entry.Offset), 0)
			if err != nil {
				return nil, fmt.Errorf("failed to seek to object %s: %w", hash, err)
			}

			obj, isDelta, baseHash, err := readPackObject(file)
			if err != nil {
				return nil, fmt.Errorf("failed to read delta object %s: %w", hash, err)
			}

			if isDelta {
				unresolvedDeltas = append(unresolvedDeltas, &DeltaObject{
					BaseHash: baseHash,
					Hash:     hash,
					Delta:    obj.Data,
					Type:     obj.Type,
				})
			}
		}

		// Resolve deltas
		for _, delta := range unresolvedDeltas {
			baseObj, ok := objectMap[delta.BaseHash]
			if !ok {
				return nil, fmt.Errorf("delta base not found: %s", delta.BaseHash)
			}

			resultData, err := applyDelta(baseObj.Data, delta.Delta)
			if err != nil {
				return nil, fmt.Errorf("failed to apply delta: %w", err)
			}

			resultObj := Object{
				Type: baseObj.Type,
				Data: resultData,
				Hash: delta.Hash,
			}

			objects = append(objects, resultObj)
			objectMap[resultObj.Hash] = &resultObj
		}
	}

	return objects, nil
}

// readPackObject reads a single object from a packfile
func readPackObject(file *os.File) (*Object, bool, string, error) {
	// Read object header
	headerByte, err := readByte(file)
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to read object header: %w", err)
	}

	// Extract object type and size from the header
	objectType := ObjectType((headerByte >> 4) & 0x7)
	size := uint64(headerByte & 0x0F)
	shift := uint(4)

	// Parse variable-length size
	for headerByte&0x80 != 0 {
		headerByte, err = readByte(file)
		if err != nil {
			return nil, false, "", fmt.Errorf("failed to read size continuation: %w", err)
		}
		size |= uint64(headerByte&0x7F) << shift
		shift += 7
	}

	// Check if this is a delta object
	isDelta := false
	var baseHash string

	if objectType == OBJ_DELTA {
		isDelta = true

		// Read the base object hash (20 bytes)
		baseHashBytes := make([]byte, 20)
		_, err = io.ReadFull(file, baseHashBytes)
		if err != nil {
			return nil, false, "", fmt.Errorf("failed to read base hash: %w", err)
		}
		baseHash = fmt.Sprintf("%x", baseHashBytes)
	}

	// Read and decompress the object data
	zlibReader, err := zlib.NewReader(file)
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to create zlib reader: %w", err)
	}
	defer zlibReader.Close()

	data := make([]byte, size)
	_, err = io.ReadFull(zlibReader, data)
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to read object data: %w", err)
	}

	return &Object{
		Type: objectType,
		Data: data,
	}, isDelta, baseHash, nil
}

// readByte reads a single byte from the file
func readByte(file *os.File) (byte, error) {
	buf := make([]byte, 1)
	_, err := file.Read(buf)
	if err != nil {
		return 0, err
	}
	return buf[0], nil
}

// calculateObjectHash calculates a hash for an object
func calculateObjectHash(objType ObjectType, data []byte) string {
	// This is a placeholder - implement the actual hashing logic for your system
	return "generated-hash" // Replace with actual hash calculation
}
