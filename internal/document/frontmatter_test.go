package document

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractFrontmatter(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantFields []FrontmatterField
		wantBody   string
	}{
		{
			name:       "no frontmatter",
			input:      "# Hello\n\nbody",
			wantFields: nil,
			wantBody:   "# Hello\n\nbody",
		},
		{
			name:  "basic yaml frontmatter",
			input: "---\ntitle: Hello\ndate: 2026-05-09\n---\n# Body\n",
			wantFields: []FrontmatterField{
				{Key: "title", Value: "Hello"},
				{Key: "date", Value: "2026-05-09"},
			},
			wantBody: "# Body\n",
		},
		{
			name:  "quoted values",
			input: "---\ntitle: \"クォート付き\"\nslug: 'single-quoted'\n---\nbody",
			wantFields: []FrontmatterField{
				{Key: "title", Value: "クォート付き"},
				{Key: "slug", Value: "single-quoted"},
			},
			wantBody: "body",
		},
		{
			name:  "empty value",
			input: "---\ntitle:\nfoo: bar\n---\nbody",
			wantFields: []FrontmatterField{
				{Key: "title", Value: ""},
				{Key: "foo", Value: "bar"},
			},
			wantBody: "body",
		},
		{
			name:  "comment and blank lines ignored",
			input: "---\n# comment\ntitle: x\n\nfoo: y\n---\nbody",
			wantFields: []FrontmatterField{
				{Key: "title", Value: "x"},
				{Key: "foo", Value: "y"},
			},
			wantBody: "body",
		},
		{
			name:       "no closing fence keeps everything",
			input:      "---\ntitle: x\n# Body\n",
			wantFields: nil,
			wantBody:   "---\ntitle: x\n# Body\n",
		},
		{
			name:       "fence-like prefix but no newline (e.g. horizontal rule)",
			input:      "---hello\nbody",
			wantFields: nil,
			wantBody:   "---hello\nbody",
		},
		{
			name:  "CRLF line endings",
			input: "---\r\ntitle: x\r\n---\r\nbody\r\n",
			wantFields: []FrontmatterField{
				{Key: "title", Value: "x"},
			},
			wantBody: "body\r\n",
		},
		{
			name:  "list/flow values kept as raw string",
			input: "---\ntags: [foo, bar]\n---\nbody",
			wantFields: []FrontmatterField{
				{Key: "tags", Value: "[foo, bar]"},
			},
			wantBody: "body",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields, body := extractFrontmatter([]byte(tc.input))
			if len(fields) != len(tc.wantFields) {
				t.Fatalf("fields length: want %d, got %d (%+v)", len(tc.wantFields), len(fields), fields)
			}
			for i := range fields {
				if fields[i] != tc.wantFields[i] {
					t.Fatalf("fields[%d]: want %+v, got %+v", i, tc.wantFields[i], fields[i])
				}
			}
			if string(body) != tc.wantBody {
				t.Fatalf("body: want %q, got %q", tc.wantBody, string(body))
			}
		})
	}
}

func TestRenderRespectsFrontmatter(t *testing.T) {
	baseDir := t.TempDir()
	src := "---\ntitle: My Doc\nauthor: Alice\n---\n# Heading\n\nparagraph"
	if err := os.WriteFile(filepath.Join(baseDir, "doc.md"), []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	doc, err := NewRenderer(baseDir).Render("doc.md", "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// メタブロックが先頭に挿入される
	if !strings.HasPrefix(doc.HTML, `<aside class="document-meta">`) {
		t.Fatalf("expected meta block at top, got: %s", doc.HTML)
	}
	// title がメタタイトルとして表示される
	if !strings.Contains(doc.HTML, `<div class="document-meta-title">My Doc</div>`) {
		t.Fatalf("expected meta title, got: %s", doc.HTML)
	}
	// その他は dl に入る
	if !strings.Contains(doc.HTML, `<dt class="document-meta-key">author</dt><dd class="document-meta-value">Alice</dd>`) {
		t.Fatalf("expected author field, got: %s", doc.HTML)
	}
	// 本文の見出しは frontmatter 後に通常通り出る
	if !strings.Contains(doc.HTML, `<h1>Heading</h1>`) {
		t.Fatalf("expected h1 from body, got: %s", doc.HTML)
	}
	// frontmatter の生テキストは本文側に出ない
	if strings.Contains(doc.HTML, "title: My Doc") {
		t.Fatalf("raw frontmatter leaked into body: %s", doc.HTML)
	}
	// horizontal rule が本文先頭に勝手に出ていない（旧バグ）
	if strings.HasPrefix(doc.HTML[len(`<aside class="document-meta">`):], `<hr`) {
		t.Fatalf("unexpected hr at body start: %s", doc.HTML)
	}
}

func TestRenderEscapesFrontmatterValues(t *testing.T) {
	baseDir := t.TempDir()
	src := "---\ntitle: \"<script>alert(1)</script>\"\n---\nbody"
	if err := os.WriteFile(filepath.Join(baseDir, "doc.md"), []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	doc, err := NewRenderer(baseDir).Render("doc.md", "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(doc.HTML, "<script>alert(1)</script>") {
		t.Fatalf("frontmatter value not escaped: %s", doc.HTML)
	}
	if !strings.Contains(doc.HTML, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("expected escaped value, got: %s", doc.HTML)
	}
}
