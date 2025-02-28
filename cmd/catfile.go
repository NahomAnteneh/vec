// cmd/catfile.go
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
	Args:  cobra.ExactArgs(1), // Exactly one argument: the object hash.
	RunE: func(cmd *cobra.Command, args []string) error {
		repoRoot, err := utils.GetVecRoot()
		if err != nil {
			return err
		}

		objectHash := args[0]

		// Check that the object hash is valid (at a basic level - correct length).
		if len(objectHash) != 64 {
			return fmt.Errorf("invalid object hash: %s", objectHash)
		}

		// Check flags.  Exactly one of -p, -t, or -s MUST be provided.
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

	// Check if the object file exists.
	if !utils.FileExists(objectPath) {
		return fmt.Errorf("object not found: %s", objectHash)
	}

	// Read object content
	objectContent, err := os.ReadFile(objectPath)
	if err != nil {
		return fmt.Errorf("failed to read the object file: %w", err)
	}

	//get object type
	objectType, err := getObjectType(objectContent)
	if err != nil {
		return err
	}

	// Separate header and content
	headerEnd := bytes.IndexByte(objectContent, '\n')
	if headerEnd == -1 {
		return fmt.Errorf("invalid object format: missing header")
	}
	content := objectContent[headerEnd+1:]

	switch objectType {
	case "blob":
		fmt.Print(string(content))
	case "tree":
		tree, err := objects.GetTree(repoRoot, objectHash)
		if err != nil {
			return err
		}
		for _, entry := range tree.Entries {
			fmt.Printf("%d %s %s\t%s\n", entry.Mode, entry.Type, entry.Hash, entry.Name)
		}
	case "commit":
		commit, err := objects.GetCommit(repoRoot, objectHash)
		if err != nil {
			return err
		}
		printCommit(commit)
	default:
		return fmt.Errorf("invalid object type")
	}

	return nil
}
func catFileType(repoRoot, objectHash string) error {
	objectPath := objects.GetObjectPath(repoRoot, objectHash)

	// Check if the object file exists.
	if !utils.FileExists(objectPath) {
		return fmt.Errorf("object not found: %s", objectHash)
	}

	// Read object content
	objectContent, err := os.ReadFile(objectPath)
	if err != nil {
		return fmt.Errorf("failed to read the object file: %w", err)
	}

	objectType, err := getObjectType(objectContent)
	if err != nil {
		return err
	}

	fmt.Println(objectType)
	return nil
}
func catFileSize(repoRoot, objectHash string) error {
	objectPath := objects.GetObjectPath(repoRoot, objectHash)

	// Check if the object file exists.
	if !utils.FileExists(objectPath) {
		return fmt.Errorf("object not found: %s", objectHash)
	}

	// Read object content
	objectContent, err := os.ReadFile(objectPath)
	if err != nil {
		return fmt.Errorf("failed to read the object file: %w", err)
	}
	// Separate header and content
	headerEnd := bytes.IndexByte(objectContent, '\n')
	if headerEnd == -1 {
		return fmt.Errorf("invalid object format: missing header")
	}
	content := objectContent[headerEnd+1:]

	fmt.Println(len(content))
	return nil
}

// getObjectType determines the type of an object based on its content.
func getObjectType(content []byte) (string, error) {
	headerEnd := bytes.IndexByte(content, '\n')
	if headerEnd == -1 {
		return "", fmt.Errorf("invalid object format: missing header")
	}
	header := string(content[:headerEnd])
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid object format: invalid header")
	}
	return parts[0], nil // Return object type
}
func printCommit(commit *objects.Commit) {

	fmt.Printf("tree %s\n", commit.Tree)
	for _, parent := range commit.Parents {
		fmt.Printf("parent %s\n", parent)
	}
	fmt.Printf("author %s %d\n", commit.Author, commit.Timestamp)
	fmt.Println()
	fmt.Println(commit.Message)
}

func init() {
	rootCmd.AddCommand(catFileCmd)
	catFileCmd.Flags().BoolP("pretty-print", "p", false, "Pretty-print object's content")
	catFileCmd.Flags().BoolP("type", "t", false, "Show object's type")
	catFileCmd.Flags().BoolP("size", "s", false, "Show object's size")
}
