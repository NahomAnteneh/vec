package cmd

import "fmt"

// ErrRepositoryExists is returned when a repository already exists.
type ErrRepositoryExists struct {
	Path string
}

func (e *ErrRepositoryExists) Error() string {
	return fmt.Sprintf("a Vec repository already exists in %s", e.Path)
}

// ErrInvalidDirectory is returned when the provided directory is invalid.
type ErrInvalidDirectory struct {
	Path string
	Err  error
}

func (e *ErrInvalidDirectory) Error() string {
	return fmt.Sprintf("invalid directory '%s': %v", e.Path, e.Err)
}
