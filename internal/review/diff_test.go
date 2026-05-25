package review

import (
	"strings"
	"testing"
)

func TestCompressDiffMarksTruncation(t *testing.T) {
	files := []FileDiff{{Path: "internal/auth/token.go", Status: "modified", Patch: strings.Repeat("x", 200)}}
	diff, truncated, omitted, out := CompressDiff(files, 80)
	if !truncated {
		t.Fatal("expected truncated diff")
	}
	if omitted <= 0 {
		t.Fatalf("expected omitted bytes, got %d", omitted)
	}
	if len(diff) > 80 {
		t.Fatalf("diff length = %d, want <= 80", len(diff))
	}
	if !out[0].Truncated {
		t.Fatal("expected file to be marked truncated")
	}
}
