package srv

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
