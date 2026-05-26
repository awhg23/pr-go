# pr-go Deployment Guide

This guide is the V5 production checklist for running pr-go as a GitHub App.

## 1. MySQL

Create a database and user:

```sql
CREATE DATABASE pr_go CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'pr_go'@'%' IDENTIFIED BY 'change-me';
GRANT ALL PRIVILEGES ON pr_go.* TO 'pr_go'@'%';
FLUSH PRIVILEGES;
```

The app runs built-in schema creation on startup. SQL migration files are also available in `migrations/`.

For an existing V5 database, apply the launch-readiness migration before rollout:

```bash
mysql -h <host> -P <port> -u pr_go -p pr_go < migrations/004_launch_readiness.sql
```

## 2. GitHub App

Create a GitHub App with:

- Webhook URL: `https://your-domain.example/webhook`
- Webhook secret: strong random value
- Repository permissions:
  - Contents: read
  - Pull requests: read/write
  - Issues: read/write
  - Checks: read
  - Commit statuses: read
  - Metadata: read
- Subscribe to events:
  - Pull request
  - Issue comment
  - Installation
  - Installation repositories

Download the private key PEM and install the App into the target repositories.

## 3. Runtime Environment

```bash
export GITHUB_APP_ID=123456
export GITHUB_APP_PRIVATE_KEY_FILE=/run/secrets/github-app.pem
export GITHUB_WEBHOOK_SECRET='replace-with-webhook-secret'
export MYSQL_DSN='pr_go:change-me@tcp(mysql:3306)/pr_go?parseTime=true'
export OPENAI_API_KEY='sk-...'
export OPENAI_BASE_URL='https://api.openai.com/v1'
export OPENAI_MODEL='gpt-4.1-mini'
export PR_GO_ADMIN_TOKENS='viewer:long-random-read-token:read;ops:long-random-metrics-token:metrics'
export PR_GO_PROVIDER='openai-compatible'
```

Optional:

```bash
export PR_GO_WORKER_COUNT=4
export PR_GO_MAX_RETRIES=5
export PR_GO_QUEUE_POLL=2s
export PR_GO_ALERT_WEBHOOK_URL='https://alerts.example/webhook'
export PR_GO_ADMIN_TOKEN='single-legacy-admin-token'
```

## 4. Run

```bash
go run ./cmd/pr-go --server --addr :8080
```

Docker:

```bash
docker build -t pr-go:latest .
docker run --rm -p 8080:8080 --env-file .env pr-go:latest
```

Docker Compose:

```bash
cp .env.example .env
mkdir -p secrets
# put the GitHub App private key at secrets/github-app.pem
docker compose up --build
```

## 5. Verify

```bash
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
curl -fsS 'http://127.0.0.1:8080/metrics?token=long-random-metrics-token'
```

Open:

```text
https://your-domain.example/admin?token=long-random-read-token
```

## 6. Repository Policy

Add `.pr-approval-agent.yml` to each repository when custom rules are needed. Auto approve remains disabled unless `approval.auto_approve.enabled: true` is present and `/ai-approve-check` passes all policy checks.

## 7. Operations

- Accepted webhook work is stored in `webhook_jobs`.
- Jobs left in `processing` for more than 15 minutes can be reclaimed after worker or process crashes.
- Duplicate webhook deliveries are deduplicated by `X-GitHub-Delivery`.
- Workers retry failed jobs with exponential backoff.
- Final job failures can be sent to `PR_GO_ALERT_WEBHOOK_URL`.
- Admin pages and APIs require `PR_GO_ADMIN_TOKEN` or scoped `PR_GO_ADMIN_TOKENS`.
- `PR_GO_ADMIN_TOKENS` entries use `name:token:scope1,scope2`; supported scopes are `read`, `metrics`, and `admin`.
- Repositories removed from the GitHub App installation are marked inactive and kept in reports for audit history.
