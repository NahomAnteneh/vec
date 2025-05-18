package packfile

import (
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"crypto/sha1"
	"encoding/hex"
	"sort"
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

		// Object data follows the type byte
		if offset+dataLen-1 > len(packfile) {
			return nil, fmt.Errorf("packfile format error: incomplete object data (expected %d bytes, have %d)", 
				dataLen-1, len(packfile)-offset)
		}

		// The data length includes the type byte, so we need dataLen-1 bytes for actual data
		data := packfile[offset : offset+dataLen-1]
		offset += dataLen - 1

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

	// Store object locations and information
	type objectInfo struct {
		offset   int64      // File offset of object
		isDelta  bool       // Whether this is a delta object
		baseHash string     // Base object hash (for REF_DELTA)
		basePos  int64      // Base object position (for OFS_DELTA)
	}
	
	// Store information about each object
	objectInfos := make(map[uint64]objectInfo, header.NumObjects)
	objectsByOffset := make(map[int64]*Object) // For resolving offset deltas
	
	// First pass: scan the packfile and collect object metadata
	_, err = file.Seek(12, 0) // Skip header (12 bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to seek in packfile: %w", err)
	}

	// Track positions of all objects for delta resolution
	for i := uint32(0); i < header.NumObjects; i++ {
		// Record current position
		pos, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, fmt.Errorf("failed to get position for object %d: %w", i, err)
		}
		
		// Read object type and size from header
		headerByte, err := readByte(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read header for object %d: %w", i, err)
		}
		
		// Extract type and size
		objType := ObjectType((headerByte >> 4) & 0x7)
		size := uint64(headerByte & 0x0F)
		shift := uint(4)
		
		// Read variable-length size
		for headerByte&0x80 != 0 {
			headerByte, err = readByte(file)
			if err != nil {
				return nil, fmt.Errorf("failed to read size for object %d: %w", i, err)
			}
			size |= uint64(headerByte&0x7F) << shift
			shift += 7
		}
		
		// Determine if this is a delta and record base information
		info := objectInfo{offset: pos}
		
		if objType == OBJ_REF_DELTA {
			info.isDelta = true
			
			// Read base hash for reference delta
			baseHashBytes := make([]byte, 20)
			if _, err := io.ReadFull(file, baseHashBytes); err != nil {
				return nil, fmt.Errorf("failed to read base hash for object %d: %w", i, err)
			}
			info.baseHash = fmt.Sprintf("%x", baseHashBytes)
		} else if objType == OBJ_OFS_DELTA {
			info.isDelta = true
			
			// Read offset delta encoding
			var baseOffset uint64
			var b byte = 0x80
			var j uint = 0
			
			for (b & 0x80) != 0 {
				b, err = readByte(file)
				if err != nil {
					return nil, fmt.Errorf("failed to read delta offset for object %d: %w", i, err)
				}
				
				baseOffset |= uint64(b&0x7F) << (j * 7)
				j++
				
				// Break if no more bytes (MSB not set)
				if (b & 0x80) == 0 {
					break
				}
			}
			
			// Calculate base position
			info.basePos = pos - int64(baseOffset)
		}
		
		// Record this object
		objectInfos[uint64(i)] = info
		
		// Skip compressed data - we'll read it in the second pass
		// Create zlib reader
		zlibReader, err := zlib.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("failed to create zlib reader for object %d: %w", i, err)
		}
		
		// Skip the compressed data
		_, err = io.Copy(io.Discard, zlibReader)
		if err != nil {
			zlibReader.Close()
			return nil, fmt.Errorf("failed to skip data for object %d: %w", i, err)
		}
		zlibReader.Close()
	}

	// Second pass: read non-delta objects
	var objects []Object
	var deltaObjects []struct {
		objIndex uint64
		info     objectInfo
	}
	
	// Process non-delta objects first
	for objIndex, info := range objectInfos {
		if !info.isDelta {
			// Seek to the object
			_, err = file.Seek(info.offset, 0)
			if err != nil {
				return nil, fmt.Errorf("failed to seek to object at offset %d: %w", info.offset, err)
			}
			
			// Read the object
			obj, _, _, err := readPackObject(file)
			if err != nil {
				return nil, fmt.Errorf("failed to read object at offset %d: %w", info.offset, err)
			}
			
			// Calculate hash for non-delta objects
			obj.Hash = calculateObjectHash(obj.Type, obj.Data)
			
			// Store the object
			objects = append(objects, *obj)
			objectsByOffset[info.offset] = obj
		} else {
			// Collect delta objects for third pass
			deltaObjects = append(deltaObjects, struct {
				objIndex uint64
				info     objectInfo
			}{objIndex, info})
		}
	}
	
	// Sort delta objects to ensure base objects are processed before dependent deltas
	sort.Slice(deltaObjects, func(i, j int) bool {
		infoI, infoJ := deltaObjects[i].info, deltaObjects[j].info
		
		// Process REF_DELTA before OFS_DELTA
		if infoI.baseHash != "" && infoJ.baseHash == "" {
			return true
		}
		if infoI.baseHash == "" && infoJ.baseHash != "" {
			return false
		}
		
		// For OFS_DELTA, process in order of base offset (lowest first)
		if infoI.baseHash == "" {
			return infoI.basePos < infoJ.basePos
		}
		
		// Default to lower index first
		return deltaObjects[i].objIndex < deltaObjects[j].objIndex
	})
	
	// Third pass: resolve and apply deltas
	for _, d := range deltaObjects {
		info := d.info
		
		// Seek to the object
		_, err = file.Seek(info.offset, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to seek to delta at offset %d: %w", info.offset, err)
		}
		
		// Read the delta object
		deltaObj, isDelta, baseHash, err := readPackObject(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read delta at offset %d: %w", info.offset, err)
		}
		
		if !isDelta {
			return nil, fmt.Errorf("expected delta at offset %d", info.offset)
		}
		
		// Find the base object
		var baseObj *Object
		
		if info.baseHash != "" {
			// REF_DELTA - find the base by hash
			for i := range objects {
				if objects[i].Hash == info.baseHash {
					baseObj = &objects[i]
					break
				}
			}
		} else {
			// OFS_DELTA - find the base by offset
			baseObj = objectsByOffset[info.basePos]
		}
		
		if baseObj == nil {
			return nil, fmt.Errorf("base object not found for delta at offset %d", info.offset)
		}
		
		// Apply the delta
		resultData, err := applyDelta(baseObj.Data, deltaObj.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to apply delta at offset %d: %w", info.offset, err)
		}
		
		// Create the resulting object
		resultObj := Object{
			Type:     baseObj.Type, // Inherit type from base
			Data:     resultData,
			IsDelta:  false,        // Once resolved, it's no longer a delta
		}
		
		// Calculate the hash
		resultObj.Hash = calculateObjectHash(resultObj.Type, resultObj.Data)
		
		// Store the resolved object
		objects = append(objects, resultObj)
		objectsByOffset[info.offset] = &objects[len(objects)-1]
	}

	return objects, nil
}

// readPackObject reads a single object from a packfile
func readPackObject(file *os.File) (*Object, bool, string, error) {
	// Start position for this object
	startPos, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to get file position: %w", err)
	}
	
	// Read the first byte of the header
	headerByte, err := readByte(file)
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to read object header: %w", err)
	}

	// Extract object type (bits 4-6) and the first 4 bits of size from the header
	objectType := ObjectType((headerByte >> 4) & 0x7)
	size := uint64(headerByte & 0x0F)
	shift := uint(4)

	// Parse variable-length size encoding
	// Continue reading size bytes while the MSB (bit 7) is set
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

	if objectType == OBJ_REF_DELTA {
		// This is a reference delta - read the base object hash
		isDelta = true

		// Read the base object hash (20 bytes for SHA-1)
		baseHashBytes := make([]byte, 20)
		_, err = io.ReadFull(file, baseHashBytes)
		if err != nil {
			return nil, false, "", fmt.Errorf("failed to read base hash: %w", err)
		}
		baseHash = fmt.Sprintf("%x", baseHashBytes)
	} else if objectType == OBJ_OFS_DELTA {
		// Offset delta - read the negative offset to base
		isDelta = true
		
		// Read the variable-length offset
		var baseOffset uint64
		var b byte = 0x80
		var i uint = 0
		
		for (b & 0x80) != 0 {
			b, err = readByte(file)
			if err != nil {
				return nil, false, "", fmt.Errorf("failed to read delta offset: %w", err)
			}
			
			baseOffset |= uint64(b&0x7F) << (i * 7)
			i++
			
			// Break if this is the last byte (MSB not set)
			if (b & 0x80) == 0 {
				break
			}
		}
		
		// Calculate base object position from the offset
		basePos := startPos - int64(baseOffset)
		
		// For offset deltas, we can't determine the base hash here
		// We'll need to set a placeholder and resolve it later
		baseHash = fmt.Sprintf("offset:%d", basePos)
	}
	
	// Initialize a zlib reader to decompress the object data
	zlibReader, err := zlib.NewReader(file)
	if err != nil {
		return nil, false, "", fmt.Errorf("failed to create zlib reader: %w", err)
	}
	defer zlibReader.Close()

	// Read the decompressed object data
	data := make([]byte, 0, size)
	buf := make([]byte, 4096) // Read in chunks
	
	var totalRead uint64
	for totalRead < size {
		n, err := zlibReader.Read(buf)
		if err != nil && err != io.EOF {
			return nil, false, "", fmt.Errorf("failed to read object data: %w", err)
		}
		
		if n > 0 {
			data = append(data, buf[:n]...)
			totalRead += uint64(n)
		}
		
		if err == io.EOF {
			break
		}
	}
	
	// Verify size
	if totalRead != size {
		fmt.Printf("Warning: Object size mismatch - expected %d, got %d bytes\n", size, totalRead)
	}

	// Create and return the object
	return &Object{
		// Hash will be filled in later when we know what it is
		Type:     objectType,
		Data:     data,
		IsDelta:  isDelta,
		BaseHash: baseHash,
	}, isDelta, baseHash, nil
}

// readByte reads a single byte from the file
func readByte(file *os.File) (byte, error) {
	buf := make([]byte, 1)
	n, err := file.Read(buf)
	if err != nil {
		return 0, err
	}
	if n != 1 {
		return 0, io.ErrUnexpectedEOF
	}
	return buf[0], nil
}

// calculateObjectHash calculates a hash for an object
// This is a more complete implementation compared to the placeholder
func calculateObjectHash(objType ObjectType, data []byte) string {
	// Create the object header (e.g., "blob 12345\0")
	var typeStr string
	switch objType {
	case OBJ_COMMIT:
		typeStr = "commit"
	case OBJ_TREE:
		typeStr = "tree"
	case OBJ_BLOB:
		typeStr = "blob"
	default:
		typeStr = "blob" // Default to blob for unknown types
	}
	
	header := fmt.Sprintf("%s %d\x00", typeStr, len(data))
	
	// Calculate SHA-1 hash of the entire content including header
	h := sha1.New()
	h.Write([]byte(header))
	h.Write(data)
	
	return hex.EncodeToString(h.Sum(nil))
}
