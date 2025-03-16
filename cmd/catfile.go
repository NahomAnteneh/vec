package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/NahomAnteneh/vec/internal/objects"
	"github.com/NahomAnteneh/vec/utils"
	"github.com/spf13/cobra"
)

var catFileCmd = &cobra.Command{
	Use:   "cat-file (-p <hash> | -t <hash> | -s <hash>)",
	Short: "Provide content or type and size information for repository objects",
	Args:  cobra.ExactArgs(1), // Exactly one argument: the object hash
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}

		objectHash := args[0]

		// Check that the object hash is valid (basic length check for SHA-256)
		if len(objectHash) != 64 {
			return fmt.Errorf("invalid object hash: %s", objectHash)
		}

		// Check flags: exactly one of -p, -t, or -s must be provided
		prettyPrint, _ := cmd.Flags().GetBool("pretty-print")
		objectType, _ := cmd.Flags().GetBool("type")
		objectSize, _ := cmd.Flags().GetBool("size")

		flagCount := 0
		if prettyPrint {
			flagCount++
		}
		if objectType {
			flagCount++
		}
		if objectSize {
			flagCount++
		}

		if flagCount != 1 {
			return fmt.Errorf("exactly one of -p, -t, or -s must be specified")
		}

		if prettyPrint {
			return catFilePrettyPrint(repoRoot, objectHash)
		} else if objectType {
			return catFileType(repoRoot, objectHash)
		} else {
			return catFileSize(repoRoot, objectHash)
		}
	},
}

func catFilePrettyPrint(repoRoot, objectHash string) error {
	objectPath := objects.GetObjectPath(repoRoot, objectHash)
	if !utils.FileExists(objectPath) {
		return fmt.Errorf("object not found: %s", objectHash)
	}

	// Read object content
	objectContent, err := os.ReadFile(objectPath)
	if err != nil {
		return fmt.Errorf("failed to read object file: %w", err)
	}

	// Get object type
	objectType, err := getObjectType(objectContent)
	if err != nil {
		return err
	}

	// Separate header and content using null byte (\x00)
	headerEnd := bytes.IndexByte(objectContent, '\x00')
	if headerEnd == -1 {
		return fmt.Errorf("invalid object format: missing header delimiter")
	}
	content := objectContent[headerEnd+1:]

	switch objectType {
	case "blob":
		fmt.Print(string(content))
	case "tree":
		tree, err := objects.GetTree(repoRoot, objectHash)
		if err != nil {
			return fmt.Errorf("failed to get tree: %w", err)
		}
		for _, entry := range tree.Entries {
			fmt.Printf("%06o %s %s\t%s\n", entry.Mode, entry.Type, entry.Hash, entry.Name)
		}
	case "commit":
		commit, err := objects.GetCommit(repoRoot, objectHash)
		if err != nil {
			return fmt.Errorf("failed to get commit: %w", err)
		}
		printCommit(commit)
	default:
		return fmt.Errorf("invalid object type: %s", objectType)
	}

	return nil
}

func catFileType(repoRoot, objectHash string) error {
	objectPath := objects.GetObjectPath(repoRoot, objectHash)
	if !utils.FileExists(objectPath) {
		return fmt.Errorf("object not found: %s", objectHash)
	}

	// Read object content
	objectContent, err := os.ReadFile(objectPath)
	if err != nil {
		return fmt.Errorf("failed to read object file: %w", err)
	}

	// Get object type
	objectType, err := getObjectType(objectContent)
	if err != nil {
		return err
	}

	fmt.Println(objectType)
	return nil
}

func catFileSize(repoRoot, objectHash string) error {
	objectPath := objects.GetObjectPath(repoRoot, objectHash)
	if !utils.FileExists(objectPath) {
		return fmt.Errorf("object not found: %s", objectHash)
	}

	// Read object content
	objectContent, err := os.ReadFile(objectPath)
	if err != nil {
		return fmt.Errorf("failed to read object file: %w", err)
	}

	// Parse header to get size
	headerEnd := bytes.IndexByte(objectContent, '\x00')
	if headerEnd == -1 {
		return fmt.Errorf("invalid object format: missing header delimiter")
	}
	header := string(objectContent[:headerEnd])
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid object format: invalid header: %s", header)
	}
	sizeStr := parts[1] // Size is the second part of the header
	fmt.Println(sizeStr)
	return nil
}

// getObjectType determines the type of an object based on its content.
func getObjectType(content []byte) (string, error) {
	headerEnd := bytes.IndexByte(content, '\x00')
	if headerEnd == -1 {
		return "", fmt.Errorf("invalid object format: missing header delimiter")
	}
	header := string(content[:headerEnd])
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid object format: invalid header: %s", header)
	}
	return parts[0], nil // Return object type (e.g., "tree", "blob", "commit")
}

// printCommit displays a commit object in a human-readable format.
func printCommit(commit *objects.Commit) {
	fmt.Printf("tree:    %s\n", commit.Tree)
	for _, parent := range commit.Parents {
		fmt.Printf("parent:  %s\n", parent)
	}
	fmt.Printf("author:  %s %d\n", commit.Author, commit.Timestamp)
	fmt.Println() // Extra newline before message
	fmt.Println(commit.Message)
}

func init() {
	rootCmd.AddCommand(catFileCmd)
	catFileCmd.Flags().BoolP("pretty-print", "p", false, "Pretty-print object's content")
	catFileCmd.Flags().BoolP("type", "t", false, "Show object's type")
	catFileCmd.Flags().BoolP("size", "s", false, "Show object's size")
}
