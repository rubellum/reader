package tree

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExcludePatternsMatchWholePath(t *testing.T) {
	opts := BuildOptions{
		Exclude: []string{
			"docs/drafts/*",
			"node_modules/*",
		},
	}

	files := []string{
		"docs/ok.md",
		"docs/drafts/skip.md",
		"node_modules/pkg/skip.txt",
	}
	treeData := BuildFromGitFiles("root", files, opts)

	if hasPath(treeData, "docs/drafts") {
		t.Fatalf("expected drafts directory to be excluded")
	}
	if hasPath(treeData, "node_modules") {
		t.Fatalf("expected node_modules directory to be excluded")
	}
	if !hasPath(treeData, "docs/ok.md") {
		t.Fatalf("expected ok.md to be included")
	}
}

func TestIncludePatternsMatchFileName(t *testing.T) {
	opts := BuildOptions{
		Include: []string{"*.md"},
	}
	files := []string{"docs/a.md", "docs/b.txt"}
	treeData := BuildFromGitFiles("root", files, opts)
	if !hasPath(treeData, "docs/a.md") {
		t.Fatalf("expected md file to be included")
	}
	if hasPath(treeData, "docs/b.txt") {
		t.Fatalf("expected txt file to be excluded by include pattern")
	}
}

func TestExcludeSubstringPattern(t *testing.T) {
	opts := BuildOptions{
		Exclude: []string{"assets/img"},
	}
	files := []string{"assets/img/logo.png", "assets/note.md"}
	treeData := BuildFromGitFiles("root", files, opts)
	if hasPath(treeData, "assets/img") {
		t.Fatalf("expected img directory to be excluded")
	}
	if !hasPath(treeData, "assets/note.md") {
		t.Fatalf("expected note.md to be included")
	}
}

// ===== GitFiles テスト =====

func TestGitFilesNormal(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := initTestRepo(t)
	writeFile(t, filepath.Join(repo, "a.md"))
	os.MkdirAll(filepath.Join(repo, "docs"), 0o755)
	writeFile(t, filepath.Join(repo, "docs", "b.md"))
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "init")

	files, err := GitFiles(repo)
	if err != nil {
		t.Fatalf("GitFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}
}

func TestGitFilesIncludesUntracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := initTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.md"))
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "init")

	// 未追跡ファイルを作成
	writeFile(t, filepath.Join(repo, "untracked.md"))

	files, err := GitFiles(repo)
	if err != nil {
		t.Fatalf("GitFiles: %v", err)
	}

	found := map[string]bool{}
	for _, f := range files {
		found[f] = true
	}

	if !found["tracked.md"] {
		t.Fatalf("expected tracked.md in files: %v", files)
	}
	if !found["untracked.md"] {
		t.Fatalf("expected untracked.md in files: %v", files)
	}
}

func TestGitFilesExcludesDeletedTrackedFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := initTestRepo(t)
	writeFile(t, filepath.Join(repo, "deleted.md"))
	writeFile(t, filepath.Join(repo, "kept.md"))
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "init")

	if err := os.Remove(filepath.Join(repo, "deleted.md")); err != nil {
		t.Fatalf("remove deleted.md: %v", err)
	}

	files, err := GitFiles(repo)
	if err != nil {
		t.Fatalf("GitFiles: %v", err)
	}

	found := map[string]bool{}
	for _, f := range files {
		found[f] = true
	}

	if found["deleted.md"] {
		t.Fatalf("expected deleted tracked file to be excluded: %v", files)
	}
	if !found["kept.md"] {
		t.Fatalf("expected kept.md in files: %v", files)
	}
}

func TestGitFilesEmpty(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := initTestRepo(t)
	files, err := GitFiles(repo)
	if err != nil {
		t.Fatalf("GitFiles: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(files))
	}
}

// ===== BuildFromGitFiles テスト =====

func TestBuildFromGitFilesBasic(t *testing.T) {
	files := []string{"docs/a.md", "docs/b.md", "readme.md"}
	tree := BuildFromGitFiles("root", files, BuildOptions{})

	if tree.Name != "root" {
		t.Fatalf("expected root name")
	}
	if !hasPath(tree, "docs/a.md") {
		t.Fatalf("expected docs/a.md")
	}
	if !hasPath(tree, "docs/b.md") {
		t.Fatalf("expected docs/b.md")
	}
	if !hasPath(tree, "readme.md") {
		t.Fatalf("expected readme.md")
	}
}

func TestBuildFromGitFilesNested(t *testing.T) {
	files := []string{"a/b/c/d.md"}
	tree := BuildFromGitFiles("root", files, BuildOptions{})

	if !hasPath(tree, "a/b/c/d.md") {
		t.Fatalf("expected a/b/c/d.md")
	}
	// ディレクトリノードの検証
	if !hasPath(tree, "a") {
		t.Fatalf("expected dir a")
	}
	if !hasPath(tree, "a/b") {
		t.Fatalf("expected dir a/b")
	}
}

func TestBuildFromGitFilesSortOrder(t *testing.T) {
	files := []string{"z.md", "a/x.md", "m.md", "b/y.md"}
	tree := BuildFromGitFiles("root", files, BuildOptions{})

	children := tree.Children
	if len(children) != 4 {
		t.Fatalf("expected 4 children, got %d", len(children))
	}
	// ディレクトリが先、ファイルが後、それぞれアルファベット順
	if children[0].Name != "a" || !children[0].IsDir {
		t.Fatalf("expected dir 'a' first, got %s (isDir=%v)", children[0].Name, children[0].IsDir)
	}
	if children[1].Name != "b" || !children[1].IsDir {
		t.Fatalf("expected dir 'b' second, got %s", children[1].Name)
	}
	if children[2].Name != "m.md" {
		t.Fatalf("expected 'm.md' third, got %s", children[2].Name)
	}
	if children[3].Name != "z.md" {
		t.Fatalf("expected 'z.md' fourth, got %s", children[3].Name)
	}
}

func TestBuildFromGitFilesIncludeByFilename(t *testing.T) {
	files := []string{"a.md", "b.txt"}
	opts := BuildOptions{Include: []string{"*.md"}}
	tree := BuildFromGitFiles("root", files, opts)

	if !hasPath(tree, "a.md") {
		t.Fatalf("expected a.md included")
	}
	if hasPath(tree, "b.txt") {
		t.Fatalf("expected b.txt excluded")
	}
}

func TestBuildFromGitFilesIncludeByPath(t *testing.T) {
	files := []string{"docs/a.md", "other/b.md"}
	opts := BuildOptions{Include: []string{"docs/*.md"}}
	tree := BuildFromGitFiles("root", files, opts)

	if !hasPath(tree, "docs/a.md") {
		t.Fatalf("expected docs/a.md included")
	}
	if hasPath(tree, "other/b.md") {
		t.Fatalf("expected other/b.md excluded")
	}
}

func TestBuildFromGitFilesExclude(t *testing.T) {
	files := []string{"app.md", "vendor/lib.md"}
	opts := BuildOptions{Exclude: []string{"vendor/*"}}
	tree := BuildFromGitFiles("root", files, opts)

	if !hasPath(tree, "app.md") {
		t.Fatalf("expected app.md included")
	}
	if hasPath(tree, "vendor/lib.md") {
		t.Fatalf("expected vendor/lib.md excluded")
	}
}

func TestBuildFromGitFilesExcludeNested(t *testing.T) {
	// git ls-files は深いパスを返す。!node_modules/* がネストされたパスにもマッチすること
	files := []string{
		"app.md",
		"node_modules/pkg/index.js",
		"node_modules/pkg/lib/util.js",
		"vendor/github.com/lib/foo.go",
	}
	opts := BuildOptions{Exclude: []string{"node_modules/*", "vendor/*"}}
	tree := BuildFromGitFiles("root", files, opts)

	if !hasPath(tree, "app.md") {
		t.Fatalf("expected app.md included")
	}
	if hasPath(tree, "node_modules/pkg/index.js") {
		t.Fatalf("expected node_modules/pkg/index.js excluded")
	}
	if hasPath(tree, "node_modules/pkg/lib/util.js") {
		t.Fatalf("expected node_modules/pkg/lib/util.js excluded")
	}
	if hasPath(tree, "vendor/github.com/lib/foo.go") {
		t.Fatalf("expected vendor/github.com/lib/foo.go excluded")
	}
	// node_modules ディレクトリ自体も存在しないこと
	if hasPath(tree, "node_modules") {
		t.Fatalf("expected node_modules dir excluded")
	}
}

func TestBuildFromGitFilesNoPatterns(t *testing.T) {
	files := []string{"a.md", "b.txt", "c.go"}
	tree := BuildFromGitFiles("root", files, BuildOptions{})

	if len(tree.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(tree.Children))
	}
}

func TestBuildFromGitFilesExcludesGitkeepByDefault(t *testing.T) {
	files := []string{".gitkeep", "docs/.gitkeep", "docs/readme.md"}
	tree := BuildFromGitFiles("root", files, BuildOptions{})

	if hasPath(tree, ".gitkeep") {
		t.Fatalf("expected root .gitkeep to be excluded by default")
	}
	if hasPath(tree, "docs/.gitkeep") {
		t.Fatalf("expected nested .gitkeep to be excluded by default")
	}
	if !hasPath(tree, "docs/readme.md") {
		t.Fatalf("expected docs/readme.md to be included")
	}
}

func TestBuildFromGitFilesCanExplicitlyIncludeGitkeep(t *testing.T) {
	files := []string{".gitkeep", "docs/.gitkeep", "docs/readme.md"}
	tree := BuildFromGitFiles("root", files, BuildOptions{Include: []string{".gitkeep"}})

	if !hasPath(tree, ".gitkeep") {
		t.Fatalf("expected root .gitkeep to be included when explicitly requested")
	}
	if !hasPath(tree, "docs/.gitkeep") {
		t.Fatalf("expected nested .gitkeep to be included when explicitly requested")
	}
	if hasPath(tree, "docs/readme.md") {
		t.Fatalf("expected docs/readme.md to be excluded by include pattern")
	}
}

func TestBuildFromGitFilesEmpty(t *testing.T) {
	tree := BuildFromGitFiles("root", []string{}, BuildOptions{})
	if len(tree.Children) != 0 {
		t.Fatalf("expected empty children")
	}
}

func TestBuildFromGitFilesSortDesc(t *testing.T) {
	files := []string{
		"alpha.md",
		"zeta.md",
		"docs/alpha.md",
		"docs/zeta.md",
		"notes/readme.md",
	}
	tree := BuildFromGitFiles("root", files, BuildOptions{SortDesc: true})

	wantRoot := []string{"notes", "docs", "zeta.md", "alpha.md"}
	if got := childNames(tree); !reflect.DeepEqual(got, wantRoot) {
		t.Fatalf("root children = %v, want %v", got, wantRoot)
	}

	docs := findItem(tree, "docs")
	if docs == nil {
		t.Fatal("docs not found")
	}
	wantDocs := []string{"zeta.md", "alpha.md"}
	if got := childNames(docs); !reflect.DeepEqual(got, wantDocs) {
		t.Fatalf("docs children = %v, want %v", got, wantDocs)
	}
}

// ===== MergeTreeItems テスト =====

func TestMergeTreeItems(t *testing.T) {
	baseFiles := []string{"docs/a.md", "readme.md"}
	otherFiles := []string{"docs/a.md", "docs/new.md"}

	base := BuildFromGitFiles("root", baseFiles, BuildOptions{})
	setWorktrees(base, "main")

	other := BuildFromGitFiles("root", otherFiles, BuildOptions{})

	merged := MergeTreeItemsWithOptions(base, other, "feature", BuildOptions{})

	// docs/a.md は両方に存在
	aMD := findItem(merged, "docs/a.md")
	if aMD == nil {
		t.Fatalf("expected docs/a.md in merged tree")
	}
	if !containsAll(aMD.Worktrees, "main", "feature") {
		t.Fatalf("expected worktrees [main, feature], got %v", aMD.Worktrees)
	}

	// readme.md は main のみ
	readme := findItem(merged, "readme.md")
	if readme == nil {
		t.Fatalf("expected readme.md in merged tree")
	}
	if !containsAll(readme.Worktrees, "main") || containsAny(readme.Worktrees, "feature") {
		t.Fatalf("expected worktrees [main], got %v", readme.Worktrees)
	}

	// docs/new.md は feature のみ
	newMD := findItem(merged, "docs/new.md")
	if newMD == nil {
		t.Fatalf("expected docs/new.md in merged tree")
	}
	if !containsAll(newMD.Worktrees, "feature") || containsAny(newMD.Worktrees, "main") {
		t.Fatalf("expected worktrees [feature], got %v", newMD.Worktrees)
	}
}

func TestMergeTreeItemsSortOrder(t *testing.T) {
	base := BuildFromGitFiles("root", []string{"z.md"}, BuildOptions{})
	setWorktrees(base, "main")
	other := BuildFromGitFiles("root", []string{"a/x.md", "m.md"}, BuildOptions{})

	merged := MergeTreeItemsWithOptions(base, other, "feature", BuildOptions{})

	children := merged.Children
	if len(children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(children))
	}
	if children[0].Name != "a" || !children[0].IsDir {
		t.Fatalf("expected dir 'a' first")
	}
	if children[1].Name != "m.md" {
		t.Fatalf("expected 'm.md' second, got %s", children[1].Name)
	}
	if children[2].Name != "z.md" {
		t.Fatalf("expected 'z.md' third, got %s", children[2].Name)
	}
}

func TestMergeTreeItemsSortDesc(t *testing.T) {
	base := BuildFromGitFiles("root", []string{"alpha.md", "docs/alpha.md"}, BuildOptions{SortDesc: true})
	setWorktrees(base, "main")
	other := BuildFromGitFiles("root", []string{"zeta.md", "docs/zeta.md"}, BuildOptions{SortDesc: true})

	merged := MergeTreeItemsWithOptions(base, other, "feature", BuildOptions{SortDesc: true})
	wantRoot := []string{"docs", "zeta.md", "alpha.md"}
	if got := childNames(merged); !reflect.DeepEqual(got, wantRoot) {
		t.Fatalf("root children = %v, want %v", got, wantRoot)
	}

	docs := findItem(merged, "docs")
	if docs == nil {
		t.Fatal("docs not found")
	}
	wantDocs := []string{"zeta.md", "alpha.md"}
	if got := childNames(docs); !reflect.DeepEqual(got, wantDocs) {
		t.Fatalf("docs children = %v, want %v", got, wantDocs)
	}
}

// ===== GitFiles ネストリポジトリ テスト =====

func TestGitFilesNestedRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// 親リポジトリを作成
	parent := initTestRepo(t)
	writeFile(t, filepath.Join(parent, "root.txt"))
	runGit(t, parent, "add", ".")
	runGit(t, parent, "commit", "-m", "init parent")

	// ネストされた Git リポジトリを作成
	nested := filepath.Join(parent, "nested")
	os.MkdirAll(nested, 0o755)
	runGit(t, nested, "init")
	runGit(t, nested, "config", "user.email", "test@example.com")
	runGit(t, nested, "config", "user.name", "Test User")
	writeFile(t, filepath.Join(nested, "doc.md"))
	runGit(t, nested, "add", ".")
	runGit(t, nested, "commit", "-m", "init nested")

	files, err := GitFiles(parent)
	if err != nil {
		t.Fatalf("GitFiles: %v", err)
	}

	found := map[string]bool{}
	for _, f := range files {
		found[f] = true
	}

	if !found["root.txt"] {
		t.Fatalf("expected root.txt in files: %v", files)
	}
	if !found["nested/doc.md"] {
		t.Fatalf("expected nested/doc.md in files: %v", files)
	}
}

func TestBuildFromGitFilesExcludeNestedPrefix(t *testing.T) {
	// ネストリポジトリからのファイルパス（prefix/node_modules/...）でも除外パターンが効くこと
	files := []string{
		"subrepo/app.md",
		"subrepo/node_modules/pkg/README.md",
		"subrepo/vendor/lib/foo.go",
	}
	opts := BuildOptions{Include: []string{"*.md"}, Exclude: []string{"node_modules/*", "vendor/*"}}
	tree := BuildFromGitFiles("root", files, opts)

	if !hasPath(tree, "subrepo/app.md") {
		t.Fatalf("expected subrepo/app.md included")
	}
	if hasPath(tree, "subrepo/node_modules/pkg/README.md") {
		t.Fatalf("expected subrepo/node_modules/pkg/README.md excluded")
	}
	if hasPath(tree, "subrepo/vendor/lib/foo.go") {
		t.Fatalf("expected subrepo/vendor/lib/foo.go excluded")
	}
}

// ===== ヘルパー =====

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func findItem(root *TreeItem, rel string) *TreeItem {
	if root == nil {
		return nil
	}
	if root.Path == rel {
		return root
	}
	for _, child := range root.Children {
		if found := findItem(child, rel); found != nil {
			return found
		}
	}
	return nil
}

func setWorktrees(item *TreeItem, name string) {
	if !item.IsDir {
		item.Worktrees = []string{name}
	}
	for _, child := range item.Children {
		setWorktrees(child, name)
	}
}

func containsAll(slice []string, items ...string) bool {
	m := map[string]bool{}
	for _, s := range slice {
		m[s] = true
	}
	for _, item := range items {
		if !m[item] {
			return false
		}
	}
	return true
}

func containsAny(slice []string, items ...string) bool {
	m := map[string]bool{}
	for _, s := range slice {
		m[s] = true
	}
	for _, item := range items {
		if m[item] {
			return true
		}
	}
	return false
}

func childNames(item *TreeItem) []string {
	names := make([]string, 0, len(item.Children))
	for _, child := range item.Children {
		names = append(names, child.Name)
	}
	return names
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func hasPath(root *TreeItem, rel string) bool {
	if root == nil {
		return false
	}
	if root.Path == rel {
		return true
	}
	for _, child := range root.Children {
		if hasPath(child, rel) {
			return true
		}
	}
	return false
}
