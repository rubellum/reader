package document

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderRejectsAbsolutePath(t *testing.T) {
	baseDir := t.TempDir()
	renderer := NewRenderer(baseDir)

	absPath := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(absPath, []byte("# outside"), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}

	if _, err := renderer.Render(absPath, ""); err == nil {
		t.Fatalf("expected absolute path to be rejected")
	}
}

func TestRenderRejectsParentTraversal(t *testing.T) {
	baseDir := t.TempDir()
	renderer := NewRenderer(baseDir)

	parentFile := filepath.Join(filepath.Dir(baseDir), "outside.md")
	if err := os.WriteFile(parentFile, []byte("# outside"), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}

	if _, err := renderer.Render("../outside.md", ""); err == nil {
		t.Fatalf("expected parent traversal to be rejected")
	}
}

func TestRenderOk(t *testing.T) {
	baseDir := t.TempDir()
	renderer := NewRenderer(baseDir)

	filePath := filepath.Join(baseDir, "doc.md")
	if err := os.WriteFile(filePath, []byte("# Title"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	doc, err := renderer.Render("doc.md", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Path != "doc.md" {
		t.Fatalf("expected path to be preserved")
	}
	if doc.HTML == "" {
		t.Fatalf("expected HTML to be generated")
	}
}

func TestRenderOmitsRawHTMLByDefault(t *testing.T) {
	baseDir := t.TempDir()
	renderer := NewRenderer(baseDir)

	src := "# Title\n\n<script>alert(1)</script>\n\n<div onclick=\"alert(1)\">raw</div>\n\nbody"
	if err := os.WriteFile(filepath.Join(baseDir, "doc.md"), []byte(src), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	doc, err := renderer.Render("doc.md", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(doc.HTML, "<script") || strings.Contains(doc.HTML, "<div onclick") {
		t.Fatalf("raw HTML should not be rendered by default: %s", doc.HTML)
	}
	if !strings.Contains(doc.HTML, "body") {
		t.Fatalf("expected non-HTML markdown content to remain: %s", doc.HTML)
	}
}

func TestRenderRewritesRelativeLinks(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	src := strings.Join([]string{
		"[同階層](sibling.md)",
		"[ドット](./other.md)",
		"[親](../top.md)",
		"[ベース外](../../escape.md)",
		"[アンカー](#section)",
		"[外部](https://example.com)",
		"[mailto](mailto:foo@example.com)",
		"[絶対](/already/absolute.md)",
		"[txt](notes.txt)",
		"[fragment付き](other.md#sec)",
		"![画像](./pic.png)",
		"![絶対画像](/static/img.png)",
	}, "\n\n")
	if err := os.WriteFile(filepath.Join(baseDir, "docs", "current.md"), []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	renderer := NewRenderer(baseDir)
	doc, err := renderer.Render("docs/current.md", "feature")
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	checks := []struct {
		name        string
		mustHave    string
		mustNotHave string
	}{
		{name: "sibling md → query string with current dir", mustHave: `href="?file=docs%2Fsibling.md&amp;worktree=feature"`},
		{name: "./other.md → query string", mustHave: `href="?file=docs%2Fother.md&amp;worktree=feature"`},
		{name: "../top.md → ベース内に解決される", mustHave: `href="?file=top.md&amp;worktree=feature"`},
		{name: "ベース外 (../../) は不変", mustHave: `href="../../escape.md"`},
		{name: "anchor only stays as-is", mustHave: `href="#section"`},
		{name: "https stays", mustHave: `href="https://example.com"`},
		{name: "mailto stays", mustHave: `href="mailto:foo@example.com"`},
		{name: "absolute path stays", mustHave: `href="/already/absolute.md"`},
		{name: "txt also navigated via query", mustHave: `href="?file=docs%2Fnotes.txt&amp;worktree=feature"`},
		{name: "fragment preserved", mustHave: `?file=docs%2Fother.md&amp;worktree=feature#sec`},
		{name: "relative image → /api/raw", mustHave: `src="/api/raw?path=docs%2Fpic.png&amp;worktree=feature"`},
		{name: "absolute image stays", mustHave: `src="/static/img.png"`},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(doc.HTML, c.mustHave) {
				t.Fatalf("expected HTML to contain %q\n--- HTML ---\n%s", c.mustHave, doc.HTML)
			}
		})
	}
}

func TestRenderRewriteWithoutWorktree(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(baseDir, "doc.md"), []byte("[x](other.md)\n\n![p](pic.png)"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	renderer := NewRenderer(baseDir)
	doc, err := renderer.Render("doc.md", "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(doc.HTML, `href="?file=other.md"`) {
		t.Fatalf("expected ?file= without worktree param, got %s", doc.HTML)
	}
	if !strings.Contains(doc.HTML, `src="/api/raw?path=pic.png"`) {
		t.Fatalf("expected /api/raw without worktree param, got %s", doc.HTML)
	}
}
