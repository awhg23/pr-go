package review

import (
	"strings"
	"testing"

	"github.com/awhg23/pr-go/internal/github"
)

func TestRenderGitHubCommentIncludesRiskChecksAndNoApprove(t *testing.T) {
	comment := RenderGitHubComment(
		Input{Owner: "owner", Repo: "repo", Number: 1},
		Result{
			Summary: "Looks small.",
			Risk:    Risk{Score: 20, Level: "low", Reasons: []string{"base review risk starts at 10"}},
		},
		github.ChecksSummary{State: "success", Details: []string{"test=success"}},
	)

	for _, want := range []string{"PR Approval Agent", "Risk Level", "test=success", "Next Steps", "Auto approve is disabled unless repository policy explicitly enables it"} {
		if !strings.Contains(comment, want) {
			t.Fatalf("comment missing %q:\n%s", want, comment)
		}
	}
}
