package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/awhg23/pr-go/internal/github"
	"github.com/awhg23/pr-go/internal/llm"
	"github.com/awhg23/pr-go/internal/review"
	"github.com/awhg23/pr-go/internal/store"
)

type ServerConfig struct {
	Addr          string
	WebhookSecret string
	AppID         int64
	PrivateKeyPEM []byte
	Provider      string
	MySQLDSN      string
	Store         store.Store
	MaxDiffBytes  int
	Timeout       time.Duration
	WorkerCount   int
	MaxRetries    int
	RetryDelay    time.Duration
}

type Server struct {
	cfg      ServerConfig
	auth     *github.AppAuthenticator
	reviewer llm.Reviewer
	store    store.Store
	jobs     chan reviewJob
	logger   *log.Logger
}

type reviewJob struct {
	Event   WebhookEvent
	Attempt int
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
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 2
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = 2 * time.Second
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

	st := cfg.Store
	if st == nil {
		if cfg.MySQLDSN == "" {
			return nil, fmt.Errorf("mysql dsn is required")
		}
		mysqlStore, err := store.NewMySQLStore(cfg.MySQLDSN)
		if err != nil {
			return nil, err
		}
		st = mysqlStore
	}
	if err := st.EnsureSchema(context.Background()); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("ensure mysql schema: %w", err)
	}

	s := &Server{
		cfg:      cfg,
		auth:     auth,
		reviewer: reviewer,
		store:    st,
		jobs:     make(chan reviewJob, cfg.WorkerCount*8),
		logger:   logger,
	}
	s.startWorkers()
	return s, nil
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

func (s *Server) Close() error {
	if s.store == nil {
		return nil
	}
	return s.store.Close()
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

	inserted, err := s.store.RecordDelivery(r.Context(), store.Delivery{
		DeliveryID:         event.DeliveryID,
		Event:              event.Event,
		Action:             event.Action,
		RepositoryFullName: event.Repository.FullName,
		Status:             "queued",
	})
	if err != nil {
		http.Error(w, "record delivery failed", http.StatusInternalServerError)
		s.logger.Printf("record delivery failed delivery=%s: %v", event.DeliveryID, err)
		return
	}
	if !inserted {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("duplicate\n"))
		return
	}

	select {
	case s.jobs <- reviewJob{Event: event, Attempt: 1}:
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("queued\n"))
	default:
		err := fmt.Errorf("review queue is full")
		_ = s.store.MarkDeliveryStatus(r.Context(), event.DeliveryID, "failed", err.Error())
		http.Error(w, "review queue full", http.StatusServiceUnavailable)
	}
}

func (s *Server) startWorkers() {
	for i := 0; i < s.cfg.WorkerCount; i++ {
		go func() {
			for job := range s.jobs {
				s.processJob(job)
			}
		}()
	}
}

func (s *Server) processJob(job reviewJob) {
	ctx := context.Background()
	_ = s.store.MarkDeliveryStatus(ctx, job.Event.DeliveryID, "processing", "")
	err := s.reviewPullRequest(ctx, job.Event)
	if err == nil {
		_ = s.store.MarkDeliveryStatus(ctx, job.Event.DeliveryID, "processed", "")
		return
	}

	s.logger.Printf("review failed delivery=%s repo=%s action=%s attempt=%d: %v",
		job.Event.DeliveryID, job.Event.Repository.FullName, job.Event.Action, job.Attempt, err)
	if job.Attempt < s.cfg.MaxRetries {
		_ = s.store.MarkDeliveryStatus(ctx, job.Event.DeliveryID, "retrying", err.Error())
		next := reviewJob{Event: job.Event, Attempt: job.Attempt + 1}
		time.AfterFunc(s.retryDelay(job.Attempt), func() {
			s.jobs <- next
		})
		return
	}
	_ = s.store.MarkDeliveryStatus(ctx, job.Event.DeliveryID, "failed", err.Error())
}

func (s *Server) retryDelay(attempt int) time.Duration {
	delay := s.cfg.RetryDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
	}
	if delay > time.Minute {
		return time.Minute
	}
	return delay
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

	installationDBID, err := s.store.UpsertInstallation(ctx, store.Installation{
		InstallationID: event.Installation.ID,
		AccountLogin:   event.Repository.Owner.Login,
		AccountType:    "repository",
	})
	if err != nil {
		return fmt.Errorf("record installation: %w", err)
	}
	repoID, err := s.store.UpsertRepository(ctx, store.Repository{
		InstallationDBID: installationDBID,
		Owner:            owner,
		Name:             repo,
		FullName:         event.Repository.FullName,
	})
	if err != nil {
		return fmt.Errorf("record repository: %w", err)
	}
	prID, err := s.store.UpsertPullRequest(ctx, store.PullRequest{
		RepositoryID:   repoID,
		Number:         event.PullRequest.Number,
		HeadSHA:        event.PullRequest.Head.SHA,
		State:          "open",
		ApprovalStatus: "reviewing",
	})
	if err != nil {
		return fmt.Errorf("record pull request placeholder: %w", err)
	}
	runID, err := s.store.CreateReviewRun(ctx, store.ReviewRun{
		PullRequestID: prID,
		TriggerType:   event.Event + "." + event.Action,
		TriggerActor:  event.Sender.Login,
		HeadSHA:       event.PullRequest.Head.SHA,
		Status:        "running",
	})
	if err != nil {
		return fmt.Errorf("record review run: %w", err)
	}

	token, err := s.auth.InstallationToken(ctx, event.Installation.ID)
	if err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, event.Sender.Login, "github_app_token_failed", fmt.Errorf("create installation token: %w", err))
	}
	gh := github.NewClient(token)
	ref := github.PullRequestRef{Owner: owner, Repo: repo, Number: event.PullRequest.Number}

	pr, err := gh.FetchPullRequest(ctx, ref)
	if err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, event.Sender.Login, "github_fetch_pr_failed", err)
	}
	prID, err = s.store.UpsertPullRequest(ctx, store.PullRequest{
		RepositoryID:   repoID,
		Number:         pr.Ref.Number,
		Title:          pr.Title,
		AuthorLogin:    pr.AuthorLogin,
		BaseSHA:        pr.BaseSHA,
		HeadSHA:        pr.HeadSHA,
		State:          "open",
		ApprovalStatus: "reviewing",
	})
	if err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, event.Sender.Login, "mysql_write_failed", fmt.Errorf("record pull request: %w", err))
	}
	checks, err := gh.FetchChecksSummary(ctx, ref, pr.HeadSHA)
	if err != nil {
		s.logger.Printf("checks unavailable repo=%s pr=%d: %v", event.Repository.FullName, event.PullRequest.Number, err)
		checks = github.ChecksSummary{State: "unknown", Details: []string{"checks unavailable"}}
		detail, _ := json.Marshal(map[string]string{"error": err.Error()})
		_ = s.store.Audit(ctx, store.AuditLog{
			RepositoryID:  repoID,
			PullRequestID: prID,
			Actor:         event.Sender.Login,
			Action:        "github_checks_unavailable",
			DetailJSON:    string(detail),
		})
	}

	input := review.BuildInput(pr, s.cfg.MaxDiffBytes)
	input.CheckStatus = checks.State
	result, err := s.reviewer.Review(ctx, input)
	if err != nil {
		_ = s.store.SaveModelInvocation(ctx, runID, &review.ModelInvocation{
			Provider:     s.cfg.Provider,
			Status:       "failed",
			ErrorMessage: err.Error(),
		})
		return s.handleJobError(ctx, repoID, prID, runID, event.Sender.Login, "llm_review_failed", fmt.Errorf("review with provider: %w", err))
	}
	review.EnsureSchema(&result)
	result.Risk = review.ScoreRisk(input, result.Findings)

	if err := s.store.SaveModelInvocation(ctx, runID, result.ModelInvocation); err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, event.Sender.Login, "mysql_write_failed", fmt.Errorf("record model invocation: %w", err))
	}
	if err := s.store.SaveFindings(ctx, runID, prID, result.Findings); err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, event.Sender.Login, "mysql_write_failed", fmt.Errorf("record findings: %w", err))
	}
	if err := s.store.SaveRiskScore(ctx, runID, prID, result.Risk); err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, event.Sender.Login, "mysql_write_failed", fmt.Errorf("record risk score: %w", err))
	}

	comment := review.RenderGitHubComment(input, result, checks)
	if err := gh.CreateIssueComment(ctx, ref, comment); err != nil {
		_ = s.store.SaveReviewComment(ctx, runID, prID, "failed", err.Error())
		return s.handleJobError(ctx, repoID, prID, runID, event.Sender.Login, "github_comment_failed", fmt.Errorf("publish PR comment: %w", err))
	}
	if err := s.store.SaveReviewComment(ctx, runID, prID, "published", ""); err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, event.Sender.Login, "mysql_write_failed", fmt.Errorf("record review comment: %w", err))
	}
	detail, _ := json.Marshal(map[string]any{
		"risk_level": result.Risk.Level,
		"risk_score": result.Risk.Score,
		"head_sha":   pr.HeadSHA,
	})
	if err := s.store.Audit(ctx, store.AuditLog{
		RepositoryID:  repoID,
		PullRequestID: prID,
		Actor:         event.Sender.Login,
		Action:        "review_completed",
		DetailJSON:    string(detail),
	}); err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, event.Sender.Login, "mysql_write_failed", fmt.Errorf("record audit log: %w", err))
	}
	if err := s.store.FinishReviewRun(ctx, runID, "success", ""); err != nil {
		return fmt.Errorf("finish review run: %w", err)
	}
	if err := s.store.UpdatePullRequestApprovalStatus(ctx, prID, "reviewed"); err != nil {
		return fmt.Errorf("update pull request approval status: %w", err)
	}
	return nil
}

func (s *Server) handleJobError(ctx context.Context, repoID, prID, runID int64, actor, action string, err error) error {
	msg := err.Error()
	if runID > 0 {
		if finishErr := s.store.FinishReviewRun(ctx, runID, "failed", msg); finishErr != nil {
			s.logger.Printf("record failed review run failed run_id=%d: %v", runID, finishErr)
		}
	}
	if prID > 0 {
		if statusErr := s.store.UpdatePullRequestApprovalStatus(ctx, prID, "review_failed"); statusErr != nil {
			s.logger.Printf("record failed approval status failed pr_id=%d: %v", prID, statusErr)
		}
	}
	if repoID > 0 {
		detail, _ := json.Marshal(map[string]string{"error": msg})
		if auditErr := s.store.Audit(ctx, store.AuditLog{
			RepositoryID:  repoID,
			PullRequestID: prID,
			Actor:         actor,
			Action:        action,
			DetailJSON:    string(detail),
		}); auditErr != nil {
			s.logger.Printf("record audit log failed: %v", auditErr)
		}
	}
	return err
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
