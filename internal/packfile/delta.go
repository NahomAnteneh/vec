package packfile

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
)

// applyDelta applies a delta to a base object to produce a new object
func applyDelta(base, delta []byte) ([]byte, error) {
	if len(delta) < 2 {
		return nil, errors.New("delta too short")
	}

	// Read the source and target size from the delta
	sourceSize, bytesRead := decodeSize(delta)
	delta = delta[bytesRead:]

	targetSize, bytesRead := decodeSize(delta)
	delta = delta[bytesRead:]

	// Verify the source size matches our base object
	if sourceSize != uint64(len(base)) {
		return nil, fmt.Errorf("source size mismatch: expected %d, got %d", len(base), sourceSize)
	}

	// Create a buffer for the target object
	result := make([]byte, 0, targetSize)

	// Apply delta instructions
	for len(delta) > 0 {
		// Read the instruction byte
		cmd := delta[0]
		delta = delta[1:]

		if cmd == 0 {
			// Reserved, should not appear in valid delta
			return nil, errors.New("invalid delta: zero command byte")
		} else if cmd&0x80 != 0 {
			// Copy instruction
			var offset, size uint32
			offsetBytes := 0
			sizeBytes := 0

			// Read offset (if present)
			if cmd&0x01 != 0 {
				if len(delta) < 1 {
					return nil, errors.New("delta too short for offset")
				}
				offset = uint32(delta[0])
				offsetBytes = 1
				delta = delta[1:]
			}
			if cmd&0x02 != 0 {
				if len(delta) < 1 {
					return nil, errors.New("delta too short for offset")
				}
				offset |= uint32(delta[0]) << 8
				offsetBytes++
				delta = delta[1:]
			}
			if cmd&0x04 != 0 {
				if len(delta) < 1 {
					return nil, errors.New("delta too short for offset")
				}
				offset |= uint32(delta[0]) << 16
				offsetBytes++
				delta = delta[1:]
			}
			if cmd&0x08 != 0 {
				if len(delta) < 1 {
					return nil, errors.New("delta too short for offset")
				}
				offset |= uint32(delta[0]) << 24
				offsetBytes++
				delta = delta[1:]
			}

			// Read size (if present)
			if cmd&0x10 != 0 {
				if len(delta) < 1 {
					return nil, errors.New("delta too short for size")
				}
				size = uint32(delta[0])
				sizeBytes = 1
				delta = delta[1:]
			}
			if cmd&0x20 != 0 {
				if len(delta) < 1 {
					return nil, errors.New("delta too short for size")
				}
				size |= uint32(delta[0]) << 8
				sizeBytes++
				delta = delta[1:]
			}
			if cmd&0x40 != 0 {
				if len(delta) < 1 {
					return nil, errors.New("delta too short for size")
				}
				size |= uint32(delta[0]) << 16
				sizeBytes++
				delta = delta[1:]
			}

			// If no size specified, use default
			if sizeBytes == 0 {
				size = 0x10000 // 64KB
			}

			// Validate the copy operation
			if offset+size > uint32(len(base)) {
				return nil, fmt.Errorf("invalid copy: offset %d + size %d > base length %d",
					offset, size, len(base))
			}

			// Copy from base
			result = append(result, base[offset:offset+size]...)
		} else {
			// Insert instruction
			size := int(cmd) // cmd is the size for insert
			if len(delta) < size {
				return nil, errors.New("delta too short for insert data")
			}

			// Insert literal data
			result = append(result, delta[:size]...)
			delta = delta[size:]
		}
	}

	// Verify the result size matches the expected target size
	if uint64(len(result)) != targetSize {
		return nil, fmt.Errorf("target size mismatch: expected %d, got %d",
			targetSize, len(result))
	}

	return result, nil
}

// decodeSize decodes a size value from a delta and returns the size and bytes read
func decodeSize(delta []byte) (uint64, int) {
	var size uint64
	shift := uint(0)
	bytesRead := 0

	for {
		if bytesRead >= len(delta) {
			// Unexpected end of input, return what we have
			return size, bytesRead
		}

		b := delta[bytesRead]
		bytesRead++

		size |= uint64(b&0x7F) << shift
		shift += 7

		if b&0x80 == 0 {
			break
		}
	}

	return size, bytesRead
}

// createDelta creates a delta from a base object to a target object
func createDelta(base, target []byte) ([]byte, error) {
	// Create buffer to hold the delta
	var buffer bytes.Buffer

	// Encode base size
	encodeSize(&buffer, uint64(len(base)))

	// Encode target size
	encodeSize(&buffer, uint64(len(target)))

	// Compute delta operations
	err := computeDelta(&buffer, base, target)
	if err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

// encodeSize encodes a size value for a delta
func encodeSize(buffer *bytes.Buffer, size uint64) {
	for {
		b := byte(size & 0x7F)
		size >>= 7
		if size == 0 {
			buffer.WriteByte(b)
			break
		}
		buffer.WriteByte(b | 0x80)
	}
}

// computeDelta computes the delta operations between base and target
func computeDelta(buffer *bytes.Buffer, base, target []byte) error {
	// This is a simplified implementation of delta creation
	// A real implementation would use rolling hash or suffix array for efficient matching

	pos := 0
	for pos < len(target) {
		// Look for matching sections in base
		matchOffset, matchLength := findLongestMatch(base, target[pos:])

		if matchLength >= 4 {
			// If we found a match worth encoding as a copy, do so
			encodeCopyCommand(buffer, matchOffset, matchLength)
			pos += int(matchLength) // Convert uint32 to int for the addition
		} else {
			// Otherwise, collect literal data to insert
			literalStart := pos
			for pos < len(target) {
				if pos+4 <= len(target) {
					matchOffset, matchLength = findLongestMatch(base, target[pos:])
					if matchLength >= 4 {
						break
					}
				}
				pos++
			}

			// Insert the literal data
			literalLength := pos - literalStart
			if literalLength > 0 {
				encodeInsertCommand(buffer, target[literalStart:pos])
			}
		}
	}

	return nil
}

// findLongestMatch finds the longest matching substring in base for the start of target
func findLongestMatch(base, target []byte) (offset, length uint32) {
	// This is a naive implementation for demonstration
	// A real implementation would use more efficient algorithms

	maxLength := uint32(0)
	maxOffset := uint32(0)

	// Don't try to match more than the target length
	maxPossibleLength := uint32(len(target))
	if maxPossibleLength > 64*1024 {
		maxPossibleLength = 64 * 1024 // Limit to 64KB
	}

	// Scan through base looking for matches
	for i := 0; i <= len(base)-4; i++ {
		// Quick check first 4 bytes to filter potential matches
		if base[i] == target[0] && base[i+1] == target[1] && base[i+2] == target[2] && base[i+3] == target[3] {
			// Count how many bytes match
			j := uint32(0)
			for i+int(j) < len(base) && j < maxPossibleLength && base[i+int(j)] == target[j] {
				j++
			}

			if j > maxLength {
				maxLength = j
				maxOffset = uint32(i)
			}
		}
	}

	return maxOffset, maxLength
}

// encodeCopyCommand encodes a copy command in the delta format
func encodeCopyCommand(buffer *bytes.Buffer, offset, length uint32) {
	// Copy command: 1000xxxx [offset] [size]
	// Where bits xxxx indicate which offset/size bytes are present

	var cmd byte = 0x80 // Copy command (bit 7 set)
	var offsetBytes, sizeBytes [4]byte
	numOffsetBytes := 0
	numSizeBytes := 0

	// Encode offset bytes (little-endian)
	if offset != 0 {
		if offset&0xFF != 0 {
			cmd |= 0x01
			offsetBytes[numOffsetBytes] = byte(offset)
			numOffsetBytes++
		}
		offset >>= 8

		if offset&0xFF != 0 {
			cmd |= 0x02
			offsetBytes[numOffsetBytes] = byte(offset)
			numOffsetBytes++
		}
		offset >>= 8

		if offset&0xFF != 0 {
			cmd |= 0x04
			offsetBytes[numOffsetBytes] = byte(offset)
			numOffsetBytes++
		}
		offset >>= 8

		if offset&0xFF != 0 {
			cmd |= 0x08
			offsetBytes[numOffsetBytes] = byte(offset)
			numOffsetBytes++
		}
	}

	// Encode size bytes (little-endian)
	if length != 0x10000 { // If not the default size
		if length&0xFF != 0 {
			cmd |= 0x10
			sizeBytes[numSizeBytes] = byte(length)
			numSizeBytes++
		}
		length >>= 8

		if length&0xFF != 0 {
			cmd |= 0x20
			sizeBytes[numSizeBytes] = byte(length)
			numSizeBytes++
		}
		length >>= 8

		if length&0xFF != 0 {
			cmd |= 0x40
			sizeBytes[numSizeBytes] = byte(length)
			numSizeBytes++
		}
	}

	// Write the command byte
	buffer.WriteByte(cmd)

	// Write offset bytes
	for i := 0; i < numOffsetBytes; i++ {
		buffer.WriteByte(offsetBytes[i])
	}

	// Write size bytes
	for i := 0; i < numSizeBytes; i++ {
		buffer.WriteByte(sizeBytes[i])
	}
}

// encodeInsertCommand encodes an insert command in the delta format
func encodeInsertCommand(buffer *bytes.Buffer, data []byte) {
	// Current implementation limited to 127 bytes per insert command
	// Real implementation would chunk larger inserts

	for len(data) > 0 {
		// Determine size for this insert command (max 127)
		size := len(data)
		if size > 127 {
			size = 127
		}

		// Write insert command (size byte)
		buffer.WriteByte(byte(size))

		// Write the data
		buffer.Write(data[:size])
		data = data[size:]
	}
}

// OptimizeObjects looks for opportunities to use deltas to reduce object size
func OptimizeObjects(objects []Object) ([]Object, error) {
	// Skip optimization if we have too few objects
	if len(objects) < 2 {
		return objects, nil
	}

	// Group objects by type - only delta against objects of the same type
	objectsByType := make(map[ObjectType][]Object)
	for _, obj := range objects {
		objectsByType[obj.Type] = append(objectsByType[obj.Type], obj)
	}

	// Resulting optimized object list
	result := make([]Object, 0, len(objects))

	// Process each object type separately
	for _, typeObjects := range objectsByType {
		// Skip optimization for this type if too few objects
		if len(typeObjects) < 2 {
			result = append(result, typeObjects...)
			continue
		}

		// Create a mapping of object hash to content for easy lookup
		contentMap := make(map[string][]byte, len(typeObjects))
		for _, obj := range typeObjects {
			contentMap[obj.Hash] = obj.Data
		}

		// Build similarity matrix for all objects of this type
		similarities := calculateSimilarities(typeObjects)

		// Find optimal delta chains
		chains := buildDeltaChains(similarities, typeObjects)

		// Apply the delta chains to create delta objects
		processedHashes := make(map[string]bool)
		for _, chain := range chains {
			// Add the base object as-is
			baseObj := findObjectByHash(typeObjects, chain.BaseHash)
			if baseObj == nil {
				// This shouldn't happen in normal operation
				continue
			}

			result = append(result, *baseObj)
			processedHashes[chain.BaseHash] = true

			// Create deltas for all descendants
			for _, deltaDesc := range chain.Deltas {
				targetObj := findObjectByHash(typeObjects, deltaDesc.TargetHash)
				if targetObj == nil || processedHashes[deltaDesc.TargetHash] {
					continue // Skip if already processed or not found
				}

				baseData := contentMap[chain.BaseHash]
				targetData := contentMap[deltaDesc.TargetHash]

				delta, err := createDelta(baseData, targetData)
				if err != nil {
					return nil, fmt.Errorf("failed to create delta for %s: %w", deltaDesc.TargetHash, err)
				}

				// Only use delta if it's smaller than the original
				if len(delta) < len(targetData) {
					deltaSavings := len(targetData) - len(delta)
					if deltaSavings >= minDeltaSavings {
						deltaObj := Object{
							Hash: targetObj.Hash,
							Type: OBJ_DELTA,
							Data: delta,
						}
						result = append(result, deltaObj)
						processedHashes[deltaDesc.TargetHash] = true
					} else {
						// Delta doesn't save enough space, use original
						result = append(result, *targetObj)
						processedHashes[deltaDesc.TargetHash] = true
					}
				} else {
					// Delta is larger, use original
					result = append(result, *targetObj)
					processedHashes[deltaDesc.TargetHash] = true
				}
			}
		}

		// Add any remaining objects that weren't delta'd
		for _, obj := range typeObjects {
			if !processedHashes[obj.Hash] {
				result = append(result, obj)
			}
		}
	}

	return result, nil
}

// Constants for delta compression
const (
	// Minimum bytes to save to use a delta
	minDeltaSavings = 512

	// Maximum delta chain length
	maxChainDepth = 5

	// Chunking size for calculating similarity
	chunkSize = 64
)

// DeltaChain represents a chain of delta objects with a base
type DeltaChain struct {
	BaseHash string
	Deltas   []DeltaDescriptor
}

// DeltaDescriptor describes a delta in a chain
type DeltaDescriptor struct {
	TargetHash      string
	SimilarityScore float64
}

// ObjectSimilarity stores similarity score between two objects
type ObjectSimilarity struct {
	Obj1Hash string
	Obj2Hash string
	Score    float64
}

// findObjectByHash returns the object with the given hash from a slice
func findObjectByHash(objects []Object, hash string) *Object {
	for i, obj := range objects {
		if obj.Hash == hash {
			return &objects[i]
		}
	}
	return nil
}

// calculateSimilarities computes similarity scores between all pairs of objects
func calculateSimilarities(objects []Object) []ObjectSimilarity {
	result := make([]ObjectSimilarity, 0, len(objects)*(len(objects)-1)/2)

	// Calculate similarity between each pair of objects
	for i := 0; i < len(objects); i++ {
		for j := i + 1; j < len(objects); j++ {
			score := calculateSimilarityScore(objects[i].Data, objects[j].Data)

			result = append(result, ObjectSimilarity{
				Obj1Hash: objects[i].Hash,
				Obj2Hash: objects[j].Hash,
				Score:    score,
			})
		}
	}

	// Sort by similarity score (highest first)
	sortSimilarities(result)

	return result
}

// calculateSimilarityScore computes a similarity score between two byte slices
// Uses a chunking approach to estimate similarity
func calculateSimilarityScore(data1, data2 []byte) float64 {
	// If either is empty, no similarity
	if len(data1) == 0 || len(data2) == 0 {
		return 0
	}

	// Get size differential factor (penalty for very different sizes)
	sizeFactor := float64(min(len(data1), len(data2))) / float64(max(len(data1), len(data2)))

	// Create fingerprints of chunks
	chunks1 := createChunkFingerprints(data1)
	chunks2 := createChunkFingerprints(data2)

	// Count matching chunks
	matchCount := 0
	for fp1 := range chunks1 {
		if _, exists := chunks2[fp1]; exists {
			matchCount++
		}
	}

	// Calculate similarity based on matching chunks and size factor
	chunkSimilarity := 0.0
	if len(chunks1) > 0 && len(chunks2) > 0 {
		// Use Jaccard similarity for chunks
		union := len(chunks1) + len(chunks2) - matchCount
		chunkSimilarity = float64(matchCount) / float64(union)
	}

	// Final score combines chunk similarity and size factor
	return chunkSimilarity * sizeFactor
}

// createChunkFingerprints divides data into chunks and creates a set of fingerprints
func createChunkFingerprints(data []byte) map[uint64]struct{} {
	fingerprints := make(map[uint64]struct{})

	// Simple chunking by fixed size
	for i := 0; i < len(data)-chunkSize+1; i += chunkSize / 2 { // 50% overlap for better matching
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}

		// Calculate a hash for this chunk
		fp := simpleHash(data[i:end])
		fingerprints[fp] = struct{}{}
	}

	return fingerprints
}

// simpleHash calculates a simple 64-bit rolling hash of a byte slice
func simpleHash(data []byte) uint64 {
	var hash uint64 = 14695981039346656037 // FNV offset basis

	for _, b := range data {
		hash ^= uint64(b)
		hash *= 1099511628211 // FNV prime
	}

	return hash
}

// sortSimilarities sorts similarity scores in descending order
func sortSimilarities(similarities []ObjectSimilarity) {
	// Sort by score in descending order
	sort.Slice(similarities, func(i, j int) bool {
		return similarities[i].Score > similarities[j].Score
	})
}

// buildDeltaChains constructs optimal delta chains from similarity scores
func buildDeltaChains(similarities []ObjectSimilarity, objects []Object) []DeltaChain {
	// Map to track which objects have been assigned to chains
	assigned := make(map[string]bool)

	// Resulting delta chains
	chains := make([]DeltaChain, 0)

	// Start with the most similar pairs and build chains
	for _, sim := range similarities {
		// Skip if both objects are already in chains
		if assigned[sim.Obj1Hash] && assigned[sim.Obj2Hash] {
			continue
		}

		// If similarity is too low, don't bother delta compressing
		if sim.Score < 0.3 { // Minimum similarity threshold
			continue
		}

		// Choose which object should be the base (prefer larger object)
		obj1 := findObjectByHash(objects, sim.Obj1Hash)
		obj2 := findObjectByHash(objects, sim.Obj2Hash)

		if obj1 == nil || obj2 == nil {
			continue
		}

		var baseHash, targetHash string
		if len(obj1.Data) >= len(obj2.Data) {
			baseHash = obj1.Hash
			targetHash = obj2.Hash
		} else {
			baseHash = obj2.Hash
			targetHash = obj1.Hash
		}

		// Check if either object is already a base in an existing chain
		existingChainIndex := -1
		for i, chain := range chains {
			if chain.BaseHash == baseHash || chain.BaseHash == targetHash {
				existingChainIndex = i
				break
			}
		}

		if existingChainIndex >= 0 {
			// Add to existing chain if it doesn't create a cycle
			chain := &chains[existingChainIndex]

			if chain.BaseHash == targetHash {
				// Need to flip the chain since our target is currently the base
				// This is complex and would need a complete chain rebuild
				// For simplicity we'll skip this case
				continue
			}

			// Add to the chain if not already in it
			alreadyInChain := false
			for _, delta := range chain.Deltas {
				if delta.TargetHash == targetHash {
					alreadyInChain = true
					break
				}
			}

			if !alreadyInChain && !assigned[targetHash] {
				chain.Deltas = append(chain.Deltas, DeltaDescriptor{
					TargetHash:      targetHash,
					SimilarityScore: sim.Score,
				})
				assigned[targetHash] = true
			}
		} else {
			// Start a new chain
			if !assigned[baseHash] && !assigned[targetHash] {
				chain := DeltaChain{
					BaseHash: baseHash,
					Deltas: []DeltaDescriptor{
						{
							TargetHash:      targetHash,
							SimilarityScore: sim.Score,
						},
					},
				}
				chains = append(chains, chain)
				assigned[baseHash] = true
				assigned[targetHash] = true
			}
		}
	}

	// Add standalone objects as their own chains
	for _, obj := range objects {
		if !assigned[obj.Hash] {
			chain := DeltaChain{
				BaseHash: obj.Hash,
				Deltas:   []DeltaDescriptor{},
			}
			chains = append(chains, chain)
			assigned[obj.Hash] = true
		}
	}

	return chains
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
