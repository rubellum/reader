package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsSamePath(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	sub := filepath.Join(root, "docs")
	other := filepath.Join(base, "repo2")

	if err := ensureDir(root); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := ensureDir(sub); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := ensureDir(other); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}

	if !isSamePath(root, root) {
		t.Fatalf("expected same path to be true")
	}
	if !isSamePath(root, sub) {
		t.Fatalf("expected sub path to be treated as current")
	}
	if isSamePath(root, other) {
		t.Fatalf("expected different path to be false")
	}
}

func TestParseWorktreeOutput(t *testing.T) {
	base := t.TempDir()
	wt1 := filepath.Join(base, "repo-main")
	wt2 := filepath.Join(base, "repo-feature")

	output := strings.Join([]string{
		"worktree " + wt1,
		"HEAD 0000000000000000000000000000000000000000",
		"branch refs/heads/main",
		"",
		"worktree " + wt2,
		"HEAD 1111111111111111111111111111111111111111",
		"branch refs/heads/feature",
		"",
	}, "\n")

	worktrees := parseWorktreeOutput(output, filepath.Join(wt1, "docs"))
	if len(worktrees) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(worktrees))
	}
	if worktrees[0].Name != "main" {
		t.Fatalf("expected main branch name")
	}
	if !worktrees[0].Current {
		t.Fatalf("expected main worktree to be current")
	}
	if worktrees[1].Name != "feature" {
		t.Fatalf("expected feature branch name")
	}
	if worktrees[1].Current {
		t.Fatalf("expected feature worktree to be non-current")
	}
}

func TestGitRootFromRepoDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	base := t.TempDir()
	repoDir := filepath.Join(base, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runGit(t, repoDir, "init")

	got, err := GitRoot(repoDir)
	if err != nil {
		t.Fatalf("GitRoot: %v", err)
	}

	// シンボリックリンク解決して比較
	wantResolved, _ := filepath.EvalSymlinks(repoDir)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Fatalf("expected %s, got %s", wantResolved, gotResolved)
	}
}

func TestGitRootFromSubdir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	base := t.TempDir()
	repoDir := filepath.Join(base, "repo")
	subDir := filepath.Join(repoDir, "a", "b")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runGit(t, repoDir, "init")

	got, err := GitRoot(subDir)
	if err != nil {
		t.Fatalf("GitRoot: %v", err)
	}

	wantResolved, _ := filepath.EvalSymlinks(repoDir)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Fatalf("expected %s, got %s", wantResolved, gotResolved)
	}
}

func TestGitRootNonGitDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	_, err := GitRoot(dir)
	if err == nil {
		t.Fatalf("expected error for non-git directory")
	}
}

func TestListWithHashesGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	base := t.TempDir()
	repoDir := filepath.Join(base, "repo")
	featureDir := filepath.Join(base, "feature")

	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "Test User")
	runGit(t, repoDir, "checkout", "-b", "main")

	if err := os.WriteFile(filepath.Join(repoDir, "doc.md"), []byte("# main"), 0o644); err != nil {
		t.Fatalf("write main doc: %v", err)
	}
	runGit(t, repoDir, "add", "doc.md")
	runGit(t, repoDir, "commit", "-m", "init")

	runGit(t, repoDir, "worktree", "add", featureDir, "-b", "feature")
	if err := os.WriteFile(filepath.Join(featureDir, "doc.md"), []byte("# feature"), 0o644); err != nil {
		t.Fatalf("write feature doc: %v", err)
	}

	// relSubdir = "." → basePath が git root と一致するケース
	worktrees, err := ListWithHashes(featureDir, ".", "doc.md")
	if err != nil {
		t.Fatalf("list with hashes: %v", err)
	}
	if len(worktrees) < 2 {
		t.Fatalf("expected at least 2 worktrees")
	}
	for _, wt := range worktrees {
		if wt.FileHash == nil {
			t.Fatalf("expected file hash for worktree %s", wt.Name)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}
