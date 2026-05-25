package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/awhg23/pr-go/internal/github"
	"github.com/awhg23/pr-go/internal/llm"
	"github.com/awhg23/pr-go/internal/review"
)

type ServerConfig struct {
	Addr          string
	WebhookSecret string
	AppID         int64
	PrivateKeyPEM []byte
	Provider      string
	MaxDiffBytes  int
	Timeout       time.Duration
}

type Server struct {
	cfg      ServerConfig
	auth     *github.AppAuthenticator
	reviewer llm.Reviewer
	logger   *log.Logger
}

func NewServer(cfg ServerConfig, logger *log.Logger) (*Server, error) {
	if cfg.WebhookSecret == "" {
		return nil, fmt.Errorf("webhook secret is required")
	}
	if cfg.AppID <= 0 {
		return nil, fmt.Errorf("GitHub App ID is required")
	}
	if len(cfg.PrivateKeyPEM) == 0 {
		return nil, fmt.Errorf("GitHub App private key is required")
	}
	if cfg.MaxDiffBytes == 0 {
		cfg.MaxDiffBytes = 60000
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 90 * time.Second
	}
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if logger == nil {
		logger = log.Default()
	}

	reviewer, err := llm.NewReviewer(cfg.Provider)
	if err != nil {
		return nil, err
	}
	auth, err := github.NewAppAuthenticator(cfg.AppID, cfg.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}

	return &Server{cfg: cfg, auth: auth, reviewer: reviewer, logger: logger}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", s.handleWebhook)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

func (s *Server) ListenAndServe() error {
	s.logger.Printf("pr-go GitHub App server listening on %s", s.cfg.Addr)
	return http.ListenAndServe(s.cfg.Addr, s.Handler())
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 25<<20))
	if err != nil {
		http.Error(w, "read webhook body", http.StatusBadRequest)
		return
	}

	event, err := ParseWebhook(r.Header, body, s.cfg.WebhookSecret)
	if err != nil {
		http.Error(w, "invalid webhook", http.StatusUnauthorized)
		s.logger.Printf("reject webhook: %v", err)
		return
	}

	if !event.ShouldTriggerReview() {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("ignored\n"))
		return
	}

	if err := s.reviewPullRequest(r.Context(), event); err != nil {
		s.logger.Printf("review failed delivery=%s repo=%s action=%s: %v", event.DeliveryID, event.Repository.FullName, event.Action, err)
		http.Error(w, "review failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("reviewed\n"))
}

func (s *Server) reviewPullRequest(ctx context.Context, event WebhookEvent) error {
	if event.Installation.ID == 0 {
		return fmt.Errorf("webhook missing installation id")
	}
	if event.PullRequest == nil {
		return fmt.Errorf("webhook missing pull request")
	}
	owner, repo := event.RepositoryOwnerRepo()
	if owner == "" || repo == "" {
		return fmt.Errorf("webhook missing repository owner/name")
	}

	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	token, err := s.auth.InstallationToken(ctx, event.Installation.ID)
	if err != nil {
		return fmt.Errorf("create installation token: %w", err)
	}
	gh := github.NewClient(token)
	ref := github.PullRequestRef{Owner: owner, Repo: repo, Number: event.PullRequest.Number}

	pr, err := gh.FetchPullRequest(ctx, ref)
	if err != nil {
		return err
	}
	checks, err := gh.FetchChecksSummary(ctx, ref, pr.HeadSHA)
	if err != nil {
		s.logger.Printf("checks unavailable repo=%s pr=%d: %v", event.Repository.FullName, event.PullRequest.Number, err)
		checks = github.ChecksSummary{State: "unknown", Details: []string{"checks unavailable"}}
	}

	input := review.BuildInput(pr, s.cfg.MaxDiffBytes)
	input.CheckStatus = checks.State
	result, err := s.reviewer.Review(ctx, input)
	if err != nil {
		return fmt.Errorf("review with provider: %w", err)
	}
	review.EnsureSchema(&result)
	result.Risk = review.ScoreRisk(input, result.Findings)

	comment := review.RenderGitHubComment(input, result, checks)
	if err := gh.CreateIssueComment(ctx, ref, comment); err != nil {
		return fmt.Errorf("publish PR comment: %w", err)
	}
	return nil
}

func (e WebhookEvent) RepositoryOwnerRepo() (string, string) {
	if e.Repository.Owner.Login != "" && e.Repository.Name != "" {
		return e.Repository.Owner.Login, e.Repository.Name
	}
	parts := strings.SplitN(e.Repository.FullName, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
