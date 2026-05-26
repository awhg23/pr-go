package store

import (
	"context"
	"time"

	"github.com/awhg23/pr-go/internal/review"
)

type Store interface {
	EnsureSchema(context.Context) error
	Ping(context.Context) error
	RecordDelivery(context.Context, Delivery) (bool, error)
	MarkDeliveryStatus(context.Context, string, string, string) error
	EnqueueWebhookJob(context.Context, WebhookJob) (int64, error)
	ClaimWebhookJob(context.Context, int64, string) (WebhookJob, bool, error)
	CompleteWebhookJob(context.Context, int64) error
	RetryWebhookJob(context.Context, int64, string, time.Time) error
	FailWebhookJob(context.Context, int64, string) error
	UpsertInstallation(context.Context, Installation) (int64, error)
	UpsertRepository(context.Context, Repository) (int64, error)
	UpsertPullRequest(context.Context, PullRequest) (int64, error)
	EnsurePullRequest(context.Context, int64, int) (int64, error)
	UpdatePullRequestApprovalStatus(context.Context, int64, string) error
	CreateReviewRun(context.Context, ReviewRun) (int64, error)
	UpdateReviewRunHeadSHA(context.Context, int64, string) error
	FinishReviewRun(context.Context, int64, string, string) error
	SaveFindings(context.Context, int64, int64, []review.Finding) error
	SaveRiskScore(context.Context, int64, int64, review.Risk) error
	SaveModelInvocation(context.Context, int64, *review.ModelInvocation) error
	SaveReviewComment(context.Context, int64, int64, string, string) error
	CreateCommentCommand(context.Context, CommentCommand) (int64, error)
	FinishCommentCommand(context.Context, int64, string, string, string) error
	LatestRiskScore(context.Context, int64) (RiskSnapshot, bool, error)
	LatestSuccessfulReviewRun(context.Context, int64) (ReviewRunSnapshot, bool, error)
	ListOpenFindings(context.Context, int64) ([]FindingSnapshot, error)
	DismissFinding(context.Context, int64, string, string, string) (bool, error)
	SaveApprovalCheck(context.Context, ApprovalCheck) error
	Audit(context.Context, AuditLog) error
	RecentHighRiskPRs(context.Context, string, int) ([]HighRiskPR, error)
	ListRepositorySummaries(context.Context, int) ([]RepositorySummary, error)
	RepositoryReport(context.Context, string, int) (RepositoryReport, error)
	Metrics(context.Context) (MetricsSnapshot, error)
	Close() error
}

type Delivery struct {
	DeliveryID         string
	Event              string
	Action             string
	RepositoryFullName string
	Status             string
	ErrorMessage       string
}

type WebhookJob struct {
	ID                 int64
	DeliveryID         string
	Event              string
	Action             string
	RepositoryFullName string
	PayloadJSON        string
	Status             string
	Attempts           int
	MaxAttempts        int
	LastError          string
	AvailableAt        time.Time
}

type Installation struct {
	InstallationID int64
	AccountLogin   string
	AccountType    string
}

type Repository struct {
	InstallationDBID int64
	Owner            string
	Name             string
	FullName         string
	DefaultBranch    string
}

type PullRequest struct {
	RepositoryID   int64
	Number         int
	Title          string
	AuthorLogin    string
	BaseSHA        string
	HeadSHA        string
	State          string
	ApprovalStatus string
}

type ReviewRun struct {
	PullRequestID int64
	TriggerType   string
	TriggerActor  string
	HeadSHA       string
	Status        string
}

type AuditLog struct {
	RepositoryID  int64
	PullRequestID int64
	Actor         string
	Action        string
	DetailJSON    string
}

type CommentCommand struct {
	PullRequestID int64
	Command       string
	Args          string
	Actor         string
	Status        string
	ResultMessage string
	ErrorMessage  string
	DeliveryID    string
}

type RiskSnapshot struct {
	ReviewRunID   int64
	PullRequestID int64
	Score         int
	Level         string
	Reasons       []string
	CreatedAt     time.Time
}

type ReviewRunSnapshot struct {
	ID            int64
	PullRequestID int64
	HeadSHA       string
	Status        string
	FinishedAt    time.Time
}

type FindingSnapshot struct {
	ID         int64
	FindingID  string
	FilePath   string
	LineNumber int
	Severity   string
	Category   string
	Title      string
	Reason     string
	Suggestion string
}

type ApprovalCheck struct {
	PullRequestID int64
	ReviewRunID   int64
	TriggeredBy   string
	Result        string
	Reasons       []string
	AutoApproved  bool
}

type HighRiskPR struct {
	RepositoryFullName string
	PRNumber           int
	Title              string
	Score              int
	Level              string
	CreatedAt          time.Time
}

type RepositorySummary struct {
	ID             int64
	InstallationID int64
	FullName       string
	OpenPRs        int
	HighRiskPRs    int
	LastActivity   time.Time
}

type RiskBucket struct {
	Level string
	Count int
}

type PRSummary struct {
	Number         int
	Title          string
	AuthorLogin    string
	State          string
	ApprovalStatus string
	HeadSHA        string
	RiskLevel      string
	RiskScore      int
	UpdatedAt      time.Time
}

type FindingReport struct {
	PRNumber  int
	FindingID string
	Severity  string
	Status    string
	FilePath  string
	Title     string
	CreatedAt time.Time
}

type ApprovalReport struct {
	PRNumber     int
	Result       string
	AutoApproved bool
	TriggeredBy  string
	CreatedAt    time.Time
}

type AuditReport struct {
	PRNumber   int
	Actor      string
	Action     string
	DetailJSON string
	CreatedAt  time.Time
}

type RepositoryReport struct {
	RepositoryFullName string
	RiskDistribution   []RiskBucket
	PullRequests       []PRSummary
	Findings           []FindingReport
	ApprovalChecks     []ApprovalReport
	AuditLogs          []AuditReport
}

type MetricsSnapshot struct {
	DeliveriesByStatus     map[string]int
	JobsByStatus           map[string]int
	ReviewRunsByStatus     map[string]int
	ApprovalChecksByResult map[string]int
	TotalRepositories      int
	TotalPullRequests      int
	TotalOpenFindings      int
}
