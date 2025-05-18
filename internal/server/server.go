package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/NahomAnteneh/vec/core"
	"github.com/NahomAnteneh/vec/internal/config"
	"github.com/NahomAnteneh/vec/utils"
)

// Constants for the server
const (
	// API Version
	APIVersion = "v1"

	// Default server settings
	DefaultPort        = 8080
	DefaultHost        = "0.0.0.0"
	DefaultReposDir    = "./repositories"
	DefaultAuthEnabled = false

	// Timeouts
	ReadTimeout  = 30 * time.Second
	WriteTimeout = 60 * time.Second
)

// Server errors
var (
	ErrRepoNotFound      = errors.New("repository not found")
	ErrRepoAlreadyExists = errors.New("repository already exists")
	ErrInvalidRequest    = errors.New("invalid request")
	ErrUnauthorized      = errors.New("unauthorized")
)

// ServerOptions contains configuration options for the server
type ServerOptions struct {
	Port        int
	Host        string
	ReposDir    string
	AuthEnabled bool
	Users       map[string]string // username -> password (hashed in production)
	Verbose     bool
	TLSCertFile string
	TLSKeyFile  string
}

// ServerStats contains server statistics
type ServerStats struct {
	StartTime       time.Time
	RequestsHandled int64
	ActiveRequests  int
	Repositories    int
	mutex           sync.Mutex
}

// Server represents a Vec server instance
type Server struct {
	Options  ServerOptions
	Stats    ServerStats
	router   *http.ServeMux
	server   *http.Server
	repoLock sync.RWMutex
}

// NewServer creates a new Vec server with default options
func NewServer() *Server {
	return &Server{
		Options: ServerOptions{
			Port:        DefaultPort,
			Host:        DefaultHost,
			ReposDir:    DefaultReposDir,
			AuthEnabled: DefaultAuthEnabled,
			Users:       make(map[string]string),
			Verbose:     false,
		},
		Stats: ServerStats{
			StartTime: time.Now(),
		},
		router: http.NewServeMux(),
	}
}

// Configure sets up the server with the given options
func (s *Server) Configure(options ServerOptions) {
	// Merge user options with existing options
	if options.Port != 0 {
		s.Options.Port = options.Port
	}
	if options.Host != "" {
		s.Options.Host = options.Host
	}
	if options.ReposDir != "" {
		s.Options.ReposDir = options.ReposDir
	}
	
	// Set other options
	s.Options.AuthEnabled = options.AuthEnabled
	if options.Users != nil {
		s.Options.Users = options.Users
	}
	s.Options.Verbose = options.Verbose
	s.Options.TLSCertFile = options.TLSCertFile
	s.Options.TLSKeyFile = options.TLSKeyFile
}

// Init initializes the server
func (s *Server) Init() error {
	// Create repositories directory if it doesn't exist
	if err := os.MkdirAll(s.Options.ReposDir, 0755); err != nil {
		return fmt.Errorf("failed to create repositories directory: %w", err)
	}

	// Register routes
	s.registerRoutes()

	// Initialize the HTTP server
	s.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", s.Options.Host, s.Options.Port),
		Handler:      s.logMiddleware(s.router),
		ReadTimeout:  ReadTimeout,
		WriteTimeout: WriteTimeout,
	}

	return nil
}

// Start starts the server
func (s *Server) Start() error {
	// Log server start
	log.Printf("Vec server starting on %s:%d", s.Options.Host, s.Options.Port)
	log.Printf("Repositories directory: %s", s.Options.ReposDir)
	log.Printf("Authentication enabled: %t", s.Options.AuthEnabled)

	// Start HTTP server with or without TLS
	var err error
	if s.Options.TLSCertFile != "" && s.Options.TLSKeyFile != "" {
		err = s.server.ListenAndServeTLS(s.Options.TLSCertFile, s.Options.TLSKeyFile)
	} else {
		err = s.server.ListenAndServe()
	}

	// Handle server errors
	if err != http.ErrServerClosed {
		return fmt.Errorf("server failed: %w", err)
	}

	return nil
}

// Stop gracefully stops the server
func (s *Server) Stop(ctx context.Context) error {
	log.Println("Shutting down server...")
	return s.server.Shutdown(ctx)
}

// GetRepoPath returns the absolute path to a repository
func (s *Server) GetRepoPath(repoName string) string {
	return filepath.Join(s.Options.ReposDir, repoName)
}

// RepoExists checks if a repository exists
func (s *Server) RepoExists(repoName string) bool {
	repoPath := s.GetRepoPath(repoName)
	vecDir := filepath.Join(repoPath, ".vec")
	_, err := os.Stat(vecDir)
	return err == nil
}

// CreateRepo initializes a new repository on the server
func (s *Server) CreateRepo(repoName string, bare bool) error {
	s.repoLock.Lock()
	defer s.repoLock.Unlock()

	// Check if repository already exists
	if s.RepoExists(repoName) {
		return ErrRepoAlreadyExists
	}

	// Create repository directory
	repoPath := s.GetRepoPath(repoName)
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return fmt.Errorf("failed to create repository directory: %w", err)
	}

	// Initialize Vec repository
	if err := utils.InitVecRepo(repoPath, bare); err != nil {
		// Clean up on failure
		os.RemoveAll(repoPath)
		return fmt.Errorf("failed to initialize repository: %w", err)
	}

	log.Printf("Created new repository: %s", repoName)
	s.Stats.mutex.Lock()
	s.Stats.Repositories++
	s.Stats.mutex.Unlock()
	
	return nil
}

// DeleteRepo deletes a repository
func (s *Server) DeleteRepo(repoName string) error {
	s.repoLock.Lock()
	defer s.repoLock.Unlock()

	// Check if repository exists
	if !s.RepoExists(repoName) {
		return ErrRepoNotFound
	}

	// Delete repository directory
	repoPath := s.GetRepoPath(repoName)
	if err := os.RemoveAll(repoPath); err != nil {
		return fmt.Errorf("failed to delete repository: %w", err)
	}

	log.Printf("Deleted repository: %s", repoName)
	s.Stats.mutex.Lock()
	s.Stats.Repositories--
	s.Stats.mutex.Unlock()
	
	return nil
}

// ListRepos returns a list of all repositories
func (s *Server) ListRepos() ([]string, error) {
	s.repoLock.RLock()
	defer s.repoLock.RUnlock()

	entries, err := os.ReadDir(s.Options.ReposDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read repositories directory: %w", err)
	}

	var repos []string
	for _, entry := range entries {
		if entry.IsDir() {
			repoPath := filepath.Join(s.Options.ReposDir, entry.Name())
			vecDir := filepath.Join(repoPath, ".vec")
			if _, err := os.Stat(vecDir); err == nil {
				repos = append(repos, entry.Name())
			}
		}
	}

	return repos, nil
}

// JSON response helper
func writeJSONResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	
	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Printf("Error encoding JSON response: %v", err)
		}
	}
}

// Error response helper
func writeErrorResponse(w http.ResponseWriter, status int, err error) {
	errResponse := map[string]string{"error": err.Error()}
	writeJSONResponse(w, status, errResponse)
}

// Middleware for logging requests
func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		// Update stats
		s.Stats.mutex.Lock()
		s.Stats.RequestsHandled++
		s.Stats.ActiveRequests++
		s.Stats.mutex.Unlock()
		
		// Log request
		if s.Options.Verbose {
			log.Printf("Request: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		}
		
		// Process request
		next.ServeHTTP(w, r)
		
		// Update active requests
		s.Stats.mutex.Lock()
		s.Stats.ActiveRequests--
		s.Stats.mutex.Unlock()
		
		// Log completion
		if s.Options.Verbose {
			log.Printf("Completed %s %s in %v", r.Method, r.URL.Path, time.Since(start))
		}
	})
} 