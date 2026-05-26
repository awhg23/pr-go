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
	QueuePoll     time.Duration
	AdminToken    string
	AlertWebhook  string
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
	ID int64
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
	if cfg.QueuePoll == 0 {
		cfg.QueuePoll = 2 * time.Second
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
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/admin", s.handleAdminHome)
	mux.HandleFunc("/admin/repo", s.handleAdminRepo)
	mux.HandleFunc("/api/v1/repositories", s.handleRepositoriesAPI)
	mux.HandleFunc("/api/v1/repository", s.handleRepositoryAPI)
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

	if !event.ShouldTriggerReview() && !event.ShouldTriggerCommand() && !event.ShouldTriggerInstallation() {
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
	payloadJSON, err := json.Marshal(event)
	if err != nil {
		_ = s.store.MarkDeliveryStatus(r.Context(), event.DeliveryID, "failed", err.Error())
		http.Error(w, "encode webhook job failed", http.StatusInternalServerError)
		return
	}
	jobID, err := s.store.EnqueueWebhookJob(r.Context(), store.WebhookJob{
		DeliveryID:         event.DeliveryID,
		Event:              event.Event,
		Action:             event.Action,
		RepositoryFullName: event.Repository.FullName,
		PayloadJSON:        string(payloadJSON),
		Status:             "queued",
		MaxAttempts:        s.cfg.MaxRetries,
	})
	if err != nil {
		_ = s.store.MarkDeliveryStatus(r.Context(), event.DeliveryID, "failed", err.Error())
		http.Error(w, "enqueue webhook job failed", http.StatusInternalServerError)
		s.logger.Printf("enqueue webhook job failed delivery=%s: %v", event.DeliveryID, err)
		return
	}

	select {
	case s.jobs <- reviewJob{ID: jobID}:
	default:
		s.logger.Printf("worker signal queue is full; persisted job will be picked up by poller job_id=%d delivery=%s", jobID, event.DeliveryID)
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("queued\n"))
}

func (s *Server) startWorkers() {
	for i := 0; i < s.cfg.WorkerCount; i++ {
		workerID := fmt.Sprintf("worker-%d", i+1)
		go func() {
			for job := range s.jobs {
				s.processJob(job, workerID)
			}
		}()
		go func() {
			ticker := time.NewTicker(s.cfg.QueuePoll)
			defer ticker.Stop()
			for range ticker.C {
				s.drainJobs(workerID)
			}
		}()
	}
}

func (s *Server) drainJobs(workerID string) {
	for {
		if ok := s.processJob(reviewJob{}, workerID); !ok {
			return
		}
	}
}

func (s *Server) processJob(job reviewJob, workerID string) bool {
	ctx := context.Background()
	claimed, ok, err := s.store.ClaimWebhookJob(ctx, job.ID, workerID)
	if err != nil {
		s.logger.Printf("claim webhook job failed id=%d: %v", job.ID, err)
		return false
	}
	if !ok {
		return false
	}
	var event WebhookEvent
	err = json.Unmarshal([]byte(claimed.PayloadJSON), &event)
	if err == nil {
		_ = s.store.MarkDeliveryStatus(ctx, event.DeliveryID, "processing", "")
		err = s.processEvent(ctx, event)
	}
	if err == nil {
		_ = s.store.MarkDeliveryStatus(ctx, claimed.DeliveryID, "processed", "")
		_ = s.store.CompleteWebhookJob(ctx, claimed.ID)
		return true
	}

	s.logger.Printf("webhook job failed delivery=%s repo=%s action=%s attempt=%d/%d: %v",
		claimed.DeliveryID, claimed.RepositoryFullName, claimed.Action, claimed.Attempts, claimed.MaxAttempts, err)
	if claimed.Attempts < claimed.MaxAttempts {
		_ = s.store.MarkDeliveryStatus(ctx, claimed.DeliveryID, "retrying", err.Error())
		_ = s.store.RetryWebhookJob(ctx, claimed.ID, err.Error(), time.Now().Add(s.retryDelay(claimed.Attempts)))
		return true
	}
	_ = s.store.MarkDeliveryStatus(ctx, claimed.DeliveryID, "failed", err.Error())
	_ = s.store.FailWebhookJob(ctx, claimed.ID, err.Error())
	s.sendAlert(ctx, "webhook_job_failed", map[string]any{
		"delivery_id": claimed.DeliveryID,
		"event":       claimed.Event,
		"action":      claimed.Action,
		"repository":  claimed.RepositoryFullName,
		"attempts":    claimed.Attempts,
		"error":       err.Error(),
	})
	return true
}

func (s *Server) processEvent(ctx context.Context, event WebhookEvent) error {
	switch {
	case event.ShouldTriggerReview():
		return s.reviewPullRequest(ctx, event, "", "")
	case event.ShouldTriggerCommand():
		return s.handleCommand(ctx, event)
	case event.ShouldTriggerInstallation():
		return s.handleInstallationEvent(ctx, event)
	default:
		return nil
	}
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

func (s *Server) reviewPullRequest(ctx context.Context, event WebhookEvent, triggerType string, actor string) error {
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
	if triggerType == "" {
		triggerType = event.Event + "." + event.Action
	}
	if actor == "" {
		actor = event.Actor()
	}

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
		TriggerType:   triggerType,
		TriggerActor:  actor,
		HeadSHA:       event.PullRequest.Head.SHA,
		Status:        "running",
	})
	if err != nil {
		return fmt.Errorf("record review run: %w", err)
	}

	token, err := s.auth.InstallationToken(ctx, event.Installation.ID)
	if err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, actor, "github_app_token_failed", fmt.Errorf("create installation token: %w", err))
	}
	gh := github.NewClient(token)
	ref := github.PullRequestRef{Owner: owner, Repo: repo, Number: event.PullRequest.Number}

	pr, err := gh.FetchPullRequest(ctx, ref)
	if err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, actor, "github_fetch_pr_failed", err)
	}
	policyConfig, policyWarnings := loadRepositoryPolicy(ctx, gh, ref, pr.BaseSHA)
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
		return s.handleJobError(ctx, repoID, prID, runID, actor, "mysql_write_failed", fmt.Errorf("record pull request: %w", err))
	}
	if err := s.store.UpdateReviewRunHeadSHA(ctx, runID, pr.HeadSHA); err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, actor, "mysql_write_failed", fmt.Errorf("record review run head sha: %w", err))
	}
	for _, warning := range policyWarnings {
		if err := gh.CreateIssueComment(ctx, ref, warning); err != nil {
			s.logger.Printf("publish policy warning failed repo=%s pr=%d: %v", event.Repository.FullName, pr.Ref.Number, err)
		}
		detail, _ := json.Marshal(map[string]string{"warning": warning})
		_ = s.store.Audit(ctx, store.AuditLog{
			RepositoryID:  repoID,
			PullRequestID: prID,
			Actor:         actor,
			Action:        "policy_config_warning",
			DetailJSON:    string(detail),
		})
	}
	checks, err := gh.FetchChecksSummary(ctx, ref, pr.HeadSHA)
	if err != nil {
		s.logger.Printf("checks unavailable repo=%s pr=%d: %v", event.Repository.FullName, event.PullRequest.Number, err)
		checks = github.ChecksSummary{State: "unknown", Details: []string{"checks unavailable"}}
		detail, _ := json.Marshal(map[string]string{"error": err.Error()})
		_ = s.store.Audit(ctx, store.AuditLog{
			RepositoryID:  repoID,
			PullRequestID: prID,
			Actor:         actor,
			Action:        "github_checks_unavailable",
			DetailJSON:    string(detail),
		})
	}

	input := buildReviewInput(pr, s.cfg.MaxDiffBytes, policyConfig, policyWarnings)
	input.CheckStatus = checks.State
	reviewer, err := s.reviewerForPolicy(policyConfig)
	if err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, actor, "policy_config_failed", fmt.Errorf("create reviewer from policy: %w", err))
	}
	result, err := reviewer.Review(ctx, input)
	if err != nil {
		_ = s.store.SaveModelInvocation(ctx, runID, &review.ModelInvocation{
			Provider:     s.cfg.Provider,
			Status:       "failed",
			ErrorMessage: err.Error(),
		})
		return s.handleJobError(ctx, repoID, prID, runID, actor, "llm_review_failed", fmt.Errorf("review with provider: %w", err))
	}
	review.EnsureSchema(&result)
	result.Risk = review.ScoreRiskWithOptions(input, result.Findings, riskOptionsFromPolicy(policyConfig))

	if err := s.store.SaveModelInvocation(ctx, runID, result.ModelInvocation); err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, actor, "mysql_write_failed", fmt.Errorf("record model invocation: %w", err))
	}
	if err := s.store.SaveFindings(ctx, runID, prID, result.Findings); err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, actor, "mysql_write_failed", fmt.Errorf("record findings: %w", err))
	}
	if err := s.store.SaveRiskScore(ctx, runID, prID, result.Risk); err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, actor, "mysql_write_failed", fmt.Errorf("record risk score: %w", err))
	}

	comment := review.RenderGitHubComment(input, result, checks)
	if err := gh.CreateIssueComment(ctx, ref, comment); err != nil {
		_ = s.store.SaveReviewComment(ctx, runID, prID, "failed", err.Error())
		return s.handleJobError(ctx, repoID, prID, runID, actor, "github_comment_failed", fmt.Errorf("publish PR comment: %w", err))
	}
	if err := s.store.SaveReviewComment(ctx, runID, prID, "published", ""); err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, actor, "mysql_write_failed", fmt.Errorf("record review comment: %w", err))
	}
	detail, _ := json.Marshal(map[string]any{
		"risk_level": result.Risk.Level,
		"risk_score": result.Risk.Score,
		"head_sha":   pr.HeadSHA,
	})
	if err := s.store.Audit(ctx, store.AuditLog{
		RepositoryID:  repoID,
		PullRequestID: prID,
		Actor:         actor,
		Action:        "review_completed",
		DetailJSON:    string(detail),
	}); err != nil {
		return s.handleJobError(ctx, repoID, prID, runID, actor, "mysql_write_failed", fmt.Errorf("record audit log: %w", err))
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
