package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"github.com/awhg23/pr-go/internal/review"
)

type MySQLStore struct {
	db *sql.DB
}

func NewMySQLStore(dsn string) (*MySQLStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("mysql dsn is required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	return &MySQLStore{db: db}, nil
}

func (s *MySQLStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	for _, stmt := range SchemaStatements() {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *MySQLStore) RecordDelivery(ctx context.Context, d Delivery) (bool, error) {
	if d.DeliveryID == "" {
		return false, fmt.Errorf("delivery id is required")
	}
	if d.Status == "" {
		d.Status = "queued"
	}
	res, err := s.db.ExecContext(ctx, `
INSERT IGNORE INTO webhook_deliveries
  (delivery_id, event, action, repository_full_name, status, error_message)
VALUES (?, ?, ?, ?, ?, ?)`,
		d.DeliveryID, d.Event, d.Action, d.RepositoryFullName, d.Status, d.ErrorMessage)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (s *MySQLStore) MarkDeliveryStatus(ctx context.Context, deliveryID, status, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE webhook_deliveries
SET status = ?, error_message = ?, updated_at = CURRENT_TIMESTAMP
WHERE delivery_id = ?`, status, errorMessage, deliveryID)
	return err
}

func (s *MySQLStore) UpsertInstallation(ctx context.Context, in Installation) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO github_installations (installation_id, account_login, account_type)
VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE
  id = LAST_INSERT_ID(id),
  account_login = VALUES(account_login),
  account_type = VALUES(account_type),
  updated_at = CURRENT_TIMESTAMP`, in.InstallationID, in.AccountLogin, in.AccountType)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *MySQLStore) UpsertRepository(ctx context.Context, repo Repository) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO repositories (installation_id, owner, name, full_name, default_branch)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  id = LAST_INSERT_ID(id),
  installation_id = VALUES(installation_id),
  owner = VALUES(owner),
  name = VALUES(name),
  default_branch = VALUES(default_branch),
  updated_at = CURRENT_TIMESTAMP`,
		repo.InstallationDBID, repo.Owner, repo.Name, repo.FullName, repo.DefaultBranch)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *MySQLStore) UpsertPullRequest(ctx context.Context, pr PullRequest) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO pull_requests
  (repository_id, pr_number, title, author_login, base_sha, head_sha, state, approval_status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  id = LAST_INSERT_ID(id),
  title = VALUES(title),
  author_login = VALUES(author_login),
  base_sha = VALUES(base_sha),
  head_sha = VALUES(head_sha),
  state = VALUES(state),
  approval_status = VALUES(approval_status),
  updated_at = CURRENT_TIMESTAMP`,
		pr.RepositoryID, pr.Number, pr.Title, pr.AuthorLogin, pr.BaseSHA, pr.HeadSHA, pr.State, pr.ApprovalStatus)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *MySQLStore) UpdatePullRequestApprovalStatus(ctx context.Context, prID int64, status string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE pull_requests
SET approval_status = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?`, status, prID)
	return err
}

func (s *MySQLStore) CreateReviewRun(ctx context.Context, run ReviewRun) (int64, error) {
	if run.Status == "" {
		run.Status = "running"
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO review_runs
  (pull_request_id, trigger_type, trigger_actor, head_sha, status, started_at)
VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		run.PullRequestID, run.TriggerType, run.TriggerActor, run.HeadSHA, run.Status)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *MySQLStore) FinishReviewRun(ctx context.Context, runID int64, status, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE review_runs
SET status = ?, error_message = ?, finished_at = CURRENT_TIMESTAMP
WHERE id = ?`, status, errorMessage, runID)
	return err
}

func (s *MySQLStore) SaveFindings(ctx context.Context, runID, prID int64, findings []review.Finding) error {
	for _, finding := range findings {
		_, err := s.db.ExecContext(ctx, `
INSERT INTO review_findings
  (review_run_id, pull_request_id, finding_id, file_path, line_number, severity, category, title, reason, suggestion, status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'open')`,
			runID, prID, finding.ID, finding.FilePath, nullableLine(finding.LineNumber),
			finding.Severity, finding.Category, finding.Title, finding.Reason, finding.Suggestion)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *MySQLStore) SaveRiskScore(ctx context.Context, runID, prID int64, risk review.Risk) error {
	reasons, err := json.Marshal(risk.Reasons)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO risk_scores (review_run_id, pull_request_id, score, level, reasons_json)
VALUES (?, ?, ?, ?, ?)`, runID, prID, risk.Score, risk.Level, string(reasons))
	return err
}

func (s *MySQLStore) SaveModelInvocation(ctx context.Context, runID int64, inv *review.ModelInvocation) error {
	if inv == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO model_invocations
  (review_run_id, provider, model, prompt_version, input_tokens, output_tokens, status, error_message)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, inv.Provider, inv.Model, inv.PromptVersion, inv.InputTokens, inv.OutputTokens, inv.Status, inv.ErrorMessage)
	return err
}

func (s *MySQLStore) SaveReviewComment(ctx context.Context, runID, prID int64, status, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO review_comments (review_run_id, pull_request_id, status, error_message)
VALUES (?, ?, ?, ?)`, runID, prID, status, errorMessage)
	return err
}

func (s *MySQLStore) Audit(ctx context.Context, log AuditLog) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO audit_logs (repository_id, pull_request_id, actor, action, detail_json)
VALUES (?, ?, ?, ?, ?)`, log.RepositoryID, nullableID(log.PullRequestID), log.Actor, log.Action, log.DetailJSON)
	return err
}

func (s *MySQLStore) RecentHighRiskPRs(ctx context.Context, repoFullName string, limit int) ([]HighRiskPR, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT repo.full_name, pr.pr_number, pr.title, rs.score, rs.level, rs.created_at
FROM risk_scores rs
JOIN pull_requests pr ON pr.id = rs.pull_request_id
JOIN repositories repo ON repo.id = pr.repository_id
WHERE repo.full_name = ? AND rs.level IN ('high', 'blocker')
ORDER BY rs.created_at DESC
LIMIT ?`, repoFullName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HighRiskPR
	for rows.Next() {
		var item HighRiskPR
		if err := rows.Scan(&item.RepositoryFullName, &item.PRNumber, &item.Title, &item.Score, &item.Level, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func nullableLine(line int) any {
	if line <= 0 {
		return nil
	}
	return line
}

func nullableID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}
