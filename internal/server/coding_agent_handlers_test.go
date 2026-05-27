package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rubellum/reader/internal/codingagent"
)

type codingAgentFakeRunner struct {
	args  []string
	stdin string
}

func (r *codingAgentFakeRunner) Run(ctx context.Context, args []string, stdin string) (codingagent.RunResult, error) {
	r.args = append([]string{}, args...)
	r.stdin = stdin
	return codingagent.RunResult{
		Stdout:      `{"session_id":"server-codex-session"}` + "\n",
		LastMessage: "server done",
		SessionID:   "server-codex-session",
		ExitCode:    0,
	}, nil
}

func TestCodingAgentRunAndSessions(t *testing.T) {
	read := t.TempDir()
	write := t.TempDir()
	if err := os.MkdirAll(filepath.Join(write, "writings"), 0o755); err != nil {
		t.Fatalf("mkdir writings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(write, "writings", "example.md"), []byte("# title\n\n@@fix\n@@ fix too\ntext"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	runner := &codingAgentFakeRunner{}
	srv := NewWithOptions(Options{
		ReadBasePath:      read,
		WriteBasePath:     write,
		CodingAgentRunner: runner,
	})

	body := strings.NewReader(`{"root":"write","path":"writings/example.md","instruction":"添削して","mode":"annotation-proofread"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/coding-agent/run", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var runResp codingagent.RunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &runResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if runResp.SessionID == "" || runResp.CodexSessionID != "server-codex-session" || runResp.Status != "completed" {
		t.Fatalf("unexpected response: %#v", runResp)
	}
	if !containsArgSequence(runner.args, []string{"exec", "--json", "-C", srv.writeRoots[0].ctx.basePath, "--sandbox", "workspace-write", "-"}) {
		t.Fatalf("unexpected args: %#v", runner.args)
	}
	if !strings.Contains(runner.stdin, "writings/example.md") || !strings.Contains(runner.stdin, "@@fix") || !strings.Contains(runner.stdin, "@@ fix too") {
		t.Fatalf("prompt missing target context: %s", runner.stdin)
	}
	if _, err := os.Stat(filepath.Join(read, codingagent.SessionsDirName, runResp.SessionID+".json")); err != nil {
		t.Fatalf("session not saved: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/coding-agent/sessions", nil)
	rec = httptest.NewRecorder()
	srv.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), runResp.SessionID) {
		t.Fatalf("session list missing id: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/coding-agent/sessions/"+runResp.SessionID, nil)
	rec = httptest.NewRecorder()
	srv.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session detail expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCodingAgentRunRejectsBadPath(t *testing.T) {
	root := t.TempDir()
	srv := NewWithOptions(Options{ReadBasePath: root, CodingAgentRunner: &codingAgentFakeRunner{}})

	req := httptest.NewRequest(http.MethodPost, "/api/coding-agent/run", strings.NewReader(`{"root":"","path":"../secret.md","mode":"annotation-proofread"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCodingAgentRunRejectsReadRoot(t *testing.T) {
	read := t.TempDir()
	if err := os.WriteFile(filepath.Join(read, "example.md"), []byte("text"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	srv := NewWithOptions(Options{
		ReadBasePath:      read,
		CodingAgentRunner: &codingAgentFakeRunner{},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/coding-agent/run", strings.NewReader(`{"root":"read","path":"example.md","mode":"annotation-proofread"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCodingAgentRunRejectsSymlinkOutsideRoot(t *testing.T) {
	read := t.TempDir()
	write := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(write, "linked.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	srv := NewWithOptions(Options{
		ReadBasePath:      read,
		WriteBasePath:     write,
		CodingAgentRunner: &codingAgentFakeRunner{},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/coding-agent/run", strings.NewReader(`{"root":"write","path":"linked.md","mode":"annotation-proofread"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func containsArgSequence(args, want []string) bool {
	for i := 0; i+len(want) <= len(args); i++ {
		ok := true
		for j := range want {
			if args[i+j] != want[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
