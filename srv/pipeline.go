package srv

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"srv.exe.dev/db/dbgen"
)

// Preset is a named, one-click destination (e.g. "Movies" -> host:path/movies/).
type Preset struct {
	Name        string `json:"name"`
	Destination string `json:"destination"`
}

// Options configures the optional pipeline features. All are off by default.
type Options struct {
	Presets       []Preset // -preset NAME=DEST (repeatable)
	PresetOptions []string // -preset-opts: rsync options used by presets and /api/push
	PushRoot      string   // -push-root: directory (relative to workdir) that /api/push may send from
	WebhookURL    string   // -webhook: POSTed a JSON summary when a job finishes
	WebhookSecret string   // RSYNC_WEB_WEBHOOK_SECRET: HMAC-SHA256 key for the X-Rsync-Web-Signature header
}

// DefaultPresetOptions are safe "copy and keep going" flags: recursive, keep links/perms/times,
// don't try to set owner/group on the receiver, resumable, and never --delete.
var DefaultPresetOptions = []string{"-v", "-P", "-h", "-r", "-l", "-p", "-t", "-H", "--no-o", "--no-g", "--omit-dir-times", "--timeout=185"}

// normalizeOptions strips shell-style quotes that do nothing without a shell
// (--exclude='/temp/' would otherwise match a literal "'/temp/'" and exclude nothing)
// and drops exact duplicates (e.g. --timeout from both the field and custom options).
func normalizeOptions(opts []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if i := strings.Index(o, "="); i > 0 && strings.HasPrefix(o, "--") {
			o = o[:i+1] + unquote(o[i+1:])
		} else {
			o = unquote(o)
		}
		if seen[o] {
			continue
		}
		seen[o] = true
		out = append(out, o)
	}
	return out
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func (s *Server) presetOptions() []string {
	if len(s.Opts.PresetOptions) > 0 {
		return s.Opts.PresetOptions
	}
	return DefaultPresetOptions
}

func (s *Server) HandlePresets(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"presets":   s.Opts.Presets,
		"options":   s.presetOptions(),
		"push_root": s.Opts.PushRoot,
	})
}

// HandlePush is a narrow alternative to /api/run for automation: it can only send an existing
// direct child of PushRoot to a configured preset, with the preset options.
func (s *Server) HandlePush(w http.ResponseWriter, r *http.Request) {
	if s.RsyncPath == "" {
		http.Error(w, "rsync not available", http.StatusServiceUnavailable)
		return
	}
	if s.Opts.PushRoot == "" || len(s.Opts.Presets) == 0 {
		http.Error(w, "push is not configured (-push-root and -preset)", http.StatusNotFound)
		return
	}
	var req struct {
		Name   string `json:"name"`
		Preset string `json:"preset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var preset *Preset
	for i := range s.Opts.Presets {
		if strings.EqualFold(s.Opts.Presets[i].Name, req.Preset) {
			preset = &s.Opts.Presets[i]
		}
	}
	if preset == nil {
		http.Error(w, "unknown preset", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Name == "." || req.Name == ".." || strings.ContainsAny(req.Name, "/\\\x00") || strings.HasPrefix(req.Name, "-") {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	root := filepath.Join(s.WorkDir, s.Opts.PushRoot)
	if _, err := os.Lstat(filepath.Join(root, req.Name)); err != nil {
		http.Error(w, "no such file in push root", http.StatusNotFound)
		return
	}
	source := filepath.ToSlash(filepath.Join(s.Opts.PushRoot, req.Name))
	s.startJob(w, r, source, preset.Destination, s.presetOptions())
}

// startJob records a job in history, starts it and writes the history row as JSON.
func (s *Server) startJob(w http.ResponseWriter, r *http.Request, source, destination string, options []string) {
	options = normalizeOptions(options)
	args := append(append([]string{}, options...), source, destination)
	fullCmd := fmt.Sprintf("rsync %s", strings.Join(args, " "))
	optionsJSON, _ := json.Marshal(options)
	q := dbgen.New(s.DB)
	history, err := q.CreateRsyncHistory(r.Context(), dbgen.CreateRsyncHistoryParams{
		Source:      source,
		Destination: destination,
		Options:     string(optionsJSON),
		FullCommand: fullCmd,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	go s.JobManager.RunJob(history.ID, args)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

// notifyWebhook POSTs a signed summary of a finished job. Best effort, a few retries.
func (s *Server) notifyWebhook(id int64, status string, exitCode int) {
	if s.Opts.WebhookURL == "" {
		return
	}
	h, err := dbgen.New(s.DB).GetRsyncHistory(context.Background(), id)
	if err != nil {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"id": id, "status": status, "exit_code": exitCode,
		"source": h.Source, "destination": h.Destination, "finished_at": time.Now().UTC().Format(time.RFC3339),
	})
	for attempt := 0; attempt < 3; attempt++ {
		req, _ := http.NewRequest("POST", s.Opts.WebhookURL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "rsync-web")
		if s.Opts.WebhookSecret != "" {
			mac := hmac.New(sha256.New, []byte(s.Opts.WebhookSecret))
			mac.Write(body)
			req.Header.Set("X-Rsync-Web-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 300 {
				return
			}
			err = fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		slog.Warn("webhook failed", "job", id, "attempt", attempt+1, "err", err)
		time.Sleep(time.Duration(attempt+1) * 5 * time.Second)
	}
}

// sameOrigin rejects cross-site requests that change state. Browsers send Origin on POST/DELETE
// and on WebSocket handshakes; requiring JSON also forces a CORS preflight for cross-origin fetch.
// Requests without Origin (curl, scripts) are allowed: put authentication in front of rsync-web.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		write := r.Method != http.MethodGet && r.Method != http.MethodHead
		if (write || strings.HasPrefix(r.URL.Path, "/ws/")) && !sameOrigin(r) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && r.ContentLength != 0 && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
			return
		}
		next.ServeHTTP(w, r)
	})
}
