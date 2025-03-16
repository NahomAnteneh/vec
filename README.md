# Vec - Version Control System

Vec is a lightweight version control system designed to work with the Vec server. It provides basic version control functionality similar to Git but with a simpler approach and built-in authentication support.

## Features

- Basic version control operations (init, add, commit, status)
- Remote repository operations (clone, push, pull)
- Authentication support for secure remote operations
- Custom HTTP headers for advanced integration

## Installation

```bash
# Clone the repository
git clone https://github.com/NahomAnteneh/vec.git

# Build the binary
cd vec
go build -o vec

# Move to a directory in your PATH (optional)
sudo mv vec /usr/local/bin/
```

## Basic Usage

### Initialize a repository

```bash
vec init
```

### Add files to staging

```bash
vec add <file1> <file2> ...
```

### Commit changes

```bash
vec commit -m "Your commit message"
```

### Check status

```bash
vec status
```

## Remote Operations

### Add a remote repository

```bash
vec remote add origin https://example.com/repo
```

With authentication:

```bash
vec remote add origin https://example.com/repo --auth "your-auth-token"
```

### Clone a repository

```bash
vec clone https://example.com/repo
```

With authentication:

```bash
vec clone https://example.com/repo --auth "your-auth-token"
```

### Push changes to remote

```bash
vec push origin main
```

### Pull changes from remote

```bash
vec pull origin main
```

## Authentication

Vec supports authentication for secure remote operations. You can set authentication tokens in several ways:

### Setting authentication during remote addition

```bash
vec remote add origin https://example.com/repo --auth "your-auth-token"
```

### Setting authentication for an existing remote

```bash
vec config remote-auth origin "your-auth-token"
```

### Setting custom HTTP headers

```bash
vec config remote-header origin "Header-Name" "header-value"
```

## Configuration

Vec stores configuration in the `.vec/config` file in your repository. This includes remote URLs and authentication information.

## License

[MIT License](LICENSE) 