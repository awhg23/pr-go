ALTER TABLE repositories
  ADD COLUMN active BOOLEAN NOT NULL DEFAULT TRUE,
  ADD COLUMN removed_at TIMESTAMP NULL;

CREATE INDEX idx_repositories_active ON repositories (active);
