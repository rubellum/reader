package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newTestServer(basePath string, include, exclude []string, verbose bool) *Server {
	return NewWithOptions(Options{
		ReadBasePath: basePath,
		Include:      include,
		Exclude:      exclude,
		Verbose:      verbose,
	})
}

func newTestServerWithWrite(basePath, writeBasePath string, include, exclude []string, verbose bool) *Server {
	return NewWithOptions(Options{
		ReadBasePath:  basePath,
		WriteBasePath: writeBasePath,
		Include:       include,
		Exclude:       exclude,
		Verbose:       verbose,
	})
}

func TestValidateRelativePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "simple", path: "docs/readme.md", wantErr: false},
		{name: "dot path", path: "./docs/readme.md", wantErr: false},
		{name: "dot only", path: ".", wantErr: true},
		{name: "parent", path: "../secret.txt", wantErr: true},
		{name: "absolute", path: "/etc/passwd", wantErr: true},
		{name: "cleaned parent", path: "a/../../b", wantErr: true},
		{name: "cleaned ok", path: "a/../b/c.md", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRelativePath(tt.path)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestIsDiffTooLarge(t *testing.T) {
	if isDiffTooLarge([]byte("small"), []byte("data")) {
		t.Fatalf("expected small diff to be allowed")
	}

	large := make([]byte, 2*1024*1024)
	if !isDiffTooLarge(large, []byte("x")) {
		t.Fatalf("expected large diff to be rejected")
	}

	var lines []byte
	for i := 0; i < 20001; i++ {
		lines = append(lines, 'a', '\n')
	}
	if !isDiffTooLarge(lines, lines) {
		t.Fatalf("expected line-count limit to be rejected")
	}
}

func TestHandleTreeGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	runGit(t, repo, "checkout", "-b", "main")

	os.MkdirAll(filepath.Join(repo, "docs"), 0o755)
	os.WriteFile(filepath.Join(repo, "readme.md"), []byte("# hello"), 0o644)
	os.WriteFile(filepath.Join(repo, "docs", "guide.md"), []byte("# guide"), 0o644)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "init")

	srv := newTestServer(repo, []string{"*.md"}, nil, false)

	req := httptest.NewRequest(http.MethodGet, "/api/tree", nil)
	rec := httptest.NewRecorder()
	srv.echo.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse: %v", err)
	}

	// ルートがディレクトリであること
	if result["isDir"] != true {
		t.Fatalf("expected root to be dir")
	}

	// children が存在すること
	children, ok := result["children"].([]interface{})
	if !ok || len(children) == 0 {
		t.Fatalf("expected non-empty children")
	}
}

func TestHandleTreeWithWorktrees(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	featureDir := filepath.Join(base, "feature")

	os.MkdirAll(repo, 0o755)
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	runGit(t, repo, "checkout", "-b", "main")

	os.WriteFile(filepath.Join(repo, "shared.md"), []byte("# shared"), 0o644)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "init")

	runGit(t, repo, "worktree", "add", featureDir, "-b", "feature")
	os.WriteFile(filepath.Join(featureDir, "new.md"), []byte("# new"), 0o644)
	runGit(t, featureDir, "add", ".")
	runGit(t, featureDir, "commit", "-m", "add new")

	srv := newTestServer(repo, nil, nil, false)

	req := httptest.NewRequest(http.MethodGet, "/api/tree", nil)
	rec := httptest.NewRecorder()
	srv.echo.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse: %v", err)
	}

	children, ok := result["children"].([]interface{})
	if !ok {
		t.Fatalf("expected children array")
	}

	// shared.md と new.md の両方が存在すること（統合ツリー）
	foundShared, foundNew := false, false
	for _, child := range children {
		item := child.(map[string]interface{})
		name := item["name"].(string)
		if name == "shared.md" {
			foundShared = true
			// worktrees フィールドが存在すること
			wts, ok := item["worktrees"].([]interface{})
			if !ok || len(wts) == 0 {
				t.Fatalf("expected worktrees for shared.md")
			}
		}
		if name == "new.md" {
			foundNew = true
		}
	}

	if !foundShared {
		t.Fatalf("expected shared.md in unified tree")
	}
	if !foundNew {
		t.Fatalf("expected new.md in unified tree (from feature worktree)")
	}
}

func TestStartWithListener(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	runGit(t, repo, "checkout", "-b", "main")
	os.WriteFile(filepath.Join(repo, "readme.md"), []byte("# hello"), 0o644)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "init")

	srv := newTestServer(repo, nil, nil, false)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go func() {
		srv.StartWithListener(listener)
	}()
	defer srv.echo.Close()

	addr := listener.Addr().String()
	resp, err := http.Get(fmt.Sprintf("http://%s/", addr))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestStaticAssetsServed(t *testing.T) {
	// 静的アセット（CSS/JS/HTML）が embed 経由で正しく配信されることを保証する。
	// この経路が壊れると UI が完全に機能しなくなるためリリース品質の必須項目。
	repo := t.TempDir()
	srv := newTestServer(repo, nil, nil, false)
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	cases := []struct {
		name        string
		path        string
		mustContain string
	}{
		{name: "css", path: "/static/css/style.css", mustContain: ".sidebar"},
		{name: "js", path: "/static/js/app.js", mustContain: "loadTree"},
		{name: "index", path: "/", mustContain: "<title>"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tc.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tc.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200 for %s, got %d", tc.path, resp.StatusCode)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if !strings.Contains(string(body), tc.mustContain) {
				t.Fatalf("expected %s body to contain %q", tc.path, tc.mustContain)
			}
		})
	}
}

// runGit は handlers_test.go で定義済み
