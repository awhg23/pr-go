package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/awhg23/pr-go/internal/github"
	"github.com/awhg23/pr-go/internal/llm"
	"github.com/awhg23/pr-go/internal/review"
)

type config struct {
	PRURL        string
	GitHubToken  string
	Provider     string
	Output       string
	MaxDiffBytes int
	Timeout      time.Duration
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "pr-go: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := parseFlags()
	if cfg.PRURL == "" {
		return errors.New("--pr-url is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	prRef, err := github.ParsePullRequestURL(cfg.PRURL)
	if err != nil {
		return err
	}

	gh := github.NewClient(cfg.GitHubToken)
	pr, err := gh.FetchPullRequest(ctx, prRef)
	if err != nil {
		return err
	}

	input := review.BuildInput(pr, cfg.MaxDiffBytes)
	reviewer, err := llm.NewReviewer(cfg.Provider)
	if err != nil {
		return err
	}

	result, err := reviewer.Review(ctx, input)
	if err != nil {
		return err
	}
	review.EnsureSchema(&result)
	result.Risk = review.ScoreRisk(input, result.Findings)

	switch cfg.Output {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	case "markdown":
		fmt.Print(review.RenderMarkdown(input, result))
		return nil
	default:
		return fmt.Errorf("unsupported --output %q, want json or markdown", cfg.Output)
	}
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.PRURL, "pr-url", "", "GitHub pull request URL")
	flag.StringVar(&cfg.GitHubToken, "github-token", os.Getenv("GITHUB_TOKEN"), "GitHub token, defaults to GITHUB_TOKEN")
	flag.StringVar(&cfg.Provider, "provider", envDefault("PR_GO_PROVIDER", "openai"), "review provider: openai or mock")
	flag.StringVar(&cfg.Output, "output", "markdown", "output format: markdown or json")
	flag.IntVar(&cfg.MaxDiffBytes, "max-diff-bytes", 60000, "maximum diff bytes sent to the reviewer")
	flag.DurationVar(&cfg.Timeout, "timeout", 90*time.Second, "overall command timeout")
	flag.Parse()
	return cfg
}

func envDefault(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
