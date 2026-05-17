package tree

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// TreeItem はツリーに表示される項目を表す
type TreeItem struct {
	Name      string      `json:"name"`
	Path      string      `json:"path"`
	IsDir     bool        `json:"isDir"`
	Children  []*TreeItem `json:"children,omitempty"`
	Worktrees []string    `json:"worktrees,omitempty"`
}

// BuildOptions はツリー構築のオプション
type BuildOptions struct {
	Include  []string // 包含パターン（空なら全ファイル）
	Exclude  []string // 除外パターン
	SortDesc bool     // true なら同一種別内を降順に並べる
}

// isExcludedByPath はパスが除外パターンにマッチするかを判定する
// 除外パターンはパス全体および各親パスに対して評価する
func isExcludedByPath(relPath string, excludePatterns []string) bool {
	normalizedPath := normalizePath(relPath)
	for _, p := range excludePatterns {
		pattern := normalizePath(p)
		if isGlobPattern(pattern) {
			// globパターンはパス全体と各親パスに対してマッチ
			// 例: "node_modules/*" は "node_modules/pkg/index.js" の
			//     親パス "node_modules/pkg" にマッチする
			if matchPathOrParent(normalizedPath, pattern) {
				return true
			}
			continue
		}
		// 通常の文字列は部分一致
		if strings.Contains(normalizedPath, pattern) {
			return true
		}
	}
	return false
}

// matchPathOrParent はパスまたはその親パスがパターンにマッチするか判定する
// ネストされた Git リポジトリのサポートのため、パスの各サフィックスに対してもマッチを試行する
// 例: "webchute/node_modules/pkg" は "node_modules/*" にマッチする
func matchPathOrParent(normalizedPath, pattern string) bool {
	// パス全体とそのサフィックスそれぞれについて、フルパスと親パスを試行
	for _, suffix := range pathSuffixes(normalizedPath) {
		if m, _ := path.Match(pattern, suffix); m {
			return true
		}
		p := suffix
		for {
			parent := path.Dir(p)
			if parent == p || parent == "." {
				break
			}
			if m, _ := path.Match(pattern, parent); m {
				return true
			}
			p = parent
		}
	}
	return false
}

// pathSuffixes はパスのすべてのサフィックスを返す
// 例: "a/b/c" → ["a/b/c", "b/c", "c"]
func pathSuffixes(p string) []string {
	suffixes := []string{p}
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			suffixes = append(suffixes, p[i+1:])
		}
	}
	return suffixes
}

// isGlobPattern はパターンがglob特殊文字を含むかを判定する
func isGlobPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

func normalizePath(p string) string {
	normalized := filepath.ToSlash(p)
	return strings.TrimPrefix(normalized, "./")
}

func isDefaultExcludedByPath(relPath string, includePatterns []string) bool {
	normalizedPath := normalizePath(relPath)
	if path.Base(normalizedPath) != ".gitkeep" {
		return false
	}
	if len(includePatterns) == 0 {
		return true
	}
	return !matchesIncludeByPath(normalizedPath, includePatterns)
}

// matchesIncludeByPath は Git root 相対パスに対して包含パターンをマッチする
func matchesIncludeByPath(relPath string, includePatterns []string) bool {
	if len(includePatterns) == 0 {
		return true
	}
	normalizedPath := normalizePath(relPath)
	name := path.Base(normalizedPath)
	for _, p := range includePatterns {
		pattern := normalizePath(p)
		if strings.Contains(pattern, "/") {
			// パスにセパレータを含むパターンはパス全体でマッチ
			if m, _ := path.Match(pattern, normalizedPath); m {
				return true
			}
		} else {
			// セパレータなしはファイル名でマッチ（後方互換）
			if m, _ := filepath.Match(p, name); m {
				return true
			}
		}
	}
	return false
}

// GitFiles は指定ディレクトリで git ls-files を実行してファイルリストを返す
// --cached: 追跡済みファイル、--others: 未追跡ファイル（gitignore含む）
// ネストされた Git リポジトリ（直下のサブディレクトリに .git がある場合）も探索する
func GitFiles(dir string) ([]string, error) {
	files, err := gitLsFiles(dir)
	if err != nil {
		return nil, err
	}

	// 直下のサブディレクトリにネストされた Git リポジトリがあれば、そのファイルも含める
	entries, err := os.ReadDir(dir)
	if err != nil {
		return files, nil
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		subDir := filepath.Join(dir, entry.Name())
		gitDir := filepath.Join(subDir, ".git")
		if _, statErr := os.Stat(gitDir); statErr != nil {
			continue
		}
		nestedFiles, err := gitLsFiles(subDir)
		if err != nil {
			continue
		}
		prefix := entry.Name()
		for _, f := range nestedFiles {
			files = append(files, prefix+"/"+f)
		}
	}

	return files, nil
}

// gitLsFiles は指定ディレクトリで git ls-files を実行する
func gitLsFiles(dir string) ([]string, error) {
	cmd := exec.Command("git", "-c", "core.quotePath=false", "ls-files", "--cached", "--others")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return []string{}, nil
	}
	return filterExistingFiles(dir, strings.Split(raw, "\n")), nil
}

func filterExistingFiles(baseDir string, files []string) []string {
	filtered := make([]string, 0, len(files))
	for _, file := range files {
		normalized := normalizePath(file)
		if normalized == "" {
			continue
		}
		info, err := os.Stat(filepath.Join(baseDir, filepath.FromSlash(normalized)))
		if err != nil || info.IsDir() {
			continue
		}
		filtered = append(filtered, normalized)
	}
	return filtered
}

// BuildFromGitFiles は git ls-files の結果からツリーを構築する
func BuildFromGitFiles(rootName string, files []string, opts BuildOptions) *TreeItem {
	root := &TreeItem{
		Name:  rootName,
		Path:  "",
		IsDir: true,
	}

	for _, file := range files {
		normalized := normalizePath(file)
		if normalized == "" {
			continue
		}

		// exclude チェック
		if isExcludedByPath(normalized, opts.Exclude) {
			continue
		}

		// .gitkeep は通常の表示対象から外す。明示的な include が一致する場合のみ表示する。
		if isDefaultExcludedByPath(normalized, opts.Include) {
			continue
		}

		// include チェック（root 相対パスベース）
		if !matchesIncludeByPath(normalized, opts.Include) {
			continue
		}

		// パスを分割してツリーに挿入
		parts := strings.Split(normalized, "/")
		insertIntoTree(root, parts, normalized)
	}

	// 各レベルをソート
	sortTree(root, opts.SortDesc)

	return root
}

// insertIntoTree はパスをツリーに挿入する
func insertIntoTree(parent *TreeItem, parts []string, fullPath string) {
	if len(parts) == 0 {
		return
	}

	name := parts[0]

	if len(parts) == 1 {
		// リーフ（ファイル）
		parent.Children = append(parent.Children, &TreeItem{
			Name:  name,
			Path:  fullPath,
			IsDir: false,
		})
		return
	}

	// ディレクトリを探す or 作成
	var dirItem *TreeItem
	dirPath := strings.Join(strings.Split(fullPath, "/")[:len(strings.Split(fullPath, "/"))-len(parts)+1], "/")
	for _, child := range parent.Children {
		if child.IsDir && child.Name == name {
			dirItem = child
			break
		}
	}
	if dirItem == nil {
		dirItem = &TreeItem{
			Name:  name,
			Path:  dirPath,
			IsDir: true,
		}
		parent.Children = append(parent.Children, dirItem)
	}

	insertIntoTree(dirItem, parts[1:], fullPath)
}

// sortTree はツリー全体をソートする（ディレクトリ先、ファイル後、各アルファベット順）
func sortTree(item *TreeItem, desc bool) {
	if item.Children == nil {
		return
	}

	// 子を再帰的にソート
	for _, child := range item.Children {
		sortTree(child, desc)
	}

	sort.Slice(item.Children, func(i, j int) bool {
		ci, cj := item.Children[i], item.Children[j]
		if ci.IsDir != cj.IsDir {
			return ci.IsDir // ディレクトリが先
		}
		if desc {
			return ci.Name > cj.Name
		}
		return ci.Name < cj.Name
	})
}

// MergeTreeItemsWithOptions は2つのツリーを統合（ユニオン）し、指定された並び順で整列する。
func MergeTreeItemsWithOptions(base *TreeItem, other *TreeItem, otherWorktree string, opts BuildOptions) *TreeItem {
	merged := &TreeItem{
		Name:  base.Name,
		Path:  base.Path,
		IsDir: base.IsDir,
	}

	if !base.IsDir {
		// ファイルノード: worktrees をコピー
		merged.Worktrees = make([]string, len(base.Worktrees))
		copy(merged.Worktrees, base.Worktrees)
		return merged
	}

	// base の children をコピー（深いコピー）
	childMap := map[string]*TreeItem{}
	for _, child := range base.Children {
		copied := deepCopyTree(child)
		childMap[child.Name+"|"+boolStr(child.IsDir)] = copied
	}

	// other の children をマージ
	for _, otherChild := range other.Children {
		key := otherChild.Name + "|" + boolStr(otherChild.IsDir)
		if existing, ok := childMap[key]; ok {
			if existing.IsDir {
				// ディレクトリ: 再帰マージ
				childMap[key] = MergeTreeItemsWithOptions(existing, otherChild, otherWorktree, opts)
			} else {
				// ファイル: worktrees に追加
				existing.Worktrees = append(existing.Worktrees, otherWorktree)
			}
		} else {
			// 新規ノード
			copied := deepCopyTree(otherChild)
			setWorktreesRecursive(copied, otherWorktree)
			childMap[key] = copied
		}
	}

	// map から children を構築
	for _, item := range childMap {
		merged.Children = append(merged.Children, item)
	}

	sortTree(merged, opts.SortDesc)

	return merged
}

func boolStr(b bool) string {
	if b {
		return "d"
	}
	return "f"
}

func deepCopyTree(item *TreeItem) *TreeItem {
	copied := &TreeItem{
		Name:  item.Name,
		Path:  item.Path,
		IsDir: item.IsDir,
	}
	if item.Worktrees != nil {
		copied.Worktrees = make([]string, len(item.Worktrees))
		copy(copied.Worktrees, item.Worktrees)
	}
	for _, child := range item.Children {
		copied.Children = append(copied.Children, deepCopyTree(child))
	}
	return copied
}

func setWorktreesRecursive(item *TreeItem, worktree string) {
	if !item.IsDir {
		item.Worktrees = []string{worktree}
	}
	for _, child := range item.Children {
		setWorktreesRecursive(child, worktree)
	}
}
