package app

import (
	"context"
	"fmt"

	"github.com/awhg23/pr-go/internal/github"
	"github.com/awhg23/pr-go/internal/llm"
	"github.com/awhg23/pr-go/internal/policy"
	"github.com/awhg23/pr-go/internal/review"
)

type policyFileClient interface {
	FetchTextFile(context.Context, github.PullRequestRef, string, string) (string, bool, error)
}

func loadRepositoryPolicy(ctx context.Context, client policyFileClient, ref github.PullRequestRef, baseSHA string) (policy.Config, []string) {
	cfg := policy.DefaultConfig()
	raw, ok, err := client.FetchTextFile(ctx, ref, policy.FileName, baseSHA)
	if err != nil {
		return cfg, []string{fmt.Sprintf("读取 `%s` 失败：%v。已使用安全默认配置。", policy.FileName, err)}
	}
	if !ok {
		return cfg, nil
	}
	parsed, err := policy.Parse(raw)
	if err != nil {
		return cfg, []string{fmt.Sprintf("`%s` 配置无效：%v。已使用安全默认配置。", policy.FileName, err)}
	}
	return parsed, nil
}

func applyPolicyToPullRequest(pr github.PullRequest, cfg policy.Config) (github.PullRequest, []string) {
	paths := make([]string, 0, len(pr.Files))
	byPath := map[string]github.ChangedFile{}
	for _, file := range pr.Files {
		paths = append(paths, file.Filename)
		byPath[file.Filename] = file
	}
	kept, ignored := policy.FilterIgnored(paths, cfg.Review.IgnoreFiles)
	filtered := make([]github.ChangedFile, 0, len(kept))
	for _, p := range kept {
		filtered = append(filtered, byPath[p])
	}
	pr.Files = filtered
	return pr, ignored
}

func buildReviewInput(pr github.PullRequest, maxDiffBytes int, cfg policy.Config, warnings []string) review.Input {
	filteredPR, ignored := applyPolicyToPullRequest(pr, cfg)
	input := review.BuildInput(filteredPR, maxDiffBytes)
	input.IgnoredFiles = ignored
	input.OutputLanguage = cfg.Review.Language
	input.PolicyWarnings = append(input.PolicyWarnings, warnings...)
	return input
}

func riskOptionsFromPolicy(cfg policy.Config) review.RiskOptions {
	return review.RiskOptions{
		HighRiskPaths:       cfg.Review.HighRiskPaths,
		RequireChangedTests: cfg.Tests.RequireChangedTests,
		TestFilePatterns:    cfg.Tests.TestFilePatterns,
	}
}

func (s *Server) reviewerForPolicy(cfg policy.Config) (llm.Reviewer, error) {
	provider := policy.ProviderName(s.cfg.Provider, cfg.Model.Provider)
	if provider == "" {
		provider = s.cfg.Provider
	}
	if provider == s.cfg.Provider && (cfg.Model.Model == "" || cfg.Model.Model == "default") && cfg.Model.Temperature == nil {
		return s.reviewer, nil
	}
	return llm.NewReviewerWithOptions(provider, llm.Options{
		Model:       cfg.Model.Model,
		Temperature: cfg.Model.Temperature,
	})
}
