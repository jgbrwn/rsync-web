package srv

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"srv.exe.dev/db"
)

type Server struct {
	DB           *sql.DB
	TemplatesDir string
	StaticDir    string
	WorkDir      string
	RsyncPath    string
	JobManager   *JobManager
}

type JobManager struct {
	mu      sync.RWMutex
	jobs    map[int64]*Job
	server  *Server
}

type Job struct {
	ID          int64
	mu          sync.RWMutex
	Output      []string
	Status      string
	subscribers map[chan string]struct{}
	cancel      chan struct{}
}

func New(dbPath, workDir string) (*Server, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(thisFile)
	
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
	}
	
	rsyncPath, _ := exec.LookPath("rsync")
	
	srv := &Server{
		TemplatesDir: filepath.Join(baseDir, "templates"),
		StaticDir:    filepath.Join(baseDir, "static"),
		WorkDir:      workDir,
		RsyncPath:    rsyncPath,
	}
	
	srv.JobManager = &JobManager{
		jobs:   make(map[int64]*Job),
		server: srv,
	}
	
	if err := srv.setUpDatabase(dbPath); err != nil {
		return nil, err
	}
	return srv, nil
}

func (s *Server) setUpDatabase(dbPath string) error {
	wdb, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}
	s.DB = wdb
	if err := db.RunMigrations(wdb); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

func (s *Server) Serve(addr string) error {
	mux := http.NewServeMux()
	
	// Pages
	mux.HandleFunc("GET /{$}", s.HandleIndex)
	
	// API
	mux.HandleFunc("GET /api/status", s.HandleStatus)
	mux.HandleFunc("GET /api/browse", s.HandleBrowse)
	mux.HandleFunc("GET /api/ssh-hosts", s.HandleSSHHosts)
	mux.HandleFunc("GET /api/history", s.HandleHistory)
	mux.HandleFunc("POST /api/run", s.HandleRun)
	mux.HandleFunc("POST /api/cancel/{id}", s.HandleCancel)
	mux.HandleFunc("DELETE /api/history/{id}", s.HandleDeleteHistory)
	mux.HandleFunc("GET /api/job/{id}", s.HandleGetJob)
	
	// WebSocket
	mux.HandleFunc("/ws/job/{id}", s.HandleJobWebSocket)
	
	// Static
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(s.StaticDir))))
	
	slog.Info("starting rsync-web", "addr", addr, "workdir", s.WorkDir, "rsync", s.RsyncPath)
	return http.ListenAndServe(addr, mux)
}
