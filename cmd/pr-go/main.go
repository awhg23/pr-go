package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/awhg23/pr-go/internal/app"
	"github.com/awhg23/pr-go/internal/github"
	"github.com/awhg23/pr-go/internal/llm"
	"github.com/awhg23/pr-go/internal/review"
)

type config struct {
	Server        bool
	Addr          string
	PRURL         string
	GitHubToken   string
	AppID         int64
	PrivateKey    string
	WebhookSecret string
	Provider      string
	Output        string
	MaxDiffBytes  int
	Timeout       time.Duration
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "pr-go: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := parseFlags()
	if cfg.Server {
		return runServer(cfg)
	}
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

func runServer(cfg config) error {
	privateKey := []byte(os.Getenv("GITHUB_APP_PRIVATE_KEY"))
	if cfg.PrivateKey != "" {
		var err error
		privateKey, err = os.ReadFile(cfg.PrivateKey)
		if err != nil {
			return fmt.Errorf("read --github-app-private-key-file: %w", err)
		}
	}
	server, err := app.NewServer(app.ServerConfig{
		Addr:          cfg.Addr,
		WebhookSecret: cfg.WebhookSecret,
		AppID:         cfg.AppID,
		PrivateKeyPEM: privateKey,
		Provider:      cfg.Provider,
		MaxDiffBytes:  cfg.MaxDiffBytes,
		Timeout:       cfg.Timeout,
	}, nil)
	if err != nil {
		return err
	}
	return server.ListenAndServe()
}

func parseFlags() config {
	var cfg config
	flag.BoolVar(&cfg.Server, "server", false, "run GitHub App webhook server")
	flag.StringVar(&cfg.Addr, "addr", envDefault("PR_GO_ADDR", ":8080"), "server listen address")
	flag.StringVar(&cfg.PRURL, "pr-url", "", "GitHub pull request URL")
	flag.StringVar(&cfg.GitHubToken, "github-token", os.Getenv("GITHUB_TOKEN"), "GitHub token, defaults to GITHUB_TOKEN")
	flag.Int64Var(&cfg.AppID, "github-app-id", envInt64Default("GITHUB_APP_ID", 0), "GitHub App ID")
	flag.StringVar(&cfg.PrivateKey, "github-app-private-key-file", os.Getenv("GITHUB_APP_PRIVATE_KEY_FILE"), "path to GitHub App private key PEM")
	flag.StringVar(&cfg.WebhookSecret, "webhook-secret", os.Getenv("GITHUB_WEBHOOK_SECRET"), "GitHub webhook secret")
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

func envInt64Default(name string, fallback int64) int64 {
	if v := os.Getenv(name); v != "" {
		var parsed int64
		if _, err := fmt.Sscan(v, &parsed); err == nil {
			return parsed
		}
	}
	return fallback
}
