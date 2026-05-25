package github

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestFetchPullRequestIntegration(t *testing.T) {
	prURL := os.Getenv("PR_GO_INTEGRATION_PR_URL")
	if prURL == "" {
		t.Skip("set PR_GO_INTEGRATION_PR_URL to run GitHub integration test")
	}

	ref, err := ParsePullRequestURL(prURL)
	if err != nil {
		t.Fatalf("ParsePullRequestURL returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pr, err := NewClient(os.Getenv("GITHUB_TOKEN")).FetchPullRequest(ctx, ref)
	if err != nil {
		t.Fatalf("FetchPullRequest returned error: %v", err)
	}
	if pr.Title == "" {
		t.Fatal("expected pull request title")
	}
}
