# Centralized HTTP Client for Vec

This package implements a centralized HTTP client for all remote operations in Vec. It provides a standardized way to make HTTP requests to remote repositories, handle authentication, retry logic, and error handling.

## Overview

The Vec HTTP client centralizes all remote communication to ensure:

1. Consistent request handling
2. Standardized error management
3. Proper authentication
4. Automatic retries for transient network issues
5. Secure handling of sensitive information
6. Proper logging with sensitive data redaction
7. RESTful API endpoint conventions

## Key Components

### Client

The `Client` struct maintains the HTTP connection state and configuration. It provides methods for creating and executing requests.

```go
client := http.NewClient(remoteURL, remoteName, cfg)
client.SetVerbose(true) // Enable verbose logging
```

### ResponseResult

The `ResponseResult` struct wraps HTTP responses and errors to provide a unified error handling approach.

### API Endpoints

The package defines standardized endpoint names as constants:

- `EndpointRefs` - For fetching references
- `EndpointBranches` - For branch operations
- `EndpointNegotiations` - For object negotiation
- `EndpointPackfiles` - For packfile operations
- `EndpointPushes` - For push operations

### High-Level Functions

The package provides high-level functions for common operations:

- `FetchRefs()` - Retrieves all references from a remote
- `GetBranchCommit()` - Gets the commit hash for a specific branch
- `Negotiate()` - Determines which objects need to be transferred
- `FetchPackfile()` - Retrieves a packfile containing objects
- `Push()` - Sends objects and updates references on a remote

### Error Handling

Predefined error types ensure consistent error handling:

- `ErrNetworkError` - For network connectivity issues
- `ErrAuthenticationFailed` - For authentication problems
- `ErrResourceNotFound` - For 404 responses
- `ErrBadRequest` - For client-side errors
- `ErrServerError` - For server-side errors

## Transition Module

The package includes transition helpers in `transition.go` that make it easy to migrate existing code to use the centralized client:

```go
// Instead of directly implementing HTTP requests
refs, err := vechttp.FetchRemoteRefs(remoteURL, remoteName, cfg)

// Or for more control
client := vechttp.GetClient(remoteURL, remoteName, cfg)
refs, err := client.FetchRefs()
```

## Benefits

1. **Maintainability**: All HTTP logic is in one place, making updates easier
2. **Security**: Authentication and sensitive data handling is standardized
3. **Reliability**: Consistent retry logic and error handling
4. **Testability**: Mock the HTTP client for testing without network access
5. **Flexibility**: Easy to add new features like request throttling or caching

## Usage Examples

### Fetching References

```go
client := http.NewClient(remoteURL, remoteName, cfg)
refs, err := client.FetchRefs()
if err != nil {
    // Handle error
}
// Use refs
```

### Negotiating Objects

```go
client := http.NewClient(remoteURL, remoteName, cfg)
missingObjects, err := client.Negotiate(remoteRefs, localRefs)
if err != nil {
    // Handle error
}
// Fetch missing objects
```

### Pushing Changes

```go
client := http.NewClient(remoteURL, remoteName, cfg)
result, err := client.Push(pushData, packfileBytes)
if err != nil {
    // Handle error
}
if !result.Success {
    // Handle push failure
}
// Push successful
``` 