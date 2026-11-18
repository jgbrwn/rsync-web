package srv

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestNormalizeOptions(t *testing.T) {
	got := normalizeOptions([]string{"-v", "--exclude='/temp/'", "--timeout=185", "--exclude=\"*.part\"", "--timeout=185", " ", "'-P'"})
	want := []string{"-v", "--exclude=/temp/", "--timeout=185", "--exclude=*.part", "-P"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestGuard(t *testing.T) {
	s := &Server{}
	h := s.guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	cases := []struct {
		method, path, origin, ctype, site string
		body                              string
		want                              int
	}{
		{"POST", "/api/run", "https://rs.example", "application/json", "", "{}", 204},
		{"POST", "/api/run", "https://evil.example", "application/json", "", "{}", 403},
		{"POST", "/api/run", "", "text/plain", "", "{}", 415},             // form/no-cors POST
		{"POST", "/api/run", "", "application/json", "cross-site", "{}", 403},
		{"POST", "/api/cancel/1", "https://rs.example", "", "", "", 204}, // empty body
		{"GET", "/ws/job/1", "https://evil.example", "", "", "", 403},
		{"GET", "/api/history", "https://evil.example", "", "", "", 204}, // reads are left to CORS
		{"POST", "/api/run", "", "application/json", "", "{}", 204},      // curl / server-to-server
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "http://rs.example"+c.path, bytes.NewBufferString(c.body))
		r.Host = "rs.example"
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.ctype != "" {
			r.Header.Set("Content-Type", c.ctype)
		}
		if c.site != "" {
			r.Header.Set("Sec-Fetch-Site", c.site)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s %s origin=%q ctype=%q: got %d want %d", c.method, c.path, c.origin, c.ctype, w.Code, c.want)
		}
	}
}

func TestPushValidation(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "media", "Movie (2020)"), 0o755)
	s := &Server{WorkDir: dir, RsyncPath: "/bin/false", Opts: Options{PushRoot: "media", Presets: []Preset{{"Movies", "host:movies/"}}}}
	for _, c := range []struct {
		name, preset string
		want         int
	}{
		{"../etc", "Movies", 400}, {"a/b", "Movies", 400}, {"-e sh", "Movies", 400}, {"..", "Movies", 400},
		{"Missing", "Movies", 404}, {"Movie (2020)", "Nope", 400},
	} {
		b, _ := json.Marshal(map[string]string{"name": c.name, "preset": c.preset})
		w := httptest.NewRecorder()
		s.HandlePush(w, httptest.NewRequest("POST", "/api/push", bytes.NewReader(b)))
		if w.Code != c.want {
			t.Errorf("%q/%q: got %d want %d (%s)", c.name, c.preset, w.Code, c.want, w.Body.String())
		}
	}
}

func TestPushRemoveSource(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not installed")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "media", "Show S01", "Season 1"), 0o755)
	os.MkdirAll(filepath.Join(dir, "out"), 0o755)
	os.WriteFile(filepath.Join(dir, "media", "Show S01", "Season 1", "e1.mkv"), []byte("one"), 0o644)
	s, err := New(filepath.Join(dir, "db.sqlite"), dir, Options{PushRoot: "media", Presets: []Preset{{"TV", filepath.Join(dir, "out") + "/"}}})
	if err != nil {
		t.Fatal(err)
	}
	push := func(remove bool) {
		b, _ := json.Marshal(map[string]any{"name": "Show S01", "preset": "TV", "remove_source": remove})
		w := httptest.NewRecorder()
		s.HandlePush(w, httptest.NewRequest("POST", "/api/push", bytes.NewReader(b)))
		if w.Code != 200 {
			t.Fatalf("push: %d %s", w.Code, w.Body.String())
		}
	}
	src := filepath.Join(dir, "media", "Show S01", "Season 1", "e1.mkv")
	dst := filepath.Join(dir, "out", "Show S01", "Season 1", "e1.mkv")
	wait := func(cond func() bool) bool {
		for i := 0; i < 100; i++ {
			if cond() {
				return true
			}
			time.Sleep(50 * time.Millisecond)
		}
		return false
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	push(false)
	if !wait(func() bool { return exists(dst) }) || !exists(src) {
		t.Fatal("plain push must copy and keep the source")
	}
	os.Remove(dst)
	push(true)
	if !wait(func() bool { return exists(dst) && !exists(src) }) {
		t.Fatal("remove_source must copy, then remove the source file")
	}
	if !exists(filepath.Join(dir, "media", "Show S01", "Season 1")) {
		t.Fatal("folders stay")
	}
}
