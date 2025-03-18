package objects

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
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
	Entries  map[string]PackIndexEntry
	Checksum []byte // SHA-256 checksum of the packfile
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

// CreatePackfile creates a packfile from loose objects and writes it to disk with optimized delta compression
func CreatePackfile(repoRoot string, objectHashes []string, destPath string, createIndex bool) error {
	if len(objectHashes) == 0 {
		return errors.New("no objects specified for packfile")
	}

	// First, load all objects and prepare them for packing
	objects := make([]Object, 0, len(objectHashes))
	objectMap := make(map[string]*Object)

	fmt.Printf("Preparing %d objects for packing...\n", len(objectHashes))

	// Load all objects first for potential delta creation
	for _, hash := range objectHashes {
		// Try to determine the type of object
		if commit, err := GetCommit(repoRoot, hash); err == nil {
			data, err := commit.serialize()
			if err != nil {
				return fmt.Errorf("failed to serialize commit %s: %w", hash, err)
			}
			obj := Object{
				Hash: hash,
				Type: OBJ_COMMIT,
				Data: data,
			}
			objects = append(objects, obj)
			objectMap[hash] = &obj
		} else if tree, err := GetTree(hash); err == nil {
			data, err := tree.Serialize()
			if err != nil {
				return fmt.Errorf("failed to serialize tree %s: %w", hash, err)
			}
			obj := Object{
				Hash: hash,
				Type: OBJ_TREE,
				Data: data,
			}
			objects = append(objects, obj)
			objectMap[hash] = &obj
		} else {
			// Assume blob if not commit or tree
			blobData, err := GetBlob(repoRoot, hash)
			if err != nil {
				return fmt.Errorf("failed to load object %s: %w", hash, err)
			}
			obj := Object{
				Hash: hash,
				Type: OBJ_BLOB,
				Data: blobData,
			}
			objects = append(objects, obj)
			objectMap[hash] = &obj
		}
	}

	// Sort objects to optimize delta compression
	// Sort strategy: first by type, then by size for better delta candidates
	sort.Slice(objects, func(i, j int) bool {
		// Sort by type first
		if objects[i].Type != objects[j].Type {
			return objects[i].Type < objects[j].Type
		}

		// Then sort by size to group similar-sized objects together
		return len(objects[i].Data) < len(objects[j].Data)
	})

	// Apply delta compression for suitable candidates
	deltaObjects := make(map[string]DeltaObject)
	maxDeltaSize := 50 * 1024 * 1024 // Don't create deltas larger than 50MB

	// List of objects that will be stored in packfile directly (not as deltas)
	directObjects := make([]Object, 0)

	// Process objects by type to find delta candidates
	currentType := ObjectType(0)
	typeGroup := make([]Object, 0)

	fmt.Println("Computing deltas for similar objects...")

	for i, obj := range objects {
		// When type changes, process the previous group and start a new one
		if obj.Type != currentType {
			// Process the previous group
			processTypeGroup(typeGroup, objectMap, deltaObjects, maxDeltaSize)

			// Start a new group
			typeGroup = typeGroup[:0]
			currentType = obj.Type
		}

		// Add to current group
		typeGroup = append(typeGroup, obj)

		// If this is the last object, process the final group
		if i == len(objects)-1 {
			processTypeGroup(typeGroup, objectMap, deltaObjects, maxDeltaSize)
		}
	}

	// Determine which objects will be stored directly
	for _, obj := range objects {
		if _, isDelta := deltaObjects[obj.Hash]; !isDelta {
			directObjects = append(directObjects, obj)
		}
	}

	fmt.Printf("Created %d deltas, %d direct objects\n", len(deltaObjects), len(directObjects))

	// Create a new packfile
	packFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create packfile: %w", err)
	}
	defer packFile.Close()

	// Calculate total objects (direct + delta)
	totalObjects := uint32(len(directObjects) + len(deltaObjects))

	// Write header
	header := PackFileHeader{
		Signature:  [4]byte{'P', 'A', 'C', 'K'},
		Version:    2,
		NumObjects: totalObjects,
	}
	if err := binary.Write(packFile, binary.BigEndian, &header); err != nil {
		return fmt.Errorf("failed to write packfile header: %w", err)
	}

	// Track object offsets for index
	objectOffsets := make(map[string]uint64)

	// Write base objects first
	writtenHashes := make(map[string]bool)

	// Write all direct objects first (not deltas)
	for _, obj := range directObjects {
		// Skip if already written
		if writtenHashes[obj.Hash] {
			continue
		}

		// Record the offset
		offset, _ := packFile.Seek(0, io.SeekCurrent)
		objectOffsets[obj.Hash] = uint64(offset)
		writtenHashes[obj.Hash] = true

		// Write object header including type and size
		if err := writeObjectHeader(packFile, obj.Type, len(obj.Data)); err != nil {
			return fmt.Errorf("failed to write object header for %s: %w", obj.Hash, err)
		}

		// Compress object data using zlib
		if err := writeCompressedData(packFile, obj.Data); err != nil {
			return fmt.Errorf("failed to write object data for %s: %w", obj.Hash, err)
		}
	}

	// Now write delta objects, ensuring base objects are written first
	deltaQueue := make([]string, 0, len(deltaObjects))
	for hash := range deltaObjects {
		deltaQueue = append(deltaQueue, hash)
	}

	// Process delta queue until empty
	for len(deltaQueue) > 0 {
		// Progress tracking
		if len(deltaQueue)%100 == 0 && len(deltaQueue) > 0 {
			fmt.Printf("Remaining deltas to write: %d\n", len(deltaQueue))
		}

		// Get next candidate
		hash := deltaQueue[0]
		deltaQueue = deltaQueue[1:]

		// Skip if already written
		if writtenHashes[hash] {
			continue
		}

		delta := deltaObjects[hash]

		// Check if base is available
		if !writtenHashes[delta.BaseHash] {
			// Base not written yet, put back in queue
			deltaQueue = append(deltaQueue, hash)
			continue
		}

		// Record the offset
		offset, _ := packFile.Seek(0, io.SeekCurrent)
		objectOffsets[hash] = uint64(offset)
		writtenHashes[hash] = true

		// Write delta object header (type OBJ_DELTA)
		if err := writeObjectHeader(packFile, OBJ_DELTA, len(delta.Delta)+64); err != nil {
			return fmt.Errorf("failed to write delta header for %s: %w", hash, err)
		}

		// Write the base object hash
		if _, err := packFile.Write([]byte(delta.BaseHash)); err != nil {
			return fmt.Errorf("failed to write delta base hash: %w", err)
		}

		// Compress delta data
		if err := writeCompressedData(packFile, delta.Delta); err != nil {
			return fmt.Errorf("failed to write delta data for %s: %w", hash, err)
		}
	}

	// Calculate packfile checksum and append it to the file
	if _, err := packFile.Seek(0, io.SeekCurrent); err != nil {
		return fmt.Errorf("failed to get current position for checksum: %w", err)
	}

	// Flush any buffered data
	if err := packFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync packfile: %w", err)
	}

	// Compute SHA-256 checksum
	_, err = packFile.Seek(0, 0)
	if err != nil {
		return fmt.Errorf("failed to seek to beginning for checksum: %w", err)
	}

	hasher := sha256.New()
	if _, err := io.Copy(hasher, packFile); err != nil {
		return fmt.Errorf("failed to compute packfile checksum: %w", err)
	}

	checksum := hasher.Sum(nil)

	// Seek to end to write checksum
	if _, err := packFile.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("failed to seek to end for writing checksum: %w", err)
	}

	// Write checksum
	if _, err := packFile.Write(checksum); err != nil {
		return fmt.Errorf("failed to write packfile checksum: %w", err)
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

			// Check if it's a direct object or a delta
			if delta, isDelta := deltaObjects[hash]; isDelta {
				objType = delta.Type // Use the final type from delta
				// We need to get the size of the full object, not just the delta instructions
				if baseObj, ok := objectMap[delta.BaseHash]; ok {
					// Estimate size - ideally we would apply the delta to get exact size
					size = uint64(len(baseObj.Data)) // Approximate
				}
			} else {
				// Find object details from our loaded objects
				if obj, ok := objectMap[hash]; ok {
					objType = obj.Type
					size = uint64(len(obj.Data))
				}
			}

			index.Entries[hash] = PackIndexEntry{
				Offset: offset,
				Type:   objType,
				Size:   size,
			}
		}

		// Add checksum to index
		index.Checksum = checksum

		if err := WritePackIndex(&index, indexPath); err != nil {
			return fmt.Errorf("failed to write packfile index: %w", err)
		}
	}

	fmt.Printf("Successfully created packfile with %d objects (%d direct, %d deltas)\n",
		totalObjects, len(directObjects), len(deltaObjects))

	return nil
}

// Helper functions to improve readability and modularity

// writeObjectHeader writes the type and size header for an object
func writeObjectHeader(file *os.File, objType ObjectType, dataSize int) error {
	// Write object header (type and size)
	headerByte := byte((int(objType) << 4) | (dataSize & 0x0F))
	size := dataSize
	if size >= 16 {
		headerByte |= 0x80 // Set MSB if size continues
	}
	if _, err := file.Write([]byte{headerByte}); err != nil {
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
		if _, err := file.Write([]byte{b}); err != nil {
			return fmt.Errorf("failed to write object size: %w", err)
		}
	}

	return nil
}

// writeCompressedData compresses and writes data to the file
func writeCompressedData(file *os.File, data []byte) error {
	zlibWriter, err := zlib.NewWriterLevel(file, zlib.BestCompression)
	if err != nil {
		return fmt.Errorf("failed to create zlib writer: %w", err)
	}

	if _, err := zlibWriter.Write(data); err != nil {
		zlibWriter.Close()
		return fmt.Errorf("failed to write object data: %w", err)
	}

	if err := zlibWriter.Close(); err != nil {
		return fmt.Errorf("failed to finish zlib compression: %w", err)
	}

	return nil
}

// processTypeGroup finds delta compression candidates within a group of objects of the same type
func processTypeGroup(objects []Object, objectMap map[string]*Object, deltaObjects map[string]DeltaObject, maxDeltaSize int) {
	if len(objects) < 2 {
		return // Not enough objects to create deltas
	}

	// Sort by size first for better delta candidates
	sort.Slice(objects, func(i, j int) bool {
		return len(objects[i].Data) < len(objects[j].Data)
	})

	// Use a simple sliding window approach to find delta candidates
	windowSize := 10 // Consider the previous 10 objects as delta bases

	for i := 0; i < len(objects); i++ {
		targetObj := objects[i]

		// Skip if object is too big
		if len(targetObj.Data) > maxDeltaSize {
			continue
		}

		// Start window at most windowSize objects back
		start := i - windowSize
		if start < 0 {
			start = 0
		}

		bestSimilarity := 0.0
		var bestBaseIndex int
		var bestDelta []byte

		// Find the best base object
		for j := start; j < i; j++ {
			baseObj := objects[j]

			// Skip if too different in size (unlikely to have good delta)
			if len(baseObj.Data) < len(targetObj.Data)/3 || len(baseObj.Data) > len(targetObj.Data)*3 {
				continue
			}

			// Compute similarity score (simple for now - could be optimized)
			similarity := computeSimilarity(baseObj.Data, targetObj.Data)

			// Create delta if similarity is good enough
			if similarity > 0.5 && similarity > bestSimilarity {
				delta, err := createDelta(baseObj.Data, targetObj.Data)
				if err != nil {
					continue
				}

				// Only use delta if it's smaller than the original
				if len(delta) < len(targetObj.Data)*3/4 {
					bestSimilarity = similarity
					bestBaseIndex = j
					bestDelta = delta
				}
			}
		}

		// If we found a good delta, store it
		if bestSimilarity > 0.5 && bestDelta != nil {
			baseObj := objects[bestBaseIndex]
			deltaObjects[targetObj.Hash] = DeltaObject{
				BaseHash: baseObj.Hash,
				Hash:     targetObj.Hash,
				Type:     targetObj.Type,
				Delta:    bestDelta,
			}
		}
	}
}

// Compute similarity between two byte arrays (simple implementation)
func computeSimilarity(a, b []byte) float64 {
	// Sample up to 1000 bytes from each object to get a quick similarity measure
	sampleSize := 1000
	if len(a) < sampleSize {
		sampleSize = len(a)
	}
	if len(b) < sampleSize {
		sampleSize = len(b)
	}

	// Count matching bytes in the samples
	matches := 0
	for i := 0; i < sampleSize; i++ {
		if i < len(a) && i < len(b) && a[i] == b[i] {
			matches++
		}
	}

	return float64(matches) / float64(sampleSize)
}

// Create a delta between base and target
func createDelta(base, target []byte) ([]byte, error) {
	// Buffer to hold the delta instructions
	var delta bytes.Buffer

	// Write source size (variable length encoding)
	sourceSize := len(base)
	encodeVarint(&delta, uint64(sourceSize))

	// Write target size (variable length encoding)
	targetSize := len(target)
	encodeVarint(&delta, uint64(targetSize))

	// Find common sections using a simple sliding window
	// This is a basic implementation - a real one would use rolling checksums
	// and more sophisticated algorithms

	// Minimum copy size
	minCopySize := 4

	pos := 0
	for pos < len(target) {
		bestCopyLen := 0
		bestCopyPos := 0

		// Look for a match in the base
		for basePos := 0; basePos < len(base); basePos++ {
			// Look for a match starting at this position
			matchLen := 0
			for matchLen < len(base)-basePos &&
				matchLen < len(target)-pos &&
				base[basePos+matchLen] == target[pos+matchLen] {
				matchLen++
			}

			if matchLen > bestCopyLen {
				bestCopyLen = matchLen
				bestCopyPos = basePos
			}
		}

		if bestCopyLen >= minCopySize {
			// Copy instruction (7-bit set in first byte)
			instruction := byte(0x80)

			// Encode which offset/size bits are used
			if bestCopyPos <= 0xFF {
				instruction |= 0x01 // Offset fits in 1 byte
			} else if bestCopyPos <= 0xFFFF {
				instruction |= 0x02 // Offset fits in 2 bytes
			} else if bestCopyPos <= 0xFFFFFF {
				instruction |= 0x04 // Offset fits in 3 bytes
			} else {
				instruction |= 0x08 // Offset needs 4 bytes
			}

			if bestCopyLen <= 0xFF {
				instruction |= 0x10 // Size fits in 1 byte
			} else if bestCopyLen <= 0xFFFF {
				instruction |= 0x20 // Size fits in 2 bytes
			} else if bestCopyLen <= 0xFFFFFF {
				instruction |= 0x40 // Size fits in 3 bytes
			}

			// Write the instruction byte
			delta.WriteByte(instruction)

			// Write the offset
			if instruction&0x01 != 0 {
				delta.WriteByte(byte(bestCopyPos & 0xFF))
			}
			if instruction&0x02 != 0 {
				delta.WriteByte(byte((bestCopyPos >> 8) & 0xFF))
			}
			if instruction&0x04 != 0 {
				delta.WriteByte(byte((bestCopyPos >> 16) & 0xFF))
			}
			if instruction&0x08 != 0 {
				delta.WriteByte(byte((bestCopyPos >> 24) & 0xFF))
			}

			// Write the size
			if instruction&0x10 != 0 {
				delta.WriteByte(byte(bestCopyLen & 0xFF))
			}
			if instruction&0x20 != 0 {
				delta.WriteByte(byte((bestCopyLen >> 8) & 0xFF))
			}
			if instruction&0x40 != 0 {
				delta.WriteByte(byte((bestCopyLen >> 16) & 0xFF))
			}

			// Move position forward
			pos += bestCopyLen
		} else {
			// Insert instruction
			// Find how many bytes to insert
			insertLen := 1
			for insertLen < 127 && pos+insertLen < len(target) {
				// See if we can find a copy after this insert
				potentialCopyLen := 0

				for basePos := 0; basePos < len(base); basePos++ {
					matchLen := 0
					for matchLen < len(base)-basePos &&
						matchLen < len(target)-(pos+insertLen) &&
						base[basePos+matchLen] == target[pos+insertLen+matchLen] {
						matchLen++
					}

					if matchLen > potentialCopyLen {
						potentialCopyLen = matchLen
					}
				}

				if potentialCopyLen >= minCopySize {
					break // Stop insert here, we found a good copy
				}

				insertLen++
			}

			// Write the insert instruction (top bit is 0, lower 7 bits are length)
			delta.WriteByte(byte(insertLen & 0x7F))

			// Write the data to insert
			delta.Write(target[pos : pos+insertLen])

			// Move position forward
			pos += insertLen
		}
	}

	return delta.Bytes(), nil
}

// encodeVarint writes a variable-length integer to the buffer
func encodeVarint(buf *bytes.Buffer, val uint64) {
	for {
		b := byte(val & 0x7F)
		val >>= 7
		if val != 0 {
			b |= 0x80 // Set top bit to indicate more bytes follow
		}
		buf.WriteByte(b)
		if val == 0 {
			break
		}
	}
}

// ReadPackIndex reads a packfile index from disk
func ReadPackIndex(indexPath string) (*PackfileIndex, error) {
	file, err := os.Open(indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open index file: %w", err)
	}
	defer file.Close()

	// Read and verify magic header
	header := make([]byte, 4)
	if _, err := io.ReadFull(file, header); err != nil {
		return nil, fmt.Errorf("failed to read index header: %w", err)
	}
	if string(header) != "VIDX" {
		return nil, fmt.Errorf("invalid index header: %s", string(header))
	}

	// Read version
	var version uint32
	if err := binary.Read(file, binary.BigEndian, &version); err != nil {
		return nil, fmt.Errorf("failed to read index version: %w", err)
	}
	if version != 1 {
		return nil, fmt.Errorf("unsupported index version: %d", version)
	}

	// Read number of entries
	var numEntries uint32
	if err := binary.Read(file, binary.BigEndian, &numEntries); err != nil {
		return nil, fmt.Errorf("failed to read index entry count: %w", err)
	}

	// Create the index object
	index := &PackfileIndex{
		Entries: make(map[string]PackIndexEntry),
	}

	// Read each entry
	for i := uint32(0); i < numEntries; i++ {
		// Read hash (64 bytes for SHA-256 hex)
		hashBytes := make([]byte, hashLength)
		if _, err := io.ReadFull(file, hashBytes); err != nil {
			return nil, fmt.Errorf("failed to read hash: %w", err)
		}
		hash := string(hashBytes)

		// Read object offset
		var offset uint64
		if err := binary.Read(file, binary.BigEndian, &offset); err != nil {
			return nil, fmt.Errorf("failed to read offset: %w", err)
		}

		// Read object type
		var objType byte
		if err := binary.Read(file, binary.BigEndian, &objType); err != nil {
			return nil, fmt.Errorf("failed to read type: %w", err)
		}

		// Read object size
		var size uint64
		if err := binary.Read(file, binary.BigEndian, &size); err != nil {
			return nil, fmt.Errorf("failed to read size: %w", err)
		}

		// Add to the index
		index.Entries[hash] = PackIndexEntry{
			Offset: offset,
			Type:   ObjectType(objType),
			Size:   size,
		}
	}

	// Read packfile checksum if present (depends on file position)
	currentPos, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("failed to get current position: %w", err)
	}

	// Check if there's room for packfile checksum
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	if fileInfo.Size()-currentPos >= 64 { // Assuming there's at least a SHA256 checksum
		// Read packfile checksum
		checksum := make([]byte, 32) // SHA-256 is 32 bytes
		if _, err := io.ReadFull(file, checksum); err != nil {
			return nil, fmt.Errorf("failed to read packfile checksum: %w", err)
		}
		index.Checksum = checksum
	}

	return index, nil
}

// Update WritePackIndex and ReadPackIndex to handle the checksum
func WritePackIndex(index *PackfileIndex, indexPath string) error {
	// Create the index file
	file, err := os.Create(indexPath)
	if err != nil {
		return fmt.Errorf("failed to create index file: %w", err)
	}
	defer file.Close()

	// Write a magic header for the index file
	if _, err := file.Write([]byte("VIDX")); err != nil {
		return fmt.Errorf("failed to write index header: %w", err)
	}

	// Write version (uint32)
	if err := binary.Write(file, binary.BigEndian, uint32(1)); err != nil {
		return fmt.Errorf("failed to write index version: %w", err)
	}

	// Write number of entries (uint32)
	if err := binary.Write(file, binary.BigEndian, uint32(len(index.Entries))); err != nil {
		return fmt.Errorf("failed to write index entry count: %w", err)
	}

	// Write entries in sorted order
	sortedHashes := make([]string, 0, len(index.Entries))
	for hash := range index.Entries {
		sortedHashes = append(sortedHashes, hash)
	}
	sort.Strings(sortedHashes)

	for _, hash := range sortedHashes {
		entry := index.Entries[hash]

		// Write hash (64 bytes for SHA-256 hex)
		if _, err := file.Write([]byte(hash)); err != nil {
			return fmt.Errorf("failed to write hash: %w", err)
		}

		// Write offset (uint64)
		if err := binary.Write(file, binary.BigEndian, entry.Offset); err != nil {
			return fmt.Errorf("failed to write offset: %w", err)
		}

		// Write type (byte)
		if err := binary.Write(file, binary.BigEndian, byte(entry.Type)); err != nil {
			return fmt.Errorf("failed to write type: %w", err)
		}

		// Write size (uint64)
		if err := binary.Write(file, binary.BigEndian, entry.Size); err != nil {
			return fmt.Errorf("failed to write size: %w", err)
		}
	}

	// Write packfile checksum if available
	if index.Checksum != nil && len(index.Checksum) > 0 {
		if _, err := file.Write(index.Checksum); err != nil {
			return fmt.Errorf("failed to write packfile checksum: %w", err)
		}
	}

	// Write index checksum (SHA-256 of the index file up to this point)
	// Seek to beginning
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek to beginning for index checksum: %w", err)
	}

	// Compute checksum
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("failed to compute index checksum: %w", err)
	}

	// Seek to end
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("failed to seek to end for writing index checksum: %w", err)
	}

	// Write index checksum
	if _, err := file.Write(hasher.Sum(nil)); err != nil {
		return fmt.Errorf("failed to write index checksum: %w", err)
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
