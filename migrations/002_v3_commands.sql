CREATE TABLE IF NOT EXISTS comment_commands (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  pull_request_id BIGINT NOT NULL,
  command VARCHAR(64) NOT NULL,
  args TEXT NOT NULL,
  actor VARCHAR(255) NOT NULL,
  status VARCHAR(32) NOT NULL,
  result_message TEXT NULL,
  error_message TEXT NULL,
  delivery_id VARCHAR(128) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_comment_commands_pr_created (pull_request_id, created_at),
  INDEX idx_comment_commands_delivery_id (delivery_id)
);

CREATE TABLE IF NOT EXISTS approval_checks (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  pull_request_id BIGINT NOT NULL,
  review_run_id BIGINT NULL,
  triggered_by VARCHAR(255) NOT NULL,
  result VARCHAR(64) NOT NULL,
  reasons_json TEXT NOT NULL,
  auto_approved BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_approval_checks_pr_created (pull_request_id, created_at)
);
