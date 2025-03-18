# Changelog

All notable changes to the Vec client will be documented in this file.

## [Unreleased]

### Added
- Centralized HTTP client structure in `internal/remote/http`
- Standardized API endpoint naming conventions (pluralized resources)
- Improved error handling with specific error types
- Automatic retry logic for transient network errors
- Security improvements for handling auth tokens and sensitive data
- Comprehensive logging with sensitive information redaction
- Transition functions to help migrate existing code

### Changed
- Updated all direct HTTP requests to use the centralized client
- Modified fetch, push, and pull operations to use standardized HTTP functions
- Improved error messages to be more descriptive and helpful
- Standardized response handling across all remote operations

### Fixed
- Inconsistent error handling in HTTP requests
- Missing authentication headers in some requests
- Inconsistent timeout settings
- Non-standard API endpoint naming

### Security
- Improved handling of authentication tokens
- Added security headers to all requests
- Implemented uniform logging that redacts sensitive information
- Standardized error responses to avoid information leakage 