package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
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
	MySQLDSN      string
	WorkerCount   int
	MaxRetries    int
	QueuePoll     time.Duration
	AdminToken    string
	AdminTokens   string
	AlertWebhook  string
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
		MySQLDSN:      cfg.MySQLDSN,
		MaxDiffBytes:  cfg.MaxDiffBytes,
		Timeout:       cfg.Timeout,
		WorkerCount:   cfg.WorkerCount,
		MaxRetries:    cfg.MaxRetries,
		QueuePoll:     cfg.QueuePoll,
		AdminToken:    cfg.AdminToken,
		AdminTokens:   cfg.AdminTokens,
		AlertWebhook:  cfg.AlertWebhook,
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
	flag.StringVar(&cfg.MySQLDSN, "mysql-dsn", os.Getenv("MYSQL_DSN"), "MySQL DSN for V2 persistence")
	flag.IntVar(&cfg.WorkerCount, "worker-count", envIntDefault("PR_GO_WORKER_COUNT", 2), "number of async review workers")
	flag.IntVar(&cfg.MaxRetries, "max-retries", envIntDefault("PR_GO_MAX_RETRIES", 3), "maximum async review attempts")
	flag.DurationVar(&cfg.QueuePoll, "queue-poll", envDurationDefault("PR_GO_QUEUE_POLL", 2*time.Second), "persistent queue poll interval")
	flag.StringVar(&cfg.AdminToken, "admin-token", os.Getenv("PR_GO_ADMIN_TOKEN"), "admin dashboard/API bearer token")
	flag.StringVar(&cfg.AdminTokens, "admin-tokens", os.Getenv("PR_GO_ADMIN_TOKENS"), "semicolon-separated admin tokens: name:token:scope1,scope2")
	flag.StringVar(&cfg.AlertWebhook, "alert-webhook", os.Getenv("PR_GO_ALERT_WEBHOOK_URL"), "optional alert webhook URL for final job failures")
	flag.StringVar(&cfg.Provider, "provider", envDefault("PR_GO_PROVIDER", "openai"), "review provider: openai-compatible, openai, deepseek, siliconflow, ollama, or mock")
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
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func envIntDefault(name string, fallback int) int {
	if v := os.Getenv(name); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return fallback
}

func envDurationDefault(name string, fallback time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if parsed, err := time.ParseDuration(v); err == nil {
			return parsed
		}
	}
	return fallback
}
