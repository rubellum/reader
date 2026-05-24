package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rubellum/reader/internal/document"
	"github.com/rubellum/reader/internal/tree"
)

func TestHandlersBasic(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(root, "doc.md"), []byte("# Title"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")

	srv := newTestServer(root, nil, nil, false)
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	t.Run("tree", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/tree")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected status: %d", resp.StatusCode)
		}
		var data tree.TreeItem
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !hasTreePath(&data, "doc.md") {
			t.Fatalf("expected doc.md to be listed")
		}
	})

	t.Run("file", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/file?path=doc.md")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected status: %d", resp.StatusCode)
		}
		var doc document.Document
		if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if doc.Path != "doc.md" {
			t.Fatalf("expected path to be doc.md")
		}
		if !strings.Contains(doc.HTML, "<h1") {
			t.Fatalf("expected HTML to contain h1")
		}
	})

	t.Run("worktrees", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/worktrees")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected status: %d", resp.StatusCode)
		}
		var payload struct {
			Worktrees []interface{} `json:"worktrees"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		// Git リポジトリなので少なくとも1つの worktree がある
		if len(payload.Worktrees) == 0 {
			t.Fatalf("expected at least one worktree in git repo")
		}
	})
}

func TestHandlersWithGitDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	mainDir, featureDir := setupGitRepo(t)

	// start server in feature worktree to get diff vs main
	srv := newTestServer(featureDir, nil, nil, false)
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	t.Run("worktrees-with-hash", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/worktrees?path=doc.md")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected status: %d", resp.StatusCode)
		}
		var payload struct {
			Worktrees []struct {
				Name     string  `json:"name"`
				FileHash *string `json:"fileHash"`
			} `json:"worktrees"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(payload.Worktrees) < 2 {
			t.Fatalf("expected at least 2 worktrees")
		}
		for _, wt := range payload.Worktrees {
			if wt.FileHash == nil {
				t.Fatalf("expected file hash for worktree %s", wt.Name)
			}
		}
	})

	t.Run("diff", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/diff?path=doc.md")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status: %d body=%s", resp.StatusCode, string(body))
		}
		var payload struct {
			HasDiff bool `json:"hasDiff"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !payload.HasDiff {
			t.Fatalf("expected diff to be detected")
		}
	})

	t.Run("diff-size-guard", func(t *testing.T) {
		large := strings.Repeat("x\n", 50000)
		if err := os.WriteFile(filepath.Join(featureDir, "big.md"), []byte(large), 0o644); err != nil {
			t.Fatalf("write big: %v", err)
		}
		if err := os.WriteFile(filepath.Join(mainDir, "big.md"), []byte("small"), 0o644); err != nil {
			t.Fatalf("write big main: %v", err)
		}

		resp := mustGet(t, ts.URL+"/api/diff?path=big.md")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413, got %d", resp.StatusCode)
		}
	})
}

// TestSubdirectoryAsRoot は、git root のサブディレクトリを root として開いた場合に、
// そのサブディレクトリ配下のみがツリー・API で見え、ファイルパスもサブディレクトリ相対に
// 正規化されることを確認する。
func TestSubdirectoryAsRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "checkout", "-b", "main")

	os.MkdirAll(filepath.Join(root, "docs"), 0o755)
	os.WriteFile(filepath.Join(root, "outside.md"), []byte("# outside"), 0o644)
	os.WriteFile(filepath.Join(root, "docs", "intro.md"), []byte("# intro"), 0o644)
	os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("# [リンク](intro.md)"), 0o644)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")

	// docs サブディレクトリをルートとしてサーバを起動
	srv := newTestServer(filepath.Join(root, "docs"), nil, nil, false)
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	t.Run("tree shows only subdirectory contents", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/tree")
		defer resp.Body.Close()
		var data tree.TreeItem
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if hasTreePath(&data, "outside.md") {
			t.Fatalf("outside.md (git root直下) はサブディレクトリ root では見えてはいけない")
		}
		if hasTreePath(&data, "docs/intro.md") {
			t.Fatalf("パスは subdirectory 相対であるべき: docs/intro.md ではなく intro.md であるべき")
		}
		if !hasTreePath(&data, "intro.md") {
			t.Fatalf("intro.md がツリーに無い")
		}
		if !hasTreePath(&data, "guide.md") {
			t.Fatalf("guide.md がツリーに無い")
		}
	})

	t.Run("file API uses subdirectory-relative path", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/file?path=intro.md")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for intro.md, got %d", resp.StatusCode)
		}
	})

	t.Run("file API rejects path that escapes subdirectory", func(t *testing.T) {
		// validateRelativePath が ".." を弾くので 400
		resp := mustGet(t, ts.URL+"/api/file?path=../outside.md")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("rendered link is subdirectory-relative", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/file?path=guide.md")
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		// 相対リンク `intro.md` は subdirectory root 配下の `?file=intro.md` にリライトされる
		if !strings.Contains(string(body), `?file=intro.md`) {
			t.Fatalf("expected rewritten link to use subdir-relative path, got %s", string(body))
		}
	})
}

// TestWriteRoot は -write で指定された別ディレクトリへの読み書き API を検証する。
func TestWriteRoot(t *testing.T) {
	read := t.TempDir()
	write := t.TempDir()
	if err := os.WriteFile(filepath.Join(read, "view.md"), []byte("# read only"), 0o644); err != nil {
		t.Fatalf("write read: %v", err)
	}
	if err := os.WriteFile(filepath.Join(write, "draft.md"), []byte("# draft"), 0o644); err != nil {
		t.Fatalf("write write: %v", err)
	}
	os.MkdirAll(filepath.Join(write, "notes"), 0o755)
	os.WriteFile(filepath.Join(write, "notes", "a.md"), []byte("# a"), 0o644)

	srv := newTestServerWithWrite(read, write, nil, nil, false)
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	t.Run("config exposes writeEnabled", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/config")
		defer resp.Body.Close()
		var cfg map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if cfg["writeEnabled"] != true {
			t.Fatalf("expected writeEnabled=true, got %v", cfg["writeEnabled"])
		}
	})

	t.Run("read tree shows only read files", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/tree")
		defer resp.Body.Close()
		var data tree.TreeItem
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !hasTreePath(&data, "view.md") {
			t.Fatalf("expected view.md in read tree")
		}
		if hasTreePath(&data, "draft.md") {
			t.Fatalf("draft.md must not appear in read tree")
		}
	})

	t.Run("write tree shows only write files", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/tree?root=write")
		defer resp.Body.Close()
		var data tree.TreeItem
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if hasTreePath(&data, "view.md") {
			t.Fatalf("view.md must not appear in write tree")
		}
		if !hasTreePath(&data, "draft.md") {
			t.Fatalf("expected draft.md in write tree")
		}
		if !hasTreePath(&data, "notes/a.md") {
			t.Fatalf("expected nested notes/a.md in write tree")
		}
	})

	t.Run("raw fetch from write root", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/raw?root=write&path=draft.md")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "# draft" {
			t.Fatalf("expected '# draft', got %q", string(body))
		}
	})

	t.Run("PUT to write root creates new file", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/file?root=write&path=newdir/created.md", strings.NewReader("# new"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		got, _ := os.ReadFile(filepath.Join(write, "newdir", "created.md"))
		if string(got) != "# new" {
			t.Fatalf("expected '# new', got %q", string(got))
		}
	})

	t.Run("PUT to read root rejects nonexistent", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/file?path=missing.md", strings.NewReader("x"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("PUT to write root overwrites", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/file?root=write&path=draft.md", strings.NewReader("changed"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		got, _ := os.ReadFile(filepath.Join(write, "draft.md"))
		if string(got) != "changed" {
			t.Fatalf("expected 'changed', got %q", string(got))
		}
	})

	t.Run("write root traversal rejected", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/file?root=write&path=../escape.md", strings.NewReader("x"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})
}

func TestSidebarRootsAndDescendingOrderE2E(t *testing.T) {
	read := t.TempDir()
	firstRead := t.TempDir()
	secondRead := t.TempDir()
	firstWrite := t.TempDir()
	secondWrite := t.TempDir()

	for _, dir := range []string{read, firstRead, secondRead, firstWrite, secondWrite} {
		if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	os.WriteFile(filepath.Join(read, "base.md"), []byte("# base"), 0o644)
	os.WriteFile(filepath.Join(firstRead, "alpha.md"), []byte("# alpha"), 0o644)
	os.WriteFile(filepath.Join(firstRead, "zeta.md"), []byte("# zeta"), 0o644)
	os.WriteFile(filepath.Join(firstRead, "docs", "alpha.md"), []byte("# docs alpha"), 0o644)
	os.WriteFile(filepath.Join(firstRead, "docs", "zeta.md"), []byte("# docs zeta"), 0o644)
	os.WriteFile(filepath.Join(secondRead, "second.md"), []byte("# second"), 0o644)
	os.WriteFile(filepath.Join(firstWrite, "alpha.md"), []byte("# alpha write"), 0o644)
	os.WriteFile(filepath.Join(firstWrite, "zeta.md"), []byte("# zeta write"), 0o644)
	os.WriteFile(filepath.Join(secondWrite, "second-write.md"), []byte("# second write"), 0o644)

	srv := NewWithOptions(Options{
		ReadRoots: []RootOption{
			{BasePath: firstRead, SortDesc: true},
			{BasePath: secondRead},
		},
		WriteRoots: []RootOption{
			{BasePath: firstWrite, SortDesc: true},
			{BasePath: secondWrite},
		},
	})
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	t.Run("config exposes additional sidebar roots", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/config")
		defer resp.Body.Close()
		var cfg map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if cfg["writeEnabled"] != true {
			t.Fatalf("expected writeEnabled=true, got %v", cfg["writeEnabled"])
		}
		readRoots, ok := cfg["readRoots"].([]interface{})
		if !ok || len(readRoots) != 2 {
			t.Fatalf("expected 2 read roots, got %#v", cfg["readRoots"])
		}
		if readRoots[0].(map[string]interface{})["id"] != "read" || readRoots[1].(map[string]interface{})["id"] != "read-2" {
			t.Fatalf("read roots not in command order: %#v", readRoots)
		}
		if readRoots[0].(map[string]interface{})["sortDesc"] != true || cfg["writeRootSortDesc"] != true {
			t.Fatalf("expected descending flags in config, got %#v", cfg)
		}
		writeRoots, ok := cfg["writeRoots"].([]interface{})
		if !ok || len(writeRoots) != 2 {
			t.Fatalf("expected 2 write roots, got %#v", cfg["writeRoots"])
		}
		if writeRoots[0].(map[string]interface{})["id"] != "write" || writeRoots[1].(map[string]interface{})["id"] != "write-2" {
			t.Fatalf("write roots not in command order: %#v", writeRoots)
		}
	})

	t.Run("read tree is isolated and descending", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/tree")
		defer resp.Body.Close()
		var data tree.TreeItem
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if hasTreePath(&data, "base.md") {
			t.Fatalf("base.md must not appear when -read replaces current directory")
		}
		if got, want := serverChildNames(&data), []string{"docs", "zeta.md", "alpha.md"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("read root order = %v, want %v", got, want)
		}
		docs := findTreeItem(&data, "docs")
		if got, want := serverChildNames(docs), []string{"zeta.md", "alpha.md"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("read docs order = %v, want %v", got, want)
		}
	})

	t.Run("second read root follows command order", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/tree?root=read-2")
		defer resp.Body.Close()
		var data tree.TreeItem
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !hasTreePath(&data, "second.md") {
			t.Fatalf("expected second.md in second read root")
		}
		if hasTreePath(&data, "base.md") || hasTreePath(&data, "zeta.md") {
			t.Fatalf("second read root must be isolated: %+v", data.Children)
		}
	})

	t.Run("read file API renders selected root", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/file?path=zeta.md")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "# zeta") && !strings.Contains(string(body), "zeta") {
			t.Fatalf("expected zeta content, got %s", string(body))
		}
	})

	t.Run("write tree is isolated and descending", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/tree?root=write")
		defer resp.Body.Close()
		var data tree.TreeItem
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if hasTreePath(&data, "base.md") {
			t.Fatalf("base.md must not appear in write tree")
		}
		if got, want := serverChildNames(&data), []string{"zeta.md", "alpha.md"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("write root order = %v, want %v", got, want)
		}
	})

	t.Run("second write root can create files independently", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/file?root=write-2&path=new.md", strings.NewReader("# new"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if _, err := os.Stat(filepath.Join(secondWrite, "new.md")); err != nil {
			t.Fatalf("expected new.md in second write root: %v", err)
		}
		if _, err := os.Stat(filepath.Join(firstWrite, "new.md")); !os.IsNotExist(err) {
			t.Fatalf("new.md must not be created in first write root")
		}
	})
}

// TestWriteRootDisabled は -write 未指定時の挙動を確認する。
func TestWriteRootDisabled(t *testing.T) {
	srv := newTestServer(t.TempDir(), nil, nil, false)
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	resp := mustGet(t, ts.URL+"/api/config")
	defer resp.Body.Close()
	var cfg map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&cfg)
	if cfg["writeEnabled"] != false {
		t.Fatalf("expected writeEnabled=false, got %v", cfg["writeEnabled"])
	}

	resp2 := mustGet(t, ts.URL+"/api/tree?root=write")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for tree?root=write when disabled, got %d", resp2.StatusCode)
	}
}

func TestHandleArchive(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "path", "to"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "path", "to", "file.md"), []byte("# file"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	srv := NewWithOptions(Options{ReadBasePath: root})
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/archive?path=path/to/file.md", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, string(body))
	}
	if _, err := os.Stat(filepath.Join(root, "path", "to", "file.md")); !os.IsNotExist(err) {
		t.Fatalf("source file should be moved, stat err=%v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "archive", "path", "to", "file.md"))
	if err != nil {
		t.Fatalf("read archived file: %v", err)
	}
	if string(got) != "# file" {
		t.Fatalf("archived content = %q", string(got))
	}
}

func TestHandleArchiveCustomDirAndConflict(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "doc.md"), []byte("# doc"), 0o644)
	os.MkdirAll(filepath.Join(root, "done"), 0o755)
	os.WriteFile(filepath.Join(root, "done", "doc.md"), []byte("# existing"), 0o644)

	srv := NewWithOptions(Options{ReadBasePath: root, ArchiveDir: "done"})
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/archive?path=doc.md", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	got, err := os.ReadFile(filepath.Join(root, "doc.md"))
	if err != nil {
		t.Fatalf("source should remain: %v", err)
	}
	if string(got) != "# doc" {
		t.Fatalf("source content = %q", string(got))
	}
}

func TestHandleArchiveWriteRoot(t *testing.T) {
	read := t.TempDir()
	write := t.TempDir()
	os.WriteFile(filepath.Join(write, "draft.md"), []byte("# draft"), 0o644)

	srv := NewWithOptions(Options{ReadBasePath: read, WriteBasePath: write, ArchiveDir: "arch"})
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/archive?root=write&path=draft.md", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(write, "draft.md")); !os.IsNotExist(err) {
		t.Fatalf("source file should be moved, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(write, "arch", "draft.md")); err != nil {
		t.Fatalf("expected archived write file: %v", err)
	}
}

func TestHandleFileWrite(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "checkout", "-b", "main")

	if err := os.WriteFile(filepath.Join(root, "doc.md"), []byte("# initial"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")

	srv := newTestServer(root, nil, nil, false)
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	put := func(path, body string) *http.Response {
		req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/file?path="+path, strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("put: %v", err)
		}
		return resp
	}

	t.Run("overwrites existing file", func(t *testing.T) {
		newBody := "# updated\n\nbody"
		resp := put("doc.md", newBody)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var payload struct {
			Path       string `json:"path"`
			ModifiedAt int64  `json:"modifiedAt"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if payload.Path != "doc.md" || payload.ModifiedAt == 0 {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		written, _ := os.ReadFile(filepath.Join(root, "doc.md"))
		if string(written) != newBody {
			t.Fatalf("expected %q, got %q", newBody, string(written))
		}
	})

	t.Run("missing path returns 400", func(t *testing.T) {
		resp := put("", "x")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("traversal returns 400", func(t *testing.T) {
		resp := put("../escape.md", "x")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("nonexistent file returns 404", func(t *testing.T) {
		resp := put("missing.md", "x")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("directory returns 400", func(t *testing.T) {
		os.MkdirAll(filepath.Join(root, "sub"), 0o755)
		resp := put("sub", "x")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})
}

func TestHandleRaw(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "checkout", "-b", "main")

	imgBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A} // PNG マジックバイト
	if err := os.WriteFile(filepath.Join(root, "pic.png"), imgBytes, 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "doc.md"), []byte("# x"), 0o644); err != nil {
		t.Fatalf("write md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "page.html"), []byte("<!doctype html><title>x</title>"), 0o644); err != nil {
		t.Fatalf("write html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "style.css"), []byte("body{color:red}"), 0o644); err != nil {
		t.Fatalf("write css: %v", err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")

	srv := newTestServer(root, nil, nil, false)
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	t.Run("serves binary asset", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/raw?path=pic.png")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if len(body) != len(imgBytes) || body[0] != 0x89 {
			t.Fatalf("expected PNG bytes, got %d bytes (first=%x)", len(body), body[0])
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "image/png") {
			t.Fatalf("expected image/png Content-Type, got %s", ct)
		}
	})

	t.Run("serves html preview with sandbox csp", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/html/read/page.html")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Fatalf("expected text/html Content-Type, got %s", ct)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("expected nosniff header")
		}
		csp := resp.Header.Get("Content-Security-Policy")
		if !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "script-src 'none'") {
			t.Fatalf("expected sandbox CSP without scripts, got %q", csp)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "<title>x</title>") {
			t.Fatalf("expected html body, got %q", string(body))
		}
	})

	t.Run("serves html preview relative css", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/html/read/style.css")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/css") {
			t.Fatalf("expected text/css Content-Type, got %s", ct)
		}
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "body{color:red}" {
			t.Fatalf("expected css body, got %q", string(body))
		}
	})

	t.Run("html preview rejects symlink outside root", func(t *testing.T) {
		outsideDir := t.TempDir()
		outsideFile := filepath.Join(outsideDir, "outside.html")
		if err := os.WriteFile(outsideFile, []byte("<!doctype html><title>outside</title>"), 0o644); err != nil {
			t.Fatalf("write outside html: %v", err)
		}
		linkPath := filepath.Join(root, "outside.html")
		if err := os.Symlink(outsideFile, linkPath); err != nil {
			t.Skipf("symlink not available: %v", err)
		}

		resp := mustGet(t, ts.URL+"/html/read/outside.html")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("missing path returns 400", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/raw")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("traversal returns 400", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/raw?path=../etc/passwd")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("nonexistent returns 404", func(t *testing.T) {
		resp := mustGet(t, ts.URL+"/api/raw?path=missing.png")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("directory returns 400", func(t *testing.T) {
		os.MkdirAll(filepath.Join(root, "sub"), 0o755)
		resp := mustGet(t, ts.URL+"/api/raw?path=sub")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})
}

func TestHandleTreePatternFiltering(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "checkout", "-b", "main")

	// .md と .go の両方を作成
	os.WriteFile(filepath.Join(root, "readme.md"), []byte("# readme"), 0o644)
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("notes"), 0o644)
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644)
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)
	os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("# guide"), 0o644)
	os.WriteFile(filepath.Join(root, "docs", "util.go"), []byte("package docs"), 0o644)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")

	t.Run("include_md_only", func(t *testing.T) {
		srv := newTestServer(root, []string{"*.md"}, nil, false)
		ts := httptest.NewServer(srv.echo)
		defer ts.Close()

		resp := mustGet(t, ts.URL+"/api/tree")
		defer resp.Body.Close()

		var data tree.TreeItem
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if !hasTreePath(&data, "readme.md") {
			t.Fatalf("expected readme.md in tree")
		}
		if !hasTreePath(&data, "docs/guide.md") {
			t.Fatalf("expected docs/guide.md in tree")
		}
		if hasTreePath(&data, "main.go") {
			t.Fatalf("expected main.go to be excluded by *.md pattern")
		}
		if hasTreePath(&data, "notes.txt") {
			t.Fatalf("expected notes.txt to be excluded by *.md pattern")
		}
		if hasTreePath(&data, "docs/util.go") {
			t.Fatalf("expected docs/util.go to be excluded by *.md pattern")
		}
	})

	t.Run("include_md_and_txt", func(t *testing.T) {
		srv := newTestServer(root, []string{"*.md", "*.txt"}, nil, false)
		ts := httptest.NewServer(srv.echo)
		defer ts.Close()

		resp := mustGet(t, ts.URL+"/api/tree")
		defer resp.Body.Close()

		var data tree.TreeItem
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if !hasTreePath(&data, "readme.md") {
			t.Fatalf("expected readme.md in tree")
		}
		if !hasTreePath(&data, "notes.txt") {
			t.Fatalf("expected notes.txt in tree")
		}
		if hasTreePath(&data, "main.go") {
			t.Fatalf("expected main.go to be excluded")
		}
		if hasTreePath(&data, "docs/util.go") {
			t.Fatalf("expected docs/util.go to be excluded")
		}
	})

	t.Run("exclude_pattern", func(t *testing.T) {
		srv := newTestServer(root, nil, []string{"docs/*"}, false)
		ts := httptest.NewServer(srv.echo)
		defer ts.Close()

		resp := mustGet(t, ts.URL+"/api/tree")
		defer resp.Body.Close()

		var data tree.TreeItem
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if !hasTreePath(&data, "readme.md") {
			t.Fatalf("expected readme.md in tree")
		}
		if !hasTreePath(&data, "main.go") {
			t.Fatalf("expected main.go in tree (no include filter)")
		}
		if hasTreePath(&data, "docs/guide.md") {
			t.Fatalf("expected docs/guide.md to be excluded by !docs/*")
		}
		if hasTreePath(&data, "docs/util.go") {
			t.Fatalf("expected docs/util.go to be excluded by !docs/*")
		}
	})

	t.Run("path_include_pattern", func(t *testing.T) {
		srv := newTestServer(root, []string{"docs/*.md"}, nil, false)
		ts := httptest.NewServer(srv.echo)
		defer ts.Close()

		resp := mustGet(t, ts.URL+"/api/tree")
		defer resp.Body.Close()

		var data tree.TreeItem
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if !hasTreePath(&data, "docs/guide.md") {
			t.Fatalf("expected docs/guide.md in tree")
		}
		if hasTreePath(&data, "readme.md") {
			t.Fatalf("expected readme.md to be excluded by docs/*.md pattern")
		}
		if hasTreePath(&data, "main.go") {
			t.Fatalf("expected main.go to be excluded by docs/*.md pattern")
		}
	})
}

func TestHandleTreePatternFilteringWithWorktrees(t *testing.T) {
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

	os.WriteFile(filepath.Join(repo, "readme.md"), []byte("# readme"), 0o644)
	os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main"), 0o644)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "init")

	runGit(t, repo, "worktree", "add", featureDir, "-b", "feature")
	os.WriteFile(filepath.Join(featureDir, "new.md"), []byte("# new"), 0o644)
	os.WriteFile(filepath.Join(featureDir, "new.go"), []byte("package new"), 0o644)
	runGit(t, featureDir, "add", ".")
	runGit(t, featureDir, "commit", "-m", "add new files")

	srv := newTestServer(repo, []string{"*.md"}, nil, false)
	ts := httptest.NewServer(srv.echo)
	defer ts.Close()

	resp := mustGet(t, ts.URL+"/api/tree")
	defer resp.Body.Close()

	var data tree.TreeItem
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// .md ファイルは統合ツリーに含まれる
	if !hasTreePath(&data, "readme.md") {
		t.Fatalf("expected readme.md in tree")
	}
	if !hasTreePath(&data, "new.md") {
		t.Fatalf("expected new.md from feature worktree in tree")
	}

	// .go ファイルはフィルタで除外される
	if hasTreePath(&data, "main.go") {
		t.Fatalf("expected main.go to be excluded by *.md pattern")
	}
	if hasTreePath(&data, "new.go") {
		t.Fatalf("expected new.go to be excluded by *.md pattern")
	}
}

func TestHandleTreeFilteringWithUntrackedFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "checkout", "-b", "main")

	// 追跡済みファイル
	os.WriteFile(filepath.Join(root, "readme.md"), []byte("# readme"), 0o644)
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")

	// 未追跡ファイル（git add しない）
	os.WriteFile(filepath.Join(root, "draft.md"), []byte("# draft"), 0o644)
	os.WriteFile(filepath.Join(root, "scratch.go"), []byte("package scratch"), 0o644)
	os.MkdirAll(filepath.Join(root, "tmp"), 0o755)
	os.WriteFile(filepath.Join(root, "tmp", "note.md"), []byte("# note"), 0o644)
	os.WriteFile(filepath.Join(root, "tmp", "debug.go"), []byte("package tmp"), 0o644)

	t.Run("include_md_filters_untracked", func(t *testing.T) {
		srv := newTestServer(root, []string{"*.md"}, nil, false)
		ts := httptest.NewServer(srv.echo)
		defer ts.Close()

		resp := mustGet(t, ts.URL+"/api/tree")
		defer resp.Body.Close()

		var data tree.TreeItem
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("decode: %v", err)
		}

		// 追跡・未追跡問わず .md は表示
		if !hasTreePath(&data, "readme.md") {
			t.Fatalf("expected tracked readme.md in tree")
		}
		if !hasTreePath(&data, "draft.md") {
			t.Fatalf("expected untracked draft.md in tree")
		}
		if !hasTreePath(&data, "tmp/note.md") {
			t.Fatalf("expected untracked tmp/note.md in tree")
		}

		// .go はフィルタで除外
		if hasTreePath(&data, "main.go") {
			t.Fatalf("expected tracked main.go to be excluded by *.md pattern")
		}
		if hasTreePath(&data, "scratch.go") {
			t.Fatalf("expected untracked scratch.go to be excluded by *.md pattern")
		}
		if hasTreePath(&data, "tmp/debug.go") {
			t.Fatalf("expected untracked tmp/debug.go to be excluded by *.md pattern")
		}
	})

	t.Run("no_pattern_shows_all", func(t *testing.T) {
		srv := newTestServer(root, nil, nil, false)
		ts := httptest.NewServer(srv.echo)
		defer ts.Close()

		resp := mustGet(t, ts.URL+"/api/tree")
		defer resp.Body.Close()

		var data tree.TreeItem
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("decode: %v", err)
		}

		// パターンなし: 全ファイル表示
		if !hasTreePath(&data, "readme.md") {
			t.Fatalf("expected readme.md")
		}
		if !hasTreePath(&data, "main.go") {
			t.Fatalf("expected main.go")
		}
		if !hasTreePath(&data, "draft.md") {
			t.Fatalf("expected draft.md")
		}
		if !hasTreePath(&data, "scratch.go") {
			t.Fatalf("expected scratch.go")
		}
	})

	t.Run("exclude_pattern_with_untracked", func(t *testing.T) {
		srv := newTestServer(root, nil, []string{"tmp/*"}, false)
		ts := httptest.NewServer(srv.echo)
		defer ts.Close()

		resp := mustGet(t, ts.URL+"/api/tree")
		defer resp.Body.Close()

		var data tree.TreeItem
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			t.Fatalf("decode: %v", err)
		}

		// tmp/ 以下は除外
		if hasTreePath(&data, "tmp/note.md") {
			t.Fatalf("expected tmp/note.md to be excluded")
		}
		if hasTreePath(&data, "tmp/debug.go") {
			t.Fatalf("expected tmp/debug.go to be excluded")
		}

		// ルートのファイルは表示
		if !hasTreePath(&data, "readme.md") {
			t.Fatalf("expected readme.md")
		}
		if !hasTreePath(&data, "draft.md") {
			t.Fatalf("expected draft.md")
		}
	})
}

func TestHandleTreeExcludeNestedUntracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "checkout", "-b", "main")

	// 追跡済みファイル
	os.WriteFile(filepath.Join(root, "readme.md"), []byte("# readme"), 0o644)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")

	// 未追跡のネストされたディレクトリ（git ls-files --others で深いパスが返る）
	os.MkdirAll(filepath.Join(root, "node_modules", "pkg", "lib"), 0o755)
	os.WriteFile(filepath.Join(root, "node_modules", "pkg", "index.js"), []byte("module"), 0o644)
	os.WriteFile(filepath.Join(root, "node_modules", "pkg", "lib", "util.js"), []byte("util"), 0o644)
	os.MkdirAll(filepath.Join(root, "vendor", "github.com", "lib"), 0o755)
	os.WriteFile(filepath.Join(root, "vendor", "github.com", "lib", "foo.go"), []byte("package lib"), 0o644)

	// デフォルトパターン相当: *.md + !node_modules/* + !vendor/*
	srv := newTestServer(root, []string{"*.md"}, []string{"node_modules/*", "vendor/*"}, false)
	ts := httptest.NewServer(srv.echo)
	defer ts.Close()

	resp := mustGet(t, ts.URL+"/api/tree")
	defer resp.Body.Close()

	var data tree.TreeItem
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !hasTreePath(&data, "readme.md") {
		t.Fatalf("expected readme.md in tree")
	}
	// ネストされたnode_modules配下は除外されること
	if hasTreePath(&data, "node_modules/pkg/index.js") {
		t.Fatalf("expected node_modules/pkg/index.js excluded")
	}
	if hasTreePath(&data, "node_modules/pkg/lib/util.js") {
		t.Fatalf("expected node_modules/pkg/lib/util.js excluded")
	}
	// ネストされたvendor配下も除外されること
	if hasTreePath(&data, "vendor/github.com/lib/foo.go") {
		t.Fatalf("expected vendor/github.com/lib/foo.go excluded")
	}
}

func mustGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return resp
}

func hasTreePath(root *tree.TreeItem, rel string) bool {
	if root == nil {
		return false
	}
	if root.Path == rel {
		return true
	}
	for _, child := range root.Children {
		if hasTreePath(child, rel) {
			return true
		}
	}
	return false
}

func serverChildNames(item *tree.TreeItem) []string {
	if item == nil {
		return nil
	}
	names := make([]string, 0, len(item.Children))
	for _, child := range item.Children {
		names = append(names, child.Name)
	}
	return names
}

func setupGitRepo(t *testing.T) (mainDir, featureDir string) {
	base := t.TempDir()
	repoDir := filepath.Join(base, "repo")
	featureDir = filepath.Join(base, "feature")

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

	return repoDir, featureDir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func findTreeItem(root *tree.TreeItem, name string) *tree.TreeItem {
	if root == nil {
		return nil
	}
	if root.Name == name {
		return root
	}
	for _, child := range root.Children {
		if found := findTreeItem(child, name); found != nil {
			return found
		}
	}
	return nil
}

func TestHandleTreeWorktreeMetadata(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	featureDir := filepath.Join(base, "feature")

	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", repo, err)
	}
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	runGit(t, repo, "checkout", "-b", "main")

	if err := os.WriteFile(filepath.Join(repo, "shared.md"), []byte("# shared"), 0o644); err != nil {
		t.Fatalf("WriteFile(shared.md): %v", err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "init")

	runGit(t, repo, "worktree", "add", featureDir, "-b", "feature")
	if err := os.WriteFile(filepath.Join(featureDir, "feature-only.md"), []byte("# feature only"), 0o644); err != nil {
		t.Fatalf("WriteFile(feature-only.md): %v", err)
	}
	runGit(t, featureDir, "add", ".")
	runGit(t, featureDir, "commit", "-m", "add feature file")

	srv := newTestServer(repo, nil, nil, false)
	ts := httptest.NewServer(srv.echo)
	defer ts.Close()

	resp := mustGet(t, ts.URL+"/api/tree")
	defer resp.Body.Close()

	var data tree.TreeItem
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode: %v", err)
	}

	shared := findTreeItem(&data, "shared.md")
	if shared == nil {
		t.Fatalf("expected shared.md in tree")
	}
	if len(shared.Worktrees) != 2 {
		t.Fatalf("expected shared.md in 2 worktrees, got %d: %v", len(shared.Worktrees), shared.Worktrees)
	}

	featureOnly := findTreeItem(&data, "feature-only.md")
	if featureOnly == nil {
		t.Fatalf("expected feature-only.md in unified tree")
	}
	if len(featureOnly.Worktrees) != 1 {
		t.Fatalf("expected feature-only.md in 1 worktree, got %d: %v", len(featureOnly.Worktrees), featureOnly.Worktrees)
	}
	if featureOnly.Worktrees[0] != "feature" {
		t.Fatalf("expected feature-only.md worktree to be 'feature', got %s", featureOnly.Worktrees[0])
	}
}

func TestHandleFileMeta(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "checkout", "-b", "main")

	os.MkdirAll(filepath.Join(root, "docs"), 0o755)
	os.WriteFile(filepath.Join(root, "readme.md"), []byte("# readme"), 0o644)
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("notes"), 0o644)
	os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("# guide"), 0o644)
	os.WriteFile(filepath.Join(root, "docs", "draft.md"), []byte("# draft"), 0o644)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")

	srv := newTestServer(root, []string{"*.md"}, []string{"docs/draft.md"}, false)
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	resp := mustGet(t, ts.URL+"/api/file-meta")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload struct {
		Files []struct {
			Path         string `json:"path"`
			ModifiedAtMs int64  `json:"modifiedAtMs"`
			Size         int64  `json:"size"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	paths := map[string]struct{}{}
	for _, file := range payload.Files {
		if file.ModifiedAtMs == 0 || file.Size == 0 {
			t.Fatalf("expected version metadata for %s: %+v", file.Path, file)
		}
		paths[file.Path] = struct{}{}
	}
	if _, ok := paths["readme.md"]; !ok {
		t.Fatalf("expected readme.md in file metadata")
	}
	if _, ok := paths["docs/guide.md"]; !ok {
		t.Fatalf("expected docs/guide.md in file metadata")
	}
	if _, ok := paths["notes.txt"]; ok {
		t.Fatalf("expected notes.txt to be excluded by include pattern")
	}
	if _, ok := paths["docs/draft.md"]; ok {
		t.Fatalf("expected docs/draft.md to be excluded")
	}
}

func TestHandleTreeIncludesFileMeta(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "checkout", "-b", "main")

	os.WriteFile(filepath.Join(root, "readme.md"), []byte("# readme"), 0o644)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")

	srv := newTestServer(root, []string{"*.md"}, nil, false)
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	resp := mustGet(t, ts.URL+"/api/tree")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var data tree.TreeItem
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("decode: %v", err)
	}
	item := findTreeItem(&data, "readme.md")
	if item == nil {
		t.Fatalf("expected readme.md in tree")
	}
	if item.ModifiedAtMs == 0 || item.Size == 0 {
		t.Fatalf("expected readme.md metadata, got modifiedAtMs=%d size=%d", item.ModifiedAtMs, item.Size)
	}
}
