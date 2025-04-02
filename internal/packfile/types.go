package packfile

import "fmt"

// Constants
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

// PackFileHeader represents the header of a packfile
type PackFileHeader struct {
	Signature  [4]byte // Should be "PACK"
	Version    uint32  // Pack format version (currently 2)
	NumObjects uint32  // Number of objects in the pack
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
