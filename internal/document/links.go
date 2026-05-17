package document

import (
	"net/url"
	"path"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// docExtensions は SPA 内ナビゲーションで開くファイル拡張子。
// それ以外の相対参照は raw アセットとして配信する。
var docExtensions = map[string]bool{
	".md":       true,
	".markdown": true,
	".txt":      true,
}

// rewriteRelativeLinks は AST を走査し、ローカル相対参照のリンク・画像 URL を
// SPA / raw エンドポイント向けに書き換える。
//
// - `.md`/`.txt` 等のドキュメント拡張子: `?file=<resolved>&worktree=<wt>`
// - その他資産: `/api/raw?path=<resolved>&worktree=<wt>`
//
// 外部 URL（http/https 等）、絶対パス（/...）、アンカーのみ（#...）、
// プロトコル相対（//host/...）、空 URL は変更しない。
func rewriteRelativeLinks(root ast.Node, currentDocPath, worktreeName string) {
	// SPA からのパスはスラッシュ区切り。Windows パスでも path 関数で扱う。
	currentDir := path.Dir(path.Clean(currentDocPath))
	if currentDir == "." {
		currentDir = ""
	}

	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := n.(type) {
		case *ast.Link:
			if newDest, ok := rewriteDestination(string(node.Destination), currentDir, worktreeName); ok {
				node.Destination = []byte(newDest)
			}
		case *ast.Image:
			if newDest, ok := rewriteDestination(string(node.Destination), currentDir, worktreeName); ok {
				node.Destination = []byte(newDest)
			}
		}
		return ast.WalkContinue, nil
	})
}

// rewriteDestination は一つの URL を判定して書き換える。
// 書き換え不要なら ok=false を返し、呼び出し側は何もしない。
func rewriteDestination(dest, currentDir, worktreeName string) (string, bool) {
	if dest == "" {
		return "", false
	}
	// アンカーのみ
	if strings.HasPrefix(dest, "#") {
		return "", false
	}
	// プロトコル相対
	if strings.HasPrefix(dest, "//") {
		return "", false
	}
	// 絶対パス（サイト内）
	if strings.HasPrefix(dest, "/") {
		return "", false
	}
	// スキーム付き（http, https, mailto, tel, data など）
	if u, err := url.Parse(dest); err == nil && u.Scheme != "" {
		return "", false
	}

	// fragment / query を分離
	pathPart, fragment := splitFragment(dest)
	pathPart, query := splitQuery(pathPart)

	if pathPart == "" {
		// 例: "?foo=bar" や "#frag" 単体は触らない
		return "", false
	}

	// 現在ドキュメントの位置を基準に解決
	resolved := path.Clean(path.Join(currentDir, pathPart))

	// 解決後がベースディレクトリの外を指す場合は触らない
	if resolved == ".." || strings.HasPrefix(resolved, "../") || resolved == "." {
		return "", false
	}

	ext := strings.ToLower(path.Ext(resolved))
	values := url.Values{}
	if docExtensions[ext] {
		values.Set("file", resolved)
		if worktreeName != "" {
			values.Set("worktree", worktreeName)
		}
		newURL := "?" + values.Encode()
		if query != "" {
			// 元 query を末尾に保持（情報損失を避ける目的のみ）
			newURL += "&" + query
		}
		if fragment != "" {
			newURL += "#" + fragment
		}
		return newURL, true
	}

	// 資産系は raw 配信エンドポイントへ
	values.Set("path", resolved)
	if worktreeName != "" {
		values.Set("worktree", worktreeName)
	}
	newURL := "/api/raw?" + values.Encode()
	if query != "" {
		newURL += "&" + query
	}
	if fragment != "" {
		newURL += "#" + fragment
	}
	return newURL, true
}

func splitFragment(s string) (string, string) {
	if i := strings.Index(s, "#"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func splitQuery(s string) (string, string) {
	if i := strings.Index(s, "?"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}
