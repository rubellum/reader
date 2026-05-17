package diff

import (
	"strconv"
	"strings"
	"testing"
)

func BenchmarkComputeDiffLarge(b *testing.B) {
	base := make([]string, 20000)
	current := make([]string, 20000)
	for i := 0; i < len(base); i++ {
		base[i] = "line-" + strconv.Itoa(i)
		current[i] = base[i]
	}
	current[len(current)-1] = "line-changed"

	baseText := strings.Join(base, "\n")
	currentText := strings.Join(current, "\n")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ComputeDiff(baseText, currentText, "main", "feature")
	}
}
