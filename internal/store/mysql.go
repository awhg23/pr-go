package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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

func (s *MySQLStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
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

func (s *MySQLStore) EnqueueWebhookJob(ctx context.Context, job WebhookJob) (int64, error) {
	if strings.TrimSpace(job.DeliveryID) == "" {
		return 0, fmt.Errorf("delivery id is required")
	}
	if job.Status == "" {
		job.Status = "queued"
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 3
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO webhook_jobs
  (delivery_id, event, action, repository_full_name, payload_json, status, attempts, max_attempts, available_at, last_error)
VALUES (?, ?, ?, ?, ?, ?, 0, ?, COALESCE(?, CURRENT_TIMESTAMP), ?)
ON DUPLICATE KEY UPDATE
  id = LAST_INSERT_ID(id),
  updated_at = CURRENT_TIMESTAMP`,
		job.DeliveryID, job.Event, job.Action, job.RepositoryFullName, job.PayloadJSON, job.Status,
		job.MaxAttempts, nullableTime(job.AvailableAt), job.LastError)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *MySQLStore) ClaimWebhookJob(ctx context.Context, id int64, workerID string) (WebhookJob, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WebhookJob{}, false, err
	}
	defer tx.Rollback()

	query := `
SELECT id, delivery_id, event, action, repository_full_name, payload_json, status, attempts, max_attempts, COALESCE(last_error, ''), available_at
FROM webhook_jobs
WHERE (
  (status IN ('queued', 'retrying') AND available_at <= CURRENT_TIMESTAMP)
  OR (status = 'processing' AND locked_at < TIMESTAMPADD(MINUTE, -15, CURRENT_TIMESTAMP))
)
ORDER BY
  CASE WHEN status = 'processing' THEN 0 ELSE 1 END,
  available_at ASC,
  id ASC
LIMIT 1
FOR UPDATE`
	args := []any{}
	if id > 0 {
		query = `
SELECT id, delivery_id, event, action, repository_full_name, payload_json, status, attempts, max_attempts, COALESCE(last_error, ''), available_at
FROM webhook_jobs
WHERE id = ? AND (
  (status IN ('queued', 'retrying') AND available_at <= CURRENT_TIMESTAMP)
  OR (status = 'processing' AND locked_at < TIMESTAMPADD(MINUTE, -15, CURRENT_TIMESTAMP))
)
FOR UPDATE`
		args = append(args, id)
	}

	var job WebhookJob
	err = tx.QueryRowContext(ctx, query, args...).Scan(&job.ID, &job.DeliveryID, &job.Event, &job.Action, &job.RepositoryFullName, &job.PayloadJSON, &job.Status, &job.Attempts, &job.MaxAttempts, &job.LastError, &job.AvailableAt)
	if err == sql.ErrNoRows {
		return WebhookJob{}, false, nil
	}
	if err != nil {
		return WebhookJob{}, false, err
	}
	_, err = tx.ExecContext(ctx, `
UPDATE webhook_jobs
SET status = 'processing', attempts = attempts + 1, locked_at = CURRENT_TIMESTAMP, locked_by = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?`, workerID, job.ID)
	if err != nil {
		return WebhookJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return WebhookJob{}, false, err
	}
	job.Attempts++
	job.Status = "processing"
	return job, true, nil
}

func (s *MySQLStore) CompleteWebhookJob(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE webhook_jobs
SET status = 'processed', last_error = NULL, locked_at = NULL, locked_by = '', updated_at = CURRENT_TIMESTAMP
WHERE id = ?`, id)
	return err
}

func (s *MySQLStore) RetryWebhookJob(ctx context.Context, id int64, errorMessage string, availableAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE webhook_jobs
SET status = 'retrying', last_error = ?, available_at = ?, locked_at = NULL, locked_by = '', updated_at = CURRENT_TIMESTAMP
WHERE id = ?`, errorMessage, availableAt, id)
	return err
}

func (s *MySQLStore) FailWebhookJob(ctx context.Context, id int64, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE webhook_jobs
SET status = 'failed', last_error = ?, locked_at = NULL, locked_by = '', updated_at = CURRENT_TIMESTAMP
WHERE id = ?`, errorMessage, id)
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

func (s *MySQLStore) EnsurePullRequest(ctx context.Context, repositoryID int64, number int) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO pull_requests
  (repository_id, pr_number, title, state, approval_status)
VALUES (?, ?, '', 'open', 'review_pending')
ON DUPLICATE KEY UPDATE
  id = LAST_INSERT_ID(id),
  updated_at = CURRENT_TIMESTAMP`,
		repositoryID, number)
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

func (s *MySQLStore) UpdateReviewRunHeadSHA(ctx context.Context, runID int64, headSHA string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE review_runs
SET head_sha = ?
WHERE id = ?`, headSHA, runID)
	return err
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

func (s *MySQLStore) CreateCommentCommand(ctx context.Context, command CommentCommand) (int64, error) {
	if command.Status == "" {
		command.Status = "running"
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO comment_commands
  (pull_request_id, command, args, actor, status, result_message, error_message, delivery_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		command.PullRequestID, command.Command, command.Args, command.Actor, command.Status,
		command.ResultMessage, command.ErrorMessage, command.DeliveryID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *MySQLStore) FinishCommentCommand(ctx context.Context, id int64, status, resultMessage, errorMessage string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE comment_commands
SET status = ?, result_message = ?, error_message = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?`, status, resultMessage, errorMessage, id)
	return err
}

func (s *MySQLStore) LatestRiskScore(ctx context.Context, prID int64) (RiskSnapshot, bool, error) {
	var row RiskSnapshot
	var reasonsJSON string
	err := s.db.QueryRowContext(ctx, `
SELECT review_run_id, pull_request_id, score, level, reasons_json, created_at
FROM risk_scores
WHERE pull_request_id = ?
ORDER BY created_at DESC, id DESC
LIMIT 1`, prID).Scan(&row.ReviewRunID, &row.PullRequestID, &row.Score, &row.Level, &reasonsJSON, &row.CreatedAt)
	if err == sql.ErrNoRows {
		return RiskSnapshot{}, false, nil
	}
	if err != nil {
		return RiskSnapshot{}, false, err
	}
	if reasonsJSON != "" {
		_ = json.Unmarshal([]byte(reasonsJSON), &row.Reasons)
	}
	return row, true, nil
}

func (s *MySQLStore) LatestSuccessfulReviewRun(ctx context.Context, prID int64) (ReviewRunSnapshot, bool, error) {
	var row ReviewRunSnapshot
	var finished sql.NullTime
	err := s.db.QueryRowContext(ctx, `
SELECT id, pull_request_id, head_sha, status, finished_at
FROM review_runs
WHERE pull_request_id = ? AND status = 'success'
ORDER BY COALESCE(finished_at, started_at) DESC, id DESC
LIMIT 1`, prID).Scan(&row.ID, &row.PullRequestID, &row.HeadSHA, &row.Status, &finished)
	if err == sql.ErrNoRows {
		return ReviewRunSnapshot{}, false, nil
	}
	if err != nil {
		return ReviewRunSnapshot{}, false, err
	}
	if finished.Valid {
		row.FinishedAt = finished.Time
	}
	return row, true, nil
}

func (s *MySQLStore) ListOpenFindings(ctx context.Context, prID int64) ([]FindingSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, finding_id, file_path, COALESCE(line_number, 0), severity, category, title, reason, suggestion
FROM review_findings
WHERE pull_request_id = ? AND status = 'open'
ORDER BY created_at DESC, id DESC`, prID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FindingSnapshot
	for rows.Next() {
		var item FindingSnapshot
		if err := rows.Scan(&item.ID, &item.FindingID, &item.FilePath, &item.LineNumber, &item.Severity, &item.Category, &item.Title, &item.Reason, &item.Suggestion); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *MySQLStore) DismissFinding(ctx context.Context, prID int64, findingID, actor, reason string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE review_findings
SET status = 'dismissed', dismissed_by = ?, dismissed_reason = ?
WHERE pull_request_id = ? AND finding_id = ? AND status = 'open'`, actor, reason, prID, findingID)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (s *MySQLStore) SaveApprovalCheck(ctx context.Context, check ApprovalCheck) error {
	reasons, err := json.Marshal(check.Reasons)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO approval_checks
  (pull_request_id, review_run_id, triggered_by, result, reasons_json, auto_approved)
VALUES (?, ?, ?, ?, ?, ?)`,
		check.PullRequestID, nullableID(check.ReviewRunID), check.TriggeredBy, check.Result, string(reasons), check.AutoApproved)
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

func (s *MySQLStore) ListRepositorySummaries(ctx context.Context, limit int) ([]RepositorySummary, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT repo.id, gi.installation_id, repo.full_name,
  (SELECT COUNT(*) FROM pull_requests pr WHERE pr.repository_id = repo.id AND pr.state = 'open') AS open_prs,
  (SELECT COUNT(DISTINCT pr2.id)
   FROM pull_requests pr2
   JOIN risk_scores rs2 ON rs2.pull_request_id = pr2.id
   WHERE pr2.repository_id = repo.id AND rs2.level IN ('high', 'blocker')) AS high_risk_prs,
  COALESCE(MAX(pr.updated_at), repo.updated_at) AS last_activity
FROM repositories repo
JOIN github_installations gi ON gi.id = repo.installation_id
LEFT JOIN pull_requests pr ON pr.repository_id = repo.id
GROUP BY repo.id, gi.installation_id, repo.full_name, repo.updated_at
ORDER BY last_activity DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RepositorySummary
	for rows.Next() {
		var item RepositorySummary
		if err := rows.Scan(&item.ID, &item.InstallationID, &item.FullName, &item.OpenPRs, &item.HighRiskPRs, &item.LastActivity); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *MySQLStore) RepositoryReport(ctx context.Context, fullName string, limit int) (RepositoryReport, error) {
	if strings.TrimSpace(fullName) == "" {
		return RepositoryReport{}, fmt.Errorf("repository full name is required")
	}
	if limit <= 0 {
		limit = 50
	}
	var repoID int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM repositories WHERE full_name = ?`, fullName).Scan(&repoID); err != nil {
		return RepositoryReport{}, err
	}
	report := RepositoryReport{RepositoryFullName: fullName}

	buckets, err := s.queryRiskBuckets(ctx, repoID)
	if err != nil {
		return RepositoryReport{}, err
	}
	report.RiskDistribution = buckets
	prs, err := s.queryPRSummaries(ctx, repoID, limit)
	if err != nil {
		return RepositoryReport{}, err
	}
	report.PullRequests = prs
	findings, err := s.queryFindingReports(ctx, repoID, limit)
	if err != nil {
		return RepositoryReport{}, err
	}
	report.Findings = findings
	approvals, err := s.queryApprovalReports(ctx, repoID, limit)
	if err != nil {
		return RepositoryReport{}, err
	}
	report.ApprovalChecks = approvals
	audits, err := s.queryAuditReports(ctx, repoID, limit)
	if err != nil {
		return RepositoryReport{}, err
	}
	report.AuditLogs = audits
	return report, nil
}

func (s *MySQLStore) Metrics(ctx context.Context) (MetricsSnapshot, error) {
	out := MetricsSnapshot{
		DeliveriesByStatus:     map[string]int{},
		JobsByStatus:           map[string]int{},
		ReviewRunsByStatus:     map[string]int{},
		ApprovalChecksByResult: map[string]int{},
	}
	var err error
	if out.DeliveriesByStatus, err = s.countBy(ctx, "webhook_deliveries", "status"); err != nil {
		return MetricsSnapshot{}, err
	}
	if out.JobsByStatus, err = s.countBy(ctx, "webhook_jobs", "status"); err != nil {
		return MetricsSnapshot{}, err
	}
	if out.ReviewRunsByStatus, err = s.countBy(ctx, "review_runs", "status"); err != nil {
		return MetricsSnapshot{}, err
	}
	if out.ApprovalChecksByResult, err = s.countBy(ctx, "approval_checks", "result"); err != nil {
		return MetricsSnapshot{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM repositories`).Scan(&out.TotalRepositories); err != nil {
		return MetricsSnapshot{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pull_requests`).Scan(&out.TotalPullRequests); err != nil {
		return MetricsSnapshot{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_findings WHERE status = 'open'`).Scan(&out.TotalOpenFindings); err != nil {
		return MetricsSnapshot{}, err
	}
	return out, nil
}

func (s *MySQLStore) queryRiskBuckets(ctx context.Context, repoID int64) ([]RiskBucket, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT latest.level, COUNT(*)
FROM (
  SELECT pr.id AS pull_request_id, rs.level
  FROM pull_requests pr
  JOIN risk_scores rs ON rs.pull_request_id = pr.id
  JOIN (
    SELECT pull_request_id, MAX(id) AS latest_id
    FROM risk_scores
    GROUP BY pull_request_id
  ) latest_rs ON latest_rs.latest_id = rs.id
  WHERE pr.repository_id = ?
) latest
GROUP BY latest.level`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RiskBucket
	for rows.Next() {
		var item RiskBucket
		if err := rows.Scan(&item.Level, &item.Count); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *MySQLStore) queryPRSummaries(ctx context.Context, repoID int64, limit int) ([]PRSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT pr.pr_number, pr.title, pr.author_login, pr.state, pr.approval_status, pr.head_sha,
       COALESCE(rs.level, ''), COALESCE(rs.score, 0), pr.updated_at
FROM pull_requests pr
LEFT JOIN (
  SELECT rs1.*
  FROM risk_scores rs1
  JOIN (
    SELECT pull_request_id, MAX(id) AS latest_id
    FROM risk_scores
    GROUP BY pull_request_id
  ) latest_rs ON latest_rs.latest_id = rs1.id
) rs ON rs.pull_request_id = pr.id
WHERE pr.repository_id = ?
ORDER BY pr.updated_at DESC
LIMIT ?`, repoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PRSummary
	for rows.Next() {
		var item PRSummary
		if err := rows.Scan(&item.Number, &item.Title, &item.AuthorLogin, &item.State, &item.ApprovalStatus, &item.HeadSHA, &item.RiskLevel, &item.RiskScore, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *MySQLStore) queryFindingReports(ctx context.Context, repoID int64, limit int) ([]FindingReport, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT pr.pr_number, rf.finding_id, rf.severity, rf.status, rf.file_path, rf.title, rf.created_at
FROM review_findings rf
JOIN pull_requests pr ON pr.id = rf.pull_request_id
WHERE pr.repository_id = ?
ORDER BY rf.created_at DESC
LIMIT ?`, repoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FindingReport
	for rows.Next() {
		var item FindingReport
		if err := rows.Scan(&item.PRNumber, &item.FindingID, &item.Severity, &item.Status, &item.FilePath, &item.Title, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *MySQLStore) queryApprovalReports(ctx context.Context, repoID int64, limit int) ([]ApprovalReport, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT pr.pr_number, ac.result, ac.auto_approved, ac.triggered_by, ac.created_at
FROM approval_checks ac
JOIN pull_requests pr ON pr.id = ac.pull_request_id
WHERE pr.repository_id = ?
ORDER BY ac.created_at DESC
LIMIT ?`, repoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApprovalReport
	for rows.Next() {
		var item ApprovalReport
		if err := rows.Scan(&item.PRNumber, &item.Result, &item.AutoApproved, &item.TriggeredBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *MySQLStore) queryAuditReports(ctx context.Context, repoID int64, limit int) ([]AuditReport, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(pr.pr_number, 0), al.actor, al.action, al.detail_json, al.created_at
FROM audit_logs al
LEFT JOIN pull_requests pr ON pr.id = al.pull_request_id
WHERE al.repository_id = ?
ORDER BY al.created_at DESC
LIMIT ?`, repoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditReport
	for rows.Next() {
		var item AuditReport
		if err := rows.Scan(&item.PRNumber, &item.Actor, &item.Action, &item.DetailJSON, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *MySQLStore) countBy(ctx context.Context, table string, column string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT %s, COUNT(*) FROM %s GROUP BY %s`, column, table, column))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		out[key] = count
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

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
