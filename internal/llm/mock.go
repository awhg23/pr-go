package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/awhg23/pr-go/internal/review"
)

type MockReviewer struct{}

func (MockReviewer) Review(_ context.Context, input review.Input) (review.Result, error) {
	findings := make([]review.Finding, 0)
	for idx, file := range input.ChangedFiles {
		lower := strings.ToLower(file.Path + "\n" + file.Patch)
		if strings.Contains(lower, "todo") {
			findings = append(findings, review.Finding{
				ID:         fmt.Sprintf("F-%03d", idx+1),
				FilePath:   file.Path,
				Severity:   "medium",
				Category:   "maintainability",
				Title:      "TODO left in changed code",
				Reason:     "The changed diff contains a TODO marker that may represent unfinished work.",
				Suggestion: "Resolve the TODO or link it to a tracked follow-up before approval.",
			})
		}
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") {
			findings = append(findings, review.Finding{
				ID:         fmt.Sprintf("F-%03d", idx+1),
				FilePath:   file.Path,
				Severity:   "high",
				Category:   "security",
				Title:      "Sensitive credential-related change",
				Reason:     "The changed diff touches credential-related terms and should receive manual security review.",
				Suggestion: "Verify no secret is committed and ensure credential handling follows repository policy.",
			})
		}
	}

	summary := "Mock review completed. No obvious heuristic issues were detected."
	if len(findings) > 0 {
		summary = fmt.Sprintf("Mock review completed with %d structured finding(s).", len(findings))
	}
	result := review.Result{
		Summary:  summary,
		Findings: findings,
		ModelInvocation: &review.ModelInvocation{
			Provider:      "mock",
			Model:         "heuristic",
			PromptVersion: review.CurrentPromptVersion,
			Status:        "success",
		},
	}
	review.EnsureSchema(&result)
	return result, nil
}
