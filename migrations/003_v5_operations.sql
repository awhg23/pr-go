CREATE TABLE IF NOT EXISTS webhook_jobs (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  delivery_id VARCHAR(128) NOT NULL UNIQUE,
  event VARCHAR(128) NOT NULL DEFAULT '',
  action VARCHAR(128) NOT NULL DEFAULT '',
  repository_full_name VARCHAR(512) NOT NULL DEFAULT '',
  payload_json MEDIUMTEXT NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'queued',
  attempts INT NOT NULL DEFAULT 0,
  max_attempts INT NOT NULL DEFAULT 3,
  available_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  locked_at TIMESTAMP NULL,
  locked_by VARCHAR(128) NOT NULL DEFAULT '',
  last_error TEXT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_webhook_jobs_status_available (status, available_at),
  INDEX idx_webhook_jobs_delivery_id (delivery_id)
);
