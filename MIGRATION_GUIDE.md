# Command Migration Guide

This guide outlines the steps to migrate Vec CLI commands to use the Repository context pattern.

## Migration Steps

For each command file in the `cmd/` directory, follow these steps:

### 1. Create a Handler Function

Replace the cobra.Command's RunE function with a dedicated handler:

```go
// OldCommand
var oldCmd = &cobra.Command{
    RunE: func(cmd *cobra.Command, args []string) error {
        repoRoot, err := utils.GetVecRoot()
        if err != nil {
            return err
        }
        return doSomething(repoRoot, args)
    },
}

// NewCommand
func CommandHandler(repo *core.Repository, args []string) error {
    // Implementation using repo instead of repoRoot
    return nil
}
```

### 2. Use the Command Factory

Replace the command instantiation with the factory methods:

```go
// Old init function
func init() {
    rootCmd.AddCommand(oldCmd)
    oldCmd.Flags().StringP("flag", "f", "", "Description")
}

// New init function
func init() {
    cmd := NewRepoCommand(
        "command",
        "Command description",
        CommandHandler,
    )
    cmd.Flags().StringP("flag", "f", "", "Description")
    rootCmd.AddCommand(cmd)
}
```

### 3. Use Repository Methods

Replace utility functions with Repository methods:

```go
// Old code
headCommit, err := utils.GetHeadCommit(repoRoot)
branch, err := utils.GetCurrentBranch(repoRoot)

// New code
headCommit, err := repo.ReadHead()
branch, err := repo.GetCurrentBranch()
```

### 4. Use Standard Error Handling

Replace ad-hoc error formatting with standard errors:

```go
// Old error handling
return fmt.Errorf("failed to read config: %w", err)

// New error handling
return core.ConfigError("failed to read config", err)
```

### 5. Update Helper Functions

Convert internal helper functions to use Repository:

```go
// Old helper
func helperFunction(repoRoot string, arg string) error {
    // Implementation
}

// New helper
func helperFunction(repo *core.Repository, arg string) error {
    // Implementation
}
```

### 6. Fix Command References (if needed)

For commands that access their own command instance:

```go
// Declare a package variable
var commandCmd *cobra.Command

func init() {
    // Create the command
    commandCmd = NewRepoCommand(
        "command",
        "Command description",
        CommandHandler,
    )
    // ...
}

// Use it in the handler
func CommandHandler(repo *core.Repository, args []string) error {
    cmd := commandCmd
    // Access flags and other command properties
}
```

## Migration Priority

Migrate commands in this order:

1. **Simple commands** (log, branch, add) - Use as templates
2. **Core commands** (status, commit) - Essential functionality
3. **Complex commands** (remote, config) - Tackle once patterns are established

## Testing Strategy

After migrating each command:

1. Test basic command functionality
2. Test error handling
3. Test with different inputs/flags
4. Test flag functionality

## Benefits

This migration:

1. Reduces code duplication
2. Improves error handling consistency
3. Makes command code more maintainable
4. Provides better separation of concerns 