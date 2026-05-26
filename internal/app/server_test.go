package app

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/awhg23/pr-go/internal/review"
	"github.com/awhg23/pr-go/internal/store"
)

func TestHandleWebhookQueuesDelivery(t *testing.T) {
	fake := &fakeStore{inserted: true}
	server := &Server{
		cfg:    ServerConfig{WebhookSecret: "secret"},
		store:  fake,
		jobs:   make(chan reviewJob, 1),
		logger: discardLogger(t),
	}
	body := `{"action":"opened","installation":{"id":1},"repository":{"full_name":"owner/repo"},"pull_request":{"number":1,"head":{"sha":"abc"}}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signature("secret", []byte(body)))
	req.Header.Set("X-GitHub-Event", EventPullRequest)
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	rec := httptest.NewRecorder()

	server.handleWebhook(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "queued" {
		t.Fatalf("body = %q, want queued", got)
	}
	if fake.recorded.DeliveryID != "delivery-1" {
		t.Fatalf("delivery = %+v, want delivery-1", fake.recorded)
	}
	if len(server.jobs) != 1 {
		t.Fatalf("jobs len = %d, want 1", len(server.jobs))
	}
}

func TestHandleWebhookDeduplicatesDelivery(t *testing.T) {
	fake := &fakeStore{inserted: false}
	server := &Server{
		cfg:    ServerConfig{WebhookSecret: "secret"},
		store:  fake,
		jobs:   make(chan reviewJob, 1),
		logger: discardLogger(t),
	}
	body := `{"action":"opened","installation":{"id":1},"repository":{"full_name":"owner/repo"},"pull_request":{"number":1,"head":{"sha":"abc"}}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signature("secret", []byte(body)))
	req.Header.Set("X-GitHub-Event", EventPullRequest)
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	rec := httptest.NewRecorder()

	server.handleWebhook(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "duplicate" {
		t.Fatalf("body = %q, want duplicate", got)
	}
	if len(server.jobs) != 0 {
		t.Fatalf("jobs len = %d, want 0", len(server.jobs))
	}
}

func TestHandleWebhookQueuesCommandDelivery(t *testing.T) {
	fake := &fakeStore{inserted: true}
	server := &Server{
		cfg:    ServerConfig{WebhookSecret: "secret"},
		store:  fake,
		jobs:   make(chan reviewJob, 1),
		logger: discardLogger(t),
	}
	body := `{"action":"created","installation":{"id":1},"repository":{"full_name":"owner/repo"},"issue":{"number":1,"pull_request":{}},"comment":{"body":"/ai-review","user":{"login":"maintainer"}}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signature("secret", []byte(body)))
	req.Header.Set("X-GitHub-Event", EventIssueComment)
	req.Header.Set("X-GitHub-Delivery", "delivery-2")
	rec := httptest.NewRecorder()

	server.handleWebhook(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "queued" {
		t.Fatalf("body = %q, want queued", got)
	}
	if len(server.jobs) != 1 {
		t.Fatalf("jobs len = %d, want 1", len(server.jobs))
	}
}

func TestHandleWebhookIgnoresNonCommandComment(t *testing.T) {
	fake := &fakeStore{inserted: true}
	server := &Server{
		cfg:    ServerConfig{WebhookSecret: "secret"},
		store:  fake,
		jobs:   make(chan reviewJob, 1),
		logger: discardLogger(t),
	}
	body := `{"action":"created","installation":{"id":1},"repository":{"full_name":"owner/repo"},"issue":{"number":1,"pull_request":{}},"comment":{"body":"hello","user":{"login":"maintainer"}}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signature("secret", []byte(body)))
	req.Header.Set("X-GitHub-Event", EventIssueComment)
	req.Header.Set("X-GitHub-Delivery", "delivery-3")
	rec := httptest.NewRecorder()

	server.handleWebhook(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "ignored" {
		t.Fatalf("body = %q, want ignored", got)
	}
	if len(server.jobs) != 0 {
		t.Fatalf("jobs len = %d, want 0", len(server.jobs))
	}
}

func discardLogger(t *testing.T) *log.Logger {
	t.Helper()
	return log.New(io.Discard, "", 0)
}

type fakeStore struct {
	inserted bool
	recorded store.Delivery
}

func (f *fakeStore) EnsureSchema(context.Context) error { return nil }
func (f *fakeStore) RecordDelivery(_ context.Context, d store.Delivery) (bool, error) {
	f.recorded = d
	return f.inserted, nil
}
func (f *fakeStore) MarkDeliveryStatus(context.Context, string, string, string) error { return nil }
func (f *fakeStore) UpsertInstallation(context.Context, store.Installation) (int64, error) {
	return 0, nil
}
func (f *fakeStore) UpsertRepository(context.Context, store.Repository) (int64, error) {
	return 0, nil
}
func (f *fakeStore) UpsertPullRequest(context.Context, store.PullRequest) (int64, error) {
	return 0, nil
}
func (f *fakeStore) EnsurePullRequest(context.Context, int64, int) (int64, error) {
	return 0, nil
}
func (f *fakeStore) UpdatePullRequestApprovalStatus(context.Context, int64, string) error {
	return nil
}
func (f *fakeStore) CreateReviewRun(context.Context, store.ReviewRun) (int64, error) {
	return 0, nil
}
func (f *fakeStore) UpdateReviewRunHeadSHA(context.Context, int64, string) error {
	return nil
}
func (f *fakeStore) FinishReviewRun(context.Context, int64, string, string) error { return nil }
func (f *fakeStore) SaveFindings(context.Context, int64, int64, []review.Finding) error {
	return nil
}
func (f *fakeStore) SaveRiskScore(context.Context, int64, int64, review.Risk) error {
	return nil
}
func (f *fakeStore) SaveModelInvocation(context.Context, int64, *review.ModelInvocation) error {
	return nil
}
func (f *fakeStore) SaveReviewComment(context.Context, int64, int64, string, string) error {
	return nil
}
func (f *fakeStore) CreateCommentCommand(context.Context, store.CommentCommand) (int64, error) {
	return 0, nil
}
func (f *fakeStore) FinishCommentCommand(context.Context, int64, string, string, string) error {
	return nil
}
func (f *fakeStore) LatestRiskScore(context.Context, int64) (store.RiskSnapshot, bool, error) {
	return store.RiskSnapshot{}, false, nil
}
func (f *fakeStore) LatestSuccessfulReviewRun(context.Context, int64) (store.ReviewRunSnapshot, bool, error) {
	return store.ReviewRunSnapshot{}, false, nil
}
func (f *fakeStore) ListOpenFindings(context.Context, int64) ([]store.FindingSnapshot, error) {
	return nil, nil
}
func (f *fakeStore) DismissFinding(context.Context, int64, string, string, string) (bool, error) {
	return false, nil
}
func (f *fakeStore) SaveApprovalCheck(context.Context, store.ApprovalCheck) error {
	return nil
}
func (f *fakeStore) Audit(context.Context, store.AuditLog) error { return nil }
func (f *fakeStore) RecentHighRiskPRs(context.Context, string, int) ([]store.HighRiskPR, error) {
	return nil, nil
}
func (f *fakeStore) Close() error { return nil }
