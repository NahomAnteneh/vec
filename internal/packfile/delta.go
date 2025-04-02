package packfile

import (
	"bytes"
	"errors"
	"fmt"
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
	// This is a simplified implementation
	// A real implementation would use metrics like similarity, size, and access patterns

	// Create a map of objects by type for easier lookup
	objectsByType := make(map[ObjectType][]Object)
	for _, obj := range objects {
		objectsByType[obj.Type] = append(objectsByType[obj.Type], obj)
	}

	result := make([]Object, 0, len(objects))

	// Only delta compress objects of the same type
	for _, typeObjects := range objectsByType {
		// Sort objects by size (descending) to prefer large objects as bases
		// This is a simple heuristic - real systems use more sophisticated approaches
		objCount := len(typeObjects)

		// If fewer than 2 objects of this type, no delta compression possible
		if objCount < 2 {
			result = append(result, typeObjects...)
			continue
		}

		// Choose first (largest) object as base and store as is
		baseObj := typeObjects[0]
		result = append(result, baseObj)

		// Delta compress remaining objects against the base
		for i := 1; i < objCount; i++ {
			targetObj := typeObjects[i]

			// Create delta
			delta, err := createDelta(baseObj.Data, targetObj.Data)
			if err != nil {
				return nil, fmt.Errorf("failed to create delta: %w", err)
			}

			// If delta is smaller than original, use it
			if len(delta) < len(targetObj.Data) {
				deltaObj := Object{
					Hash: targetObj.Hash,
					Type: OBJ_DELTA,
					Data: delta,
				}
				result = append(result, deltaObj)
			} else {
				// Otherwise store the original object
				result = append(result, targetObj)
			}
		}
	}

	return result, nil
}
