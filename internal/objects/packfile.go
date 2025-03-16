package objects

import (
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/NahomAnteneh/vec/utils"
)

const hashLength = 64 // SHA-256 hash length in hex form

// ObjectType represents the type of an object in the packfile
type ObjectType byte

const (
	// Object types
	OBJ_COMMIT ObjectType = 1
	OBJ_TREE   ObjectType = 2
	OBJ_BLOB   ObjectType = 3
	OBJ_DELTA  ObjectType = 4
)

// Object represents a parsed object from a packfile.
type Object struct {
	Hash string
	Type ObjectType
	Data []byte
}

// DeltaObject represents a delta object with a base object hash and delta instructions
type DeltaObject struct {
	BaseHash string     // Hash of the base object
	Hash     string     // Hash of the resulting object after applying delta
	Type     ObjectType // Type of the final object (inherited from base)
	Delta    []byte     // Delta instructions
}

// PackfileIndex represents an index for a packfile
type PackfileIndex struct {
	Entries map[string]PackIndexEntry
}

// PackIndexEntry stores information about an object in the packfile
type PackIndexEntry struct {
	Offset uint64     // Offset in the packfile
	Type   ObjectType // Object type
	Size   uint64     // Size of the object data
}

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

// PackFileHeader represents the header of a packfile
type PackFileHeader struct {
	Signature  [4]byte // Should be "PACK"
	Version    uint32  // Pack format version (currently 2)
	NumObjects uint32  // Number of objects in the pack
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
				Hash: utils.HashBytes(typeToString(baseObj.Type), resultData),
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

// CreatePackfile creates a packfile from loose objects and writes it to disk
func CreatePackfile(repoRoot string, objectHashes []string, destPath string, createIndex bool) error {
	if len(objectHashes) == 0 {
		return errors.New("no objects specified for packfile")
	}

	// First, load all objects and prepare them for packing
	objects := make([]Object, 0, len(objectHashes))
	for _, hash := range objectHashes {
		// Try to determine the type of object
		if commit, err := GetCommit(repoRoot, hash); err == nil {
			data, err := commit.serialize()
			if err != nil {
				return fmt.Errorf("failed to serialize commit %s: %w", hash, err)
			}
			objects = append(objects, Object{
				Hash: hash,
				Type: OBJ_COMMIT,
				Data: data,
			})
		} else if tree, err := GetTree(hash); err == nil {
			data, err := tree.Serialize()
			if err != nil {
				return fmt.Errorf("failed to serialize tree %s: %w", hash, err)
			}
			objects = append(objects, Object{
				Hash: hash,
				Type: OBJ_TREE,
				Data: data,
			})
		} else {
			// Assume blob if not commit or tree
			blobData, err := GetBlob(repoRoot, hash)
			if err != nil {
				return fmt.Errorf("failed to load object %s: %w", hash, err)
			}
			objects = append(objects, Object{
				Hash: hash,
				Type: OBJ_BLOB,
				Data: blobData,
			})
		}
	}

	// Sort objects to optimize delta compression (similar objects near each other)
	sort.Slice(objects, func(i, j int) bool {
		// Sort by type first, then by name/hash to group similar objects
		if objects[i].Type != objects[j].Type {
			return objects[i].Type < objects[j].Type
		}
		return objects[i].Hash < objects[j].Hash
	})

	// Create a new packfile
	packFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create packfile: %w", err)
	}
	defer packFile.Close()

	// Write header
	header := PackFileHeader{
		Signature:  [4]byte{'P', 'A', 'C', 'K'},
		Version:    2,
		NumObjects: uint32(len(objects)),
	}
	if err := binary.Write(packFile, binary.BigEndian, &header); err != nil {
		return fmt.Errorf("failed to write packfile header: %w", err)
	}

	// Track object offsets for index
	objectOffsets := make(map[string]uint64)

	// Write objects
	for _, obj := range objects {
		// Record the offset
		offset, _ := packFile.Seek(0, io.SeekCurrent)
		objectOffsets[obj.Hash] = uint64(offset)

		// Write object header
		headerByte := byte((int(obj.Type) << 4) | (len(obj.Data) & 0x0F))
		size := len(obj.Data)
		if size >= 16 {
			headerByte |= 0x80 // Set MSB if size continues
		}
		if _, err := packFile.Write([]byte{headerByte}); err != nil {
			return fmt.Errorf("failed to write object header: %w", err)
		}

		// Write additional size bytes if needed
		size >>= 4
		for size > 0 {
			b := byte(size & 0x7F)
			size >>= 7
			if size > 0 {
				b |= 0x80 // Set MSB if size continues
			}
			if _, err := packFile.Write([]byte{b}); err != nil {
				return fmt.Errorf("failed to write object size: %w", err)
			}
		}

		// Compress object data using zlib
		zlibWriter, err := zlib.NewWriterLevel(packFile, zlib.BestCompression)
		if err != nil {
			return fmt.Errorf("failed to create zlib writer: %w", err)
		}
		if _, err := zlibWriter.Write(obj.Data); err != nil {
			zlibWriter.Close()
			return fmt.Errorf("failed to write object data: %w", err)
		}
		if err := zlibWriter.Close(); err != nil {
			return fmt.Errorf("failed to finish zlib compression: %w", err)
		}
	}

	// Create index if requested
	if createIndex {
		indexPath := destPath + ".idx"
		index := PackfileIndex{
			Entries: make(map[string]PackIndexEntry),
		}

		// Create index entries
		for hash, offset := range objectOffsets {
			var objType ObjectType
			var size uint64

			// Find object details from our loaded objects
			for _, obj := range objects {
				if obj.Hash == hash {
					objType = obj.Type
					size = uint64(len(obj.Data))
					break
				}
			}

			index.Entries[hash] = PackIndexEntry{
				Offset: offset,
				Type:   objType,
				Size:   size,
			}
		}

		if err := WritePackIndex(&index, indexPath); err != nil {
			return fmt.Errorf("failed to write packfile index: %w", err)
		}
	}

	return nil
}

// ReadPackIndex reads a packfile index from disk
func ReadPackIndex(indexPath string) (*PackfileIndex, error) {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read index file: %w", err)
	}

	if len(data) < 4 {
		return nil, errors.New("invalid index file: too short")
	}

	// Check for signature "VIDX" (Vec Index)
	if string(data[:4]) != "VIDX" {
		return nil, errors.New("invalid index file signature")
	}

	// Parse index version
	if len(data) < 8 {
		return nil, errors.New("invalid index file: missing version")
	}
	version := binary.BigEndian.Uint32(data[4:8])
	if version != 1 {
		return nil, fmt.Errorf("unsupported index version: %d", version)
	}

	// Parse number of entries
	if len(data) < 12 {
		return nil, errors.New("invalid index file: missing entry count")
	}
	numEntries := binary.BigEndian.Uint32(data[8:12])

	// Create index structure
	index := &PackfileIndex{
		Entries: make(map[string]PackIndexEntry, numEntries),
	}

	// Read each entry
	offset := 12
	for i := uint32(0); i < numEntries; i++ {
		// Make sure we have enough data left
		if offset+hashLength+1+8+8 > len(data) {
			return nil, fmt.Errorf("unexpected end of index file at entry %d", i)
		}

		// Read hash (hex string)
		hash := string(data[offset : offset+hashLength])
		offset += hashLength

		// Read object type
		objType := ObjectType(data[offset])
		offset++

		// Read object offset in packfile
		objOffset := binary.BigEndian.Uint64(data[offset : offset+8])
		offset += 8

		// Read object size
		objSize := binary.BigEndian.Uint64(data[offset : offset+8])
		offset += 8

		// Add entry to index
		index.Entries[hash] = PackIndexEntry{
			Offset: objOffset,
			Type:   objType,
			Size:   objSize,
		}
	}

	return index, nil
}

// WritePackIndex writes a packfile index to disk
func WritePackIndex(index *PackfileIndex, indexPath string) error {
	// Calculate total size: header (12 bytes) + entries (hash + type + offset + size)
	entrySize := hashLength + 1 + 8 + 8
	totalSize := 12 + (len(index.Entries) * entrySize)

	// Create buffer to hold index data
	data := make([]byte, totalSize)

	// Write header
	// - Signature "VIDX" (4 bytes)
	copy(data[0:4], []byte("VIDX"))

	// - Version 1 (4 bytes)
	binary.BigEndian.PutUint32(data[4:8], 1)

	// - Number of entries (4 bytes)
	binary.BigEndian.PutUint32(data[8:12], uint32(len(index.Entries)))

	// Write entries
	offset := 12
	for hash, entry := range index.Entries {
		// Write hash (as string)
		copy(data[offset:offset+hashLength], []byte(hash))
		offset += hashLength

		// Write object type
		data[offset] = byte(entry.Type)
		offset++

		// Write object offset
		binary.BigEndian.PutUint64(data[offset:offset+8], entry.Offset)
		offset += 8

		// Write object size
		binary.BigEndian.PutUint64(data[offset:offset+8], entry.Size)
		offset += 8
	}

	// Write to file
	err := os.WriteFile(indexPath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}

	return nil
}

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

// applyDelta applies delta instructions to a base object to produce a new object
func applyDelta(base, delta []byte) ([]byte, error) {
	if len(delta) < 2 {
		return nil, errors.New("delta too short")
	}

	// Read source and target size from delta
	var sourceSize, targetSize uint64
	deltaOffset := 0

	// Read variable-length source size
	for shift := uint(0); ; shift += 7 {
		b := delta[deltaOffset]
		deltaOffset++
		sourceSize |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			break
		}
		if deltaOffset >= len(delta) {
			return nil, errors.New("unexpected end of delta while reading source size")
		}
	}

	// Read variable-length target size
	for shift := uint(0); ; shift += 7 {
		b := delta[deltaOffset]
		deltaOffset++
		targetSize |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			break
		}
		if deltaOffset >= len(delta) {
			return nil, errors.New("unexpected end of delta while reading target size")
		}
	}

	// Verify that source size matches the base object size
	if uint64(len(base)) != sourceSize {
		return nil, fmt.Errorf("base size mismatch: expected %d, got %d", sourceSize, len(base))
	}

	// Allocate space for the result
	result := make([]byte, 0, targetSize)

	// Process delta instructions
	for deltaOffset < len(delta) {
		// Read instruction byte
		instruction := delta[deltaOffset]
		deltaOffset++

		if instruction&0x80 != 0 {
			// Copy instruction - copy data from base
			var offset, size uint64

			// Read offset (if bits are set)
			if instruction&0x01 != 0 {
				if deltaOffset >= len(delta) {
					return nil, errors.New("unexpected end of delta while reading copy offset")
				}
				offset = uint64(delta[deltaOffset])
				deltaOffset++
			}
			if instruction&0x02 != 0 {
				if deltaOffset >= len(delta) {
					return nil, errors.New("unexpected end of delta while reading copy offset")
				}
				offset |= uint64(delta[deltaOffset]) << 8
				deltaOffset++
			}
			if instruction&0x04 != 0 {
				if deltaOffset >= len(delta) {
					return nil, errors.New("unexpected end of delta while reading copy offset")
				}
				offset |= uint64(delta[deltaOffset]) << 16
				deltaOffset++
			}
			if instruction&0x08 != 0 {
				if deltaOffset >= len(delta) {
					return nil, errors.New("unexpected end of delta while reading copy offset")
				}
				offset |= uint64(delta[deltaOffset]) << 24
				deltaOffset++
			}

			// Read size (if bits are set)
			if instruction&0x10 != 0 {
				if deltaOffset >= len(delta) {
					return nil, errors.New("unexpected end of delta while reading copy size")
				}
				size = uint64(delta[deltaOffset])
				deltaOffset++
			}
			if instruction&0x20 != 0 {
				if deltaOffset >= len(delta) {
					return nil, errors.New("unexpected end of delta while reading copy size")
				}
				size |= uint64(delta[deltaOffset]) << 8
				deltaOffset++
			}
			if instruction&0x40 != 0 {
				if deltaOffset >= len(delta) {
					return nil, errors.New("unexpected end of delta while reading copy size")
				}
				size |= uint64(delta[deltaOffset]) << 16
				deltaOffset++
			}

			// Default size is 0x10000 if no size bits are set
			if size == 0 {
				size = 0x10000
			}

			// Validate offset and size
			if offset+size > uint64(len(base)) {
				return nil, fmt.Errorf("copy operation out of bounds: offset=%d, size=%d, base_len=%d", offset, size, len(base))
			}

			// Copy data from base
			result = append(result, base[offset:offset+size]...)
		} else if instruction > 0 {
			// Insert instruction - copy data from delta
			size := int(instruction)

			if deltaOffset+size > len(delta) {
				return nil, errors.New("unexpected end of delta while reading insert data")
			}

			// Append the literal data from the delta
			result = append(result, delta[deltaOffset:deltaOffset+size]...)
			deltaOffset += size
		} else {
			return nil, errors.New("invalid delta instruction: 0")
		}
	}

	// Verify the resulting size
	if uint64(len(result)) != targetSize {
		return nil, fmt.Errorf("result size mismatch: expected %d, got %d", targetSize, len(result))
	}

	return result, nil
}

// typeToString converts an ObjectType to its string representation
func typeToString(objType ObjectType) string {
	switch objType {
	case OBJ_COMMIT:
		return "commit"
	case OBJ_TREE:
		return "tree"
	case OBJ_BLOB:
		return "blob"
	case OBJ_DELTA:
		return "delta"
	default:
		return fmt.Sprintf("unknown(%d)", objType)
	}
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
