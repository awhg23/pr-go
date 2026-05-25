CREATE TABLE IF NOT EXISTS github_installations (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  installation_id BIGINT NOT NULL UNIQUE,
  account_login VARCHAR(255) NOT NULL DEFAULT '',
  account_type VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS repositories (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  installation_id BIGINT NOT NULL,
  owner VARCHAR(255) NOT NULL,
  name VARCHAR(255) NOT NULL,
  full_name VARCHAR(512) NOT NULL UNIQUE,
  default_branch VARCHAR(255) NOT NULL DEFAULT '',
  config_revision VARCHAR(255) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_repositories_installation_id (installation_id)
);

CREATE TABLE IF NOT EXISTS pull_requests (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  repository_id BIGINT NOT NULL,
  pr_number INT NOT NULL,
  title TEXT NOT NULL,
  author_login VARCHAR(255) NOT NULL DEFAULT '',
  base_sha VARCHAR(64) NOT NULL DEFAULT '',
  head_sha VARCHAR(64) NOT NULL DEFAULT '',
  state VARCHAR(32) NOT NULL DEFAULT 'open',
  approval_status VARCHAR(64) NOT NULL DEFAULT 'review_pending',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_pull_requests_repo_number (repository_id, pr_number),
  INDEX idx_pull_requests_repository_id (repository_id)
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  delivery_id VARCHAR(128) NOT NULL UNIQUE,
  event VARCHAR(128) NOT NULL DEFAULT '',
  action VARCHAR(128) NOT NULL DEFAULT '',
  repository_full_name VARCHAR(512) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL DEFAULT 'queued',
  error_message TEXT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_webhook_deliveries_status (status)
);

CREATE TABLE IF NOT EXISTS review_runs (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  pull_request_id BIGINT NOT NULL,
  trigger_type VARCHAR(128) NOT NULL,
  trigger_actor VARCHAR(255) NOT NULL DEFAULT '',
  head_sha VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL,
  started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at TIMESTAMP NULL,
  error_message TEXT NULL,
  INDEX idx_review_runs_pull_request_id (pull_request_id),
  INDEX idx_review_runs_status (status)
);

CREATE TABLE IF NOT EXISTS review_findings (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  review_run_id BIGINT NOT NULL,
  pull_request_id BIGINT NOT NULL,
  finding_id VARCHAR(64) NOT NULL,
  file_path TEXT NOT NULL,
  line_number INT NULL,
  severity VARCHAR(32) NOT NULL,
  category VARCHAR(128) NOT NULL,
  title TEXT NOT NULL,
  reason TEXT NOT NULL,
  suggestion TEXT NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'open',
  dismissed_by VARCHAR(255) NULL,
  dismissed_reason TEXT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_review_findings_run_id (review_run_id),
  INDEX idx_review_findings_pr_status (pull_request_id, status)
);

CREATE TABLE IF NOT EXISTS risk_scores (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  review_run_id BIGINT NOT NULL,
  pull_request_id BIGINT NOT NULL,
  score INT NOT NULL,
  level VARCHAR(32) NOT NULL,
  reasons_json TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_risk_scores_pr_created (pull_request_id, created_at),
  INDEX idx_risk_scores_level_created (level, created_at)
);

CREATE TABLE IF NOT EXISTS model_invocations (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  review_run_id BIGINT NOT NULL,
  provider VARCHAR(128) NOT NULL,
  model VARCHAR(255) NOT NULL,
  prompt_version VARCHAR(128) NOT NULL,
  input_tokens INT NOT NULL DEFAULT 0,
  output_tokens INT NOT NULL DEFAULT 0,
  status VARCHAR(32) NOT NULL,
  error_message TEXT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_model_invocations_run_id (review_run_id)
);

CREATE TABLE IF NOT EXISTS review_comments (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  review_run_id BIGINT NOT NULL,
  pull_request_id BIGINT NOT NULL,
  status VARCHAR(32) NOT NULL,
  error_message TEXT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_review_comments_run_id (review_run_id)
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  repository_id BIGINT NOT NULL,
  pull_request_id BIGINT NULL,
  actor VARCHAR(255) NOT NULL DEFAULT '',
  action VARCHAR(128) NOT NULL,
  detail_json TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_audit_logs_repo_created (repository_id, created_at),
  INDEX idx_audit_logs_pr_created (pull_request_id, created_at)
);
