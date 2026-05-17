package diff

import "testing"

func TestComputeDiffAddedLine(t *testing.T) {
	base := "a\nb\n"
	current := "a\nb\nc\n"
	result := ComputeDiff(base, current, "main", "feature")
	if !result.HasDiff {
		t.Fatalf("expected diff to be detected")
	}
	if len(result.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(result.Lines))
	}
	if result.Lines[2].Type != LineAdded {
		t.Fatalf("expected last line to be added")
	}
}

func TestComputeDiffEmpty(t *testing.T) {
	result := ComputeDiff("", "", "main", "main")
	if result.HasDiff {
		t.Fatalf("expected no diff")
	}
	if len(result.Lines) != 0 {
		t.Fatalf("expected empty diff lines")
	}
}
