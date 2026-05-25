package github

import "testing"

func TestParsePullRequestURL(t *testing.T) {
	got, err := ParsePullRequestURL("https://github.com/owner/repo/pull/123")
	if err != nil {
		t.Fatalf("ParsePullRequestURL returned error: %v", err)
	}
	if got.Owner != "owner" || got.Repo != "repo" || got.Number != 123 {
		t.Fatalf("unexpected ref: %+v", got)
	}
}

func TestParsePullRequestURLRejectsNonPR(t *testing.T) {
	if _, err := ParsePullRequestURL("https://github.com/owner/repo/issues/123"); err == nil {
		t.Fatal("expected invalid URL error")
	}
}

func TestSummarizeChecks(t *testing.T) {
	summary := summarizeChecks("", nil, []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	}{
		{Name: "test", Status: "completed", Conclusion: "success"},
		{Name: "lint", Status: "completed", Conclusion: "failure"},
	})
	if summary.State != "failure" {
		t.Fatalf("state = %q, want failure", summary.State)
	}
	if len(summary.Details) != 2 {
		t.Fatalf("details = %v, want two entries", summary.Details)
	}
}
