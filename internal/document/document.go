package document

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

// Document はドキュメント情報を表す
type Document struct {
	HTML         string `json:"html"`
	Path         string `json:"path"`
	ModifiedAt   int64  `json:"modifiedAt"`
	ModifiedAtMs int64  `json:"modifiedAtMs"`
	Size         int64  `json:"size"`
}

// Renderer はMarkdownをHTMLに変換する
type Renderer struct {
	basePath string
	md       goldmark.Markdown
}

// NewRenderer は新しいRendererを作成する
func NewRenderer(basePath string) *Renderer {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM, // GitHub Flavored Markdown (テーブル等)
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithXHTML(),
		),
	)

	return &Renderer{
		basePath: basePath,
		md:       md,
	}
}

// Render は指定されたパスのMarkdownファイルをHTMLに変換する。
// worktreeName を渡すと、ドキュメント内の相対リンク・相対画像 URL を
// `?file=...&worktree=...` / `/api/raw?path=...&worktree=...` に書き換える。
// worktreeName が空文字でも書き換えは行われ、worktree パラメータは省略される。
func (r *Renderer) Render(relativePath, worktreeName string) (*Document, error) {
	// パスを正規化
	cleanPath := filepath.Clean(relativePath)

	// パストラバーサル対策
	if strings.HasPrefix(cleanPath, "..") {
		return nil, os.ErrPermission
	}

	fullPath := filepath.Join(r.basePath, cleanPath)

	// ベースパス外へのアクセスを拒否
	absBase, err := filepath.Abs(r.basePath)
	if err != nil {
		return nil, err
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(absBase, absPath)
	if err != nil {
		return nil, err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, os.ErrPermission
	}

	// ファイル情報を取得
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}

	// ファイル内容を読み込み
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, err
	}

	// frontmatter を抽出（あれば本文から取り除く）
	fields, body := extractFrontmatter(content)
	metaHTML := renderFrontmatterHTML(fields)

	// MarkdownをHTMLに変換（パース→相対リンク書き換え→レンダ）
	reader := text.NewReader(body)
	astRoot := r.md.Parser().Parse(reader)
	rewriteRelativeLinks(astRoot, relativePath, worktreeName)

	var buf bytes.Buffer
	if err := r.md.Renderer().Render(&buf, body, astRoot); err != nil {
		return nil, err
	}

	return &Document{
		HTML:         metaHTML + buf.String(),
		Path:         relativePath,
		ModifiedAt:   info.ModTime().Unix(),
		ModifiedAtMs: info.ModTime().UnixMilli(),
		Size:         info.Size(),
	}, nil
}
