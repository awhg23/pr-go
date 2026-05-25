package review

import "testing"

func TestScoreRiskHighFinding(t *testing.T) {
	risk := ScoreRisk(Input{}, []Finding{{Severity: "high", Title: "unsafe token handling"}})
	if risk.Level != "medium" {
		t.Fatalf("level = %q, want medium", risk.Level)
	}
	if risk.Score <= 10 {
		t.Fatalf("score = %d, want above base", risk.Score)
	}
}

func TestScoreRiskHighRiskPath(t *testing.T) {
	risk := ScoreRisk(Input{ChangedFiles: []FileDiff{{Path: "internal/auth/token.go"}}}, nil)
	if risk.Score <= 10 {
		t.Fatalf("score = %d, want high-risk path adjustment", risk.Score)
	}
}
