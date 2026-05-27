package codingagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	args  []string
	stdin string
}

func (r *fakeRunner) Run(ctx context.Context, args []string, stdin string) (RunResult, error) {
	r.args = append([]string{}, args...)
	r.stdin = stdin
	return RunResult{
		Stdout:      `{"session_id":"codex-session-1"}` + "\n",
		LastMessage: "done",
		SessionID:   "codex-session-1",
		ExitCode:    0,
	}, nil
}

func TestBuildPromptForAnnotationProofread(t *testing.T) {
	prompt := BuildPrompt(RunRequest{
		Root:        "write",
		Path:        "writings/example.md",
		Instruction: "アノテーションに従って本文を添削してください",
		Mode:        ModeAnnotationProofread,
	}, "# Title\n\n@@ここを直す\n@@ ここも直す\n本文")

	for _, want := range []string{
		"Mode: annotation-proofread",
		"Target path: writings/example.md",
		"アノテーションに従って本文を添削してください",
		"@@ここを直す",
		"@@ ここも直す",
		"Edit the target file only when needed",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestRunSavesSessionAndUsesCodexExecArgs(t *testing.T) {
	root := t.TempDir()
	runner := &fakeRunner{}
	svc := NewService(root, runner)
	session, err := svc.Run(context.Background(), RunRequest{
		Root:        "write",
		Path:        "writings/example.md",
		Instruction: "添削して",
		Mode:        ModeAnnotationProofread,
	}, "本文")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !containsArgSequence(runner.args, []string{"exec", "--json", "-C", root, "--sandbox", "workspace-write", "-"}) {
		t.Fatalf("args = %#v", runner.args)
	}
	if session.CodexSessionID != "codex-session-1" || session.Mode != ModeAnnotationProofread || len(session.Turns) != 1 {
		t.Fatalf("session not populated: %#v", session)
	}
	if _, err := os.Stat(filepath.Join(root, SessionsDirName, session.ID+".json")); err != nil {
		t.Fatalf("session not saved: %v", err)
	}
}

func TestAppendOutputArgBeforePromptPlaceholder(t *testing.T) {
	got := appendOutputArg([]string{"exec", "--json", "-"}, "/tmp/out")
	want := []string{"exec", "--json", "-o", "/tmp/out", "-"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestRunRejectsUnsupportedMode(t *testing.T) {
	svc := NewService(t.TempDir(), &fakeRunner{})
	if _, err := svc.Run(context.Background(), RunRequest{Mode: "unknown"}, ""); err == nil {
		t.Fatalf("expected unsupported mode to fail")
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
