package document

import (
	"bytes"
	"html"
	"strings"
)

// FrontmatterField はパースされた frontmatter の一つのキー/値ペアを表す。
// Order を保持するため map ではなくスライスで返す。
type FrontmatterField struct {
	Key   string
	Value string
}

// extractFrontmatter は YAML スタイル frontmatter（`---` で挟まれたブロック）を
// 抽出して、パース済みフィールド列と本文（frontmatter を除いた残り）を返す。
//
// 対応するのは典型的な単一行 key: value のみ。リストやネストは値の生文字列として
// 保持するに留め、解釈はしない（用途は表示のみ）。
//
// content の先頭行が `---` でない場合や閉じ `---` が無い場合は何も抽出せず、
// 元の content をそのまま返す。
func extractFrontmatter(content []byte) ([]FrontmatterField, []byte) {
	// CRLF は LF に正規化したスキャンを行う
	const sep = "---"

	// 先頭行が "---" 系統で始まることを確認
	if !bytes.HasPrefix(content, []byte(sep)) {
		return nil, content
	}
	// "---" の直後が改行（CRLF/LF）または EOF であること
	rest := content[len(sep):]
	if len(rest) > 0 && rest[0] != '\n' && rest[0] != '\r' {
		// 例: "---foo" のように区切りでなければ frontmatter ではない
		return nil, content
	}
	// 開始行を消費
	rest = consumeNewline(rest)

	// 閉じの "---" 行を探す
	closeIdx := findClosingFence(rest)
	if closeIdx < 0 {
		// 閉じが無い → frontmatter として扱わない
		return nil, content
	}

	bodyOfFM := rest[:closeIdx]
	// 閉じフェンス自体（"---" + 改行）をスキップしたところが残り本文
	afterFence := rest[closeIdx+len(sep):]
	afterFence = consumeNewline(afterFence)

	fields := parseFrontmatterLines(bodyOfFM)
	return fields, afterFence
}

// findClosingFence は frontmatter の閉じ "---" 行の開始位置を返す。
// 見つからなければ -1。改行直後 or データ先頭に存在することを期待する。
func findClosingFence(b []byte) int {
	const fence = "---"
	// 行の先頭で "---" を探す
	idx := 0
	for idx < len(b) {
		// 行の終端を見つける
		nl := bytes.IndexByte(b[idx:], '\n')
		var line []byte
		if nl == -1 {
			line = b[idx:]
		} else {
			line = b[idx : idx+nl]
		}
		// CRLF の \r を取り除いて判定
		trimmed := bytes.TrimRight(line, "\r")
		if string(trimmed) == fence {
			return idx
		}
		if nl == -1 {
			break
		}
		idx += nl + 1
	}
	return -1
}

// consumeNewline は先頭の改行文字（\n または \r\n）を 1 つ消費する。
func consumeNewline(b []byte) []byte {
	if len(b) > 0 && b[0] == '\r' {
		b = b[1:]
	}
	if len(b) > 0 && b[0] == '\n' {
		b = b[1:]
	}
	return b
}

// parseFrontmatterLines は `key: value` 形式の単純な YAML を行単位でパースする。
// `#` で始まる行・空行は無視。コロンを含まない行も無視（マルチライン値は非対応）。
func parseFrontmatterLines(b []byte) []FrontmatterField {
	var fields []FrontmatterField
	for _, raw := range bytes.Split(b, []byte("\n")) {
		line := strings.TrimRight(string(raw), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		colon := strings.Index(trimmed, ":")
		if colon <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:colon])
		value := strings.TrimSpace(trimmed[colon+1:])
		// 値の引用符を取り除く（"..." or '...'）
		value = trimQuotes(value)
		if key == "" {
			continue
		}
		fields = append(fields, FrontmatterField{Key: key, Value: value})
	}
	return fields
}

func trimQuotes(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// renderFrontmatterHTML はメタフィールドを表示用の HTML ブロックに変換する。
// title フィールドがあれば見出しとして強調、その他は key: value のリストにする。
// XSS を避けるため値は html.EscapeString で必ずエスケープする。
func renderFrontmatterHTML(fields []FrontmatterField) string {
	if len(fields) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<aside class="document-meta">`)

	var rest []FrontmatterField
	for _, f := range fields {
		if strings.EqualFold(f.Key, "title") && f.Value != "" {
			b.WriteString(`<div class="document-meta-title">`)
			b.WriteString(html.EscapeString(f.Value))
			b.WriteString(`</div>`)
			continue
		}
		rest = append(rest, f)
	}

	if len(rest) > 0 {
		b.WriteString(`<dl class="document-meta-fields">`)
		for _, f := range rest {
			b.WriteString(`<dt class="document-meta-key">`)
			b.WriteString(html.EscapeString(f.Key))
			b.WriteString(`</dt><dd class="document-meta-value">`)
			b.WriteString(html.EscapeString(f.Value))
			b.WriteString(`</dd>`)
		}
		b.WriteString(`</dl>`)
	}

	b.WriteString(`</aside>`)
	return b.String()
}
