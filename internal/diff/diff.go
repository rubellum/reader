package diff

import (
	"strings"
)

// LineType は差分の行タイプを表す
type LineType string

const (
	LineUnchanged LineType = "unchanged"
	LineAdded     LineType = "added"
	LineRemoved   LineType = "removed"
)

// DiffLine は差分の1行を表す
type DiffLine struct {
	Type           LineType `json:"type"`
	BaseLineNum    *int     `json:"baseLineNum"`
	CurrentLineNum *int     `json:"currentLineNum"`
	Text           string   `json:"text"`
}

// DiffResult は差分計算の結果を表す
type DiffResult struct {
	HasDiff         bool       `json:"hasDiff"`
	BaseWorktree    string     `json:"baseWorktree"`
	CurrentWorktree string     `json:"currentWorktree"`
	Lines           []DiffLine `json:"lines"`
}

// ComputeDiff は2つのテキストの差分を計算する
func ComputeDiff(baseText, currentText, baseWorktree, currentWorktree string) *DiffResult {
	baseLines := splitLines(baseText)
	currentLines := splitLines(currentText)

	// LCSを計算
	lcs := computeLCS(baseLines, currentLines)

	// 差分を生成
	lines := generateDiff(baseLines, currentLines, lcs)

	// 差分があるかどうかを判定
	hasDiff := false
	for _, line := range lines {
		if line.Type != LineUnchanged {
			hasDiff = true
			break
		}
	}

	return &DiffResult{
		HasDiff:         hasDiff,
		BaseWorktree:    baseWorktree,
		CurrentWorktree: currentWorktree,
		Lines:           lines,
	}
}

// splitLines はテキストを行に分割する
func splitLines(text string) []string {
	if text == "" {
		return []string{}
	}
	lines := strings.Split(text, "\n")
	// 末尾の空行を削除
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// computeLCS はLCS（最長共通部分列）を計算する
func computeLCS(base, current []string) [][]int {
	m := len(base)
	n := len(current)

	// DPテーブルを作成
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	// LCSの長さを計算
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if base[i-1] == current[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				if dp[i-1][j] > dp[i][j-1] {
					dp[i][j] = dp[i-1][j]
				} else {
					dp[i][j] = dp[i][j-1]
				}
			}
		}
	}

	return dp
}

// generateDiff はLCSを使って差分を生成する
func generateDiff(base, current []string, dp [][]int) []DiffLine {
	var lines []DiffLine
	i := len(base)
	j := len(current)

	// バックトラックして差分を生成（逆順で追加）
	var result []DiffLine
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && base[i-1] == current[j-1] {
			// 共通行
			baseNum := i
			currentNum := j
			result = append(result, DiffLine{
				Type:           LineUnchanged,
				BaseLineNum:    &baseNum,
				CurrentLineNum: &currentNum,
				Text:           base[i-1],
			})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			// 追加行
			currentNum := j
			result = append(result, DiffLine{
				Type:           LineAdded,
				BaseLineNum:    nil,
				CurrentLineNum: &currentNum,
				Text:           current[j-1],
			})
			j--
		} else if i > 0 {
			// 削除行
			baseNum := i
			result = append(result, DiffLine{
				Type:           LineRemoved,
				BaseLineNum:    &baseNum,
				CurrentLineNum: nil,
				Text:           base[i-1],
			})
			i--
		}
	}

	// 結果を逆順にする
	for k := len(result) - 1; k >= 0; k-- {
		lines = append(lines, result[k])
	}

	return lines
}
