package worktree

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree はgit worktreeの情報を表す
type Worktree struct {
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	Current  bool    `json:"current"`
	FileHash *string `json:"fileHash,omitempty"`
}

// GitRoot は指定ディレクトリの Git リポジトリルートを返す
func GitRoot(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("Git リポジトリではありません: %s", dir)
	}
	return strings.TrimSpace(string(output)), nil
}

// List は指定されたパスのgitリポジトリに関連するworktree一覧を返す
func List(basePath string) ([]Worktree, error) {
	// 絶対パスに変換
	absPath, err := filepath.Abs(basePath)
	if err != nil {
		return nil, err
	}

	// git worktree list --porcelain を実行
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = absPath
	output, err := cmd.Output()
	if err != nil {
		// gitリポジトリでない場合は空のリストを返す
		return []Worktree{}, nil
	}

	worktrees := parseWorktreeOutput(string(output), absPath)
	return worktrees, nil
}

// parseWorktreeOutput はgit worktree list --porcelainの出力をパースする
func parseWorktreeOutput(output string, currentPath string) []Worktree {
	var worktrees []Worktree
	var currentWorktree *Worktree

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			// 新しいworktreeエントリ
			if currentWorktree != nil {
				worktrees = append(worktrees, *currentWorktree)
			}
			path := strings.TrimPrefix(line, "worktree ")
			currentWorktree = &Worktree{
				Path:    path,
				Name:    filepath.Base(path),
				Current: isSamePath(path, currentPath),
			}
		} else if strings.HasPrefix(line, "branch ") {
			// ブランチ名を取得（refs/heads/を除去）
			branch := strings.TrimPrefix(line, "branch ")
			branch = strings.TrimPrefix(branch, "refs/heads/")
			if currentWorktree != nil {
				currentWorktree.Name = branch
			}
		}
	}

	// 最後のworktreeを追加
	if currentWorktree != nil {
		worktrees = append(worktrees, *currentWorktree)
	}

	return worktrees
}

// isSamePath は2つのパスが同じかどうかを判定する
func isSamePath(path1, path2 string) bool {
	abs1, err1 := normalizePath(path1)
	abs2, err2 := normalizePath(path2)
	if err1 != nil || err2 != nil {
		return path1 == path2
	}
	rel, err := filepath.Rel(abs1, abs2)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func normalizePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	return abs, nil
}

// FindByName は名前でworktreeを検索する
func FindByName(worktrees []Worktree, name string) *Worktree {
	for i := range worktrees {
		if worktrees[i].Name == name {
			return &worktrees[i]
		}
	}
	return nil
}

// GetFileHash はファイルのMD5ハッシュを計算する
func GetFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ListWithHashes はworktree一覧を返し、指定されたファイルパスのハッシュを含める。
// relSubdir は basePath（git root）からユーザーが指定したルートまでの相対パス。
// "."（または空文字）なら git root をそのまま basePath とみなす。
func ListWithHashes(basePath string, relSubdir string, relativePath string) ([]Worktree, error) {
	worktrees, err := List(basePath)
	if err != nil {
		return nil, err
	}

	for i := range worktrees {
		wt := &worktrees[i]

		wtBase := wt.Path
		if relSubdir != "" && relSubdir != "." {
			wtBase = filepath.Join(wt.Path, relSubdir)
		}

		filePath := filepath.Join(wtBase, relativePath)
		hash, err := GetFileHash(filePath)
		if err == nil {
			wt.FileHash = &hash
		}
		// エラー（=ファイル未存在）の場合は FileHash は nil のまま
	}

	return worktrees, nil
}
