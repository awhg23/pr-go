package llm

import (
	"context"
	"testing"

	"github.com/awhg23/pr-go/internal/review"
)

func TestMockReviewerFindsTODO(t *testing.T) {
	result, err := (MockReviewer{}).Review(context.Background(), review.Input{
		ChangedFiles: []review.FileDiff{{Path: "main.go", Patch: "+ // TODO: finish this"}},
	})
	if err != nil {
		t.Fatalf("Review returned error: %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(result.Findings))
	}
	if result.Findings[0].Severity != "medium" {
		t.Fatalf("severity = %q, want medium", result.Findings[0].Severity)
	}
}
