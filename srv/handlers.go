package srv

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"srv.exe.dev/db/dbgen"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Server) HandleIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(s.StaticDir, "index.html"))
}

type StatusResponse struct {
	RsyncAvailable bool   `json:"rsync_available"`
	RsyncPath      string `json:"rsync_path"`
	RsyncVersion   string `json:"rsync_version"`
	WorkDir        string `json:"work_dir"`
	RunningJobs    int    `json:"running_jobs"`
}

func (s *Server) HandleStatus(w http.ResponseWriter, r *http.Request) {
	resp := StatusResponse{
		RsyncAvailable: s.RsyncPath != "",
		RsyncPath:      s.RsyncPath,
		WorkDir:        s.WorkDir,
		RunningJobs:    s.JobManager.RunningCount(),
	}
	
	if s.RsyncPath != "" {
		out, err := exec.Command(s.RsyncPath, "--version").Output()
		if err == nil {
			lines := strings.Split(string(out), "\n")
			if len(lines) > 0 {
				resp.RsyncVersion = strings.TrimSpace(lines[0])
			}
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type FileEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

func (s *Server) HandleBrowse(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		relPath = "."
	}
	
	// Security: ensure path is within workdir
	fullPath := filepath.Join(s.WorkDir, relPath)
	fullPath, err := filepath.Abs(fullPath)
	if err != nil || !strings.HasPrefix(fullPath, s.WorkDir) {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	var files []FileEntry
	
	// Add parent directory if not at root
	if fullPath != s.WorkDir {
		parentRel, _ := filepath.Rel(s.WorkDir, filepath.Dir(fullPath))
		files = append(files, FileEntry{
			Name:  "..",
			Path:  parentRel,
			IsDir: true,
		})
	}
	
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		entryPath, _ := filepath.Rel(s.WorkDir, filepath.Join(fullPath, entry.Name()))
		files = append(files, FileEntry{
			Name:  entry.Name(),
			Path:  entryPath,
			IsDir: entry.IsDir(),
			Size:  info.Size(),
		})
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"current_path": relPath,
		"full_path":    fullPath,
		"entries":      files,
	})
}

type SSHHost struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	User     string `json:"user"`
	Port     string `json:"port"`
}

func (s *Server) HandleSSHHosts(w http.ResponseWriter, r *http.Request) {
	hosts := parseSSHConfig()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(hosts)
}

func parseSSHConfig() []SSHHost {
	var hosts []SSHHost
	
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return hosts
	}
	
	configPath := filepath.Join(homeDir, ".ssh", "config")
	file, err := os.Open(configPath)
	if err != nil {
		return hosts
	}
	defer file.Close()
	
	var current *SSHHost
	scanner := bufio.NewScanner(file)
	
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		
		key := strings.ToLower(parts[0])
		value := strings.Join(parts[1:], " ")
		
		switch key {
		case "host":
			if current != nil && current.Name != "" && current.Name != "*" {
				hosts = append(hosts, *current)
			}
			current = &SSHHost{Name: value}
		case "hostname":
			if current != nil {
				current.Hostname = value
			}
		case "user":
			if current != nil {
				current.User = value
			}
		case "port":
			if current != nil {
				current.Port = value
			}
		}
	}
	
	if current != nil && current.Name != "" && current.Name != "*" {
		hosts = append(hosts, *current)
	}
	
	return hosts
}

func (s *Server) HandleHistory(w http.ResponseWriter, r *http.Request) {
	q := dbgen.New(s.DB)
	history, err := q.ListRsyncHistory(r.Context(), 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	// Update status from job manager for running jobs
	for i := range history {
		if job := s.JobManager.Get(history[i].ID); job != nil {
			job.mu.RLock()
			history[i].Status = job.Status
			job.mu.RUnlock()
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

type RunRequest struct {
	Source      string   `json:"source"`
	Destination string   `json:"destination"`
	Options     []string `json:"options"`
}

func (s *Server) HandleRun(w http.ResponseWriter, r *http.Request) {
	if s.RsyncPath == "" {
		http.Error(w, "rsync not available", http.StatusServiceUnavailable)
		return
	}
	
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	// Build command
	args := append(req.Options, req.Source, req.Destination)
	fullCmd := fmt.Sprintf("rsync %s", strings.Join(args, " "))
	
	optionsJSON, _ := json.Marshal(req.Options)
	
	q := dbgen.New(s.DB)
	history, err := q.CreateRsyncHistory(r.Context(), dbgen.CreateRsyncHistoryParams{
		Source:      req.Source,
		Destination: req.Destination,
		Options:     string(optionsJSON),
		FullCommand: fullCmd,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	// Start the job
	go s.JobManager.RunJob(history.ID, args)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func (s *Server) HandleCancel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	
	if err := s.JobManager.Cancel(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	
	w.WriteHeader(http.StatusOK)
}

func (s *Server) HandleDeleteHistory(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	
	q := dbgen.New(s.DB)
	if err := q.DeleteRsyncHistory(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	w.WriteHeader(http.StatusOK)
}

func (s *Server) HandleGetJob(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	
	q := dbgen.New(s.DB)
	history, err := q.GetRsyncHistory(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	
	// Get live output if job is running
	var liveOutput []string
	if job := s.JobManager.Get(id); job != nil {
		job.mu.RLock()
		liveOutput = make([]string, len(job.Output))
		copy(liveOutput, job.Output)
		history.Status = job.Status
		job.mu.RUnlock()
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"history":     history,
		"live_output": liveOutput,
	})
}

func (s *Server) HandleJobWebSocket(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()
	
	job := s.JobManager.Get(id)
	if job == nil {
		// Job not running, send stored output
		q := dbgen.New(s.DB)
		history, err := q.GetRsyncHistory(r.Context(), id)
		if err == nil && history.Output != nil {
			conn.WriteJSON(map[string]any{
				"type":   "output",
				"data":   *history.Output,
				"status": history.Status,
			})
		}
		conn.WriteJSON(map[string]any{"type": "done", "status": history.Status})
		return
	}
	
	// Subscribe to live output
	ch := job.Subscribe()
	defer job.Unsubscribe(ch)
	
	// Send existing output
	job.mu.RLock()
	for _, line := range job.Output {
		conn.WriteJSON(map[string]any{"type": "output", "data": line})
	}
	status := job.Status
	job.mu.RUnlock()
	
	if status != "running" {
		conn.WriteJSON(map[string]any{"type": "done", "status": status})
		return
	}
	
	// Stream new output
	for line := range ch {
		if line == "__DONE__" {
			job.mu.RLock()
			status := job.Status
			job.mu.RUnlock()
			conn.WriteJSON(map[string]any{"type": "done", "status": status})
			return
		}
		if err := conn.WriteJSON(map[string]any{"type": "output", "data": line}); err != nil {
			return
		}
	}
}

// JobManager methods

func (jm *JobManager) RunningCount() int {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	count := 0
	for _, job := range jm.jobs {
		job.mu.RLock()
		if job.Status == "running" {
			count++
		}
		job.mu.RUnlock()
	}
	return count
}

func (jm *JobManager) Get(id int64) *Job {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	return jm.jobs[id]
}

func (jm *JobManager) Cancel(id int64) error {
	jm.mu.RLock()
	job := jm.jobs[id]
	jm.mu.RUnlock()
	
	if job == nil {
		return fmt.Errorf("job not found")
	}
	
	close(job.cancel)
	return nil
}

func (jm *JobManager) RunJob(id int64, args []string) {
	job := &Job{
		ID:          id,
		Status:      "running",
		subscribers: make(map[chan string]struct{}),
		cancel:      make(chan struct{}),
	}
	
	jm.mu.Lock()
	jm.jobs[id] = job
	jm.mu.Unlock()
	
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-job.cancel
		cancel()
	}()
	
	startTime := time.Now()
	q := dbgen.New(jm.server.DB)
	q.UpdateRsyncRunning(context.Background(), dbgen.UpdateRsyncRunningParams{
		StartedAt: &startTime,
		ID:        id,
	})
	
	cmd := exec.CommandContext(ctx, jm.server.RsyncPath, args...)
	cmd.Dir = jm.server.WorkDir
	
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	
	if err := cmd.Start(); err != nil {
		job.addOutput(fmt.Sprintf("Error starting rsync: %v", err))
		job.finish("failed", 1)
		jm.saveJob(job, startTime)
		return
	}
	
	// Read output
	go jm.readOutput(job, stdout)
	go jm.readOutput(job, stderr)
	
	err := cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	
	status := "completed"
	if ctx.Err() != nil {
		status = "cancelled"
	} else if exitCode != 0 {
		status = "failed"
	}
	
	job.finish(status, exitCode)
	jm.saveJob(job, startTime)
}

func (jm *JobManager) readOutput(job *Job, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		job.addOutput(scanner.Text())
	}
}

func (jm *JobManager) saveJob(job *Job, startTime time.Time) {
	job.mu.RLock()
	output := strings.Join(job.Output, "\n")
	status := job.Status
	job.mu.RUnlock()
	
	endTime := time.Now()
	exitCode := int64(0)
	
	q := dbgen.New(jm.server.DB)
	q.UpdateRsyncStatus(context.Background(), dbgen.UpdateRsyncStatusParams{
		Status:      status,
		ExitCode:    &exitCode,
		Output:      &output,
		StartedAt:   &startTime,
		CompletedAt: &endTime,
		ID:          job.ID,
	})
	
	// Keep job in memory for a bit for late subscribers
	time.AfterFunc(5*time.Minute, func() {
		jm.mu.Lock()
		delete(jm.jobs, job.ID)
		jm.mu.Unlock()
	})
}

// Job methods

func (j *Job) Subscribe() chan string {
	ch := make(chan string, 100)
	j.mu.Lock()
	j.subscribers[ch] = struct{}{}
	j.mu.Unlock()
	return ch
}

func (j *Job) Unsubscribe(ch chan string) {
	j.mu.Lock()
	delete(j.subscribers, ch)
	j.mu.Unlock()
}

func (j *Job) addOutput(line string) {
	j.mu.Lock()
	j.Output = append(j.Output, line)
	for ch := range j.subscribers {
		select {
		case ch <- line:
		default:
		}
	}
	j.mu.Unlock()
}

func (j *Job) finish(status string, exitCode int) {
	j.mu.Lock()
	j.Status = status
	for ch := range j.subscribers {
		select {
		case ch <- "__DONE__":
		default:
		}
		close(ch)
	}
	j.subscribers = make(map[chan string]struct{})
	j.mu.Unlock()
}
