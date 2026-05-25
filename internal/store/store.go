package store

import (
	"context"
	"time"

	"github.com/awhg23/pr-go/internal/review"
)

type Store interface {
	EnsureSchema(context.Context) error
	RecordDelivery(context.Context, Delivery) (bool, error)
	MarkDeliveryStatus(context.Context, string, string, string) error
	UpsertInstallation(context.Context, Installation) (int64, error)
	UpsertRepository(context.Context, Repository) (int64, error)
	UpsertPullRequest(context.Context, PullRequest) (int64, error)
	UpdatePullRequestApprovalStatus(context.Context, int64, string) error
	CreateReviewRun(context.Context, ReviewRun) (int64, error)
	FinishReviewRun(context.Context, int64, string, string) error
	SaveFindings(context.Context, int64, int64, []review.Finding) error
	SaveRiskScore(context.Context, int64, int64, review.Risk) error
	SaveModelInvocation(context.Context, int64, *review.ModelInvocation) error
	SaveReviewComment(context.Context, int64, int64, string, string) error
	Audit(context.Context, AuditLog) error
	RecentHighRiskPRs(context.Context, string, int) ([]HighRiskPR, error)
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

type HighRiskPR struct {
	RepositoryFullName string
	PRNumber           int
	Title              string
	Score              int
	Level              string
	CreatedAt          time.Time
}
