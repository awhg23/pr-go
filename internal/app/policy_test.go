package app

import (
	"context"
	"strings"
	"testing"

	"github.com/awhg23/pr-go/internal/github"
	"github.com/awhg23/pr-go/internal/policy"
)

func TestLoadRepositoryPolicyInvalidConfigUsesDefaultWithWarning(t *testing.T) {
	client := fakePolicyFileClient{raw: "version: nope\n", found: true}
	cfg, warnings := loadRepositoryPolicy(context.Background(), client, github.PullRequestRef{Owner: "owner", Repo: "repo", Number: 1}, "base")
	if cfg.Approval.AutoApprove.Enabled {
		t.Fatal("invalid config should use safe default with auto approve disabled")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "配置无效") {
		t.Fatalf("warnings = %#v, want config warning", warnings)
	}
}

func TestBuildReviewInputAppliesIgnoredFilesAndLanguage(t *testing.T) {
	cfg := policy.DefaultConfig()
	cfg.Review.Language = "zh-CN"
	cfg.Review.IgnoreFiles = []string{"dist/**"}
	pr := github.PullRequest{
		Ref: github.PullRequestRef{Owner: "owner", Repo: "repo", Number: 1},
		Files: []github.ChangedFile{
			{Filename: "dist/app.js", Patch: "ignored"},
			{Filename: "main.go", Patch: "kept"},
		},
	}
	input := buildReviewInput(pr, 60000, cfg, []string{"warning"})
	if input.OutputLanguage != "zh-CN" {
		t.Fatalf("language = %q, want zh-CN", input.OutputLanguage)
	}
	if len(input.ChangedFiles) != 1 || input.ChangedFiles[0].Path != "main.go" {
		t.Fatalf("changed files = %#v, want only main.go", input.ChangedFiles)
	}
	if len(input.IgnoredFiles) != 1 || input.IgnoredFiles[0] != "dist/app.js" {
		t.Fatalf("ignored files = %#v, want dist/app.js", input.IgnoredFiles)
	}
}

type fakePolicyFileClient struct {
	raw   string
	found bool
	err   error
}

func (f fakePolicyFileClient) FetchTextFile(context.Context, github.PullRequestRef, string, string) (string, bool, error) {
	return f.raw, f.found, f.err
}
