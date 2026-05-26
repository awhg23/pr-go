# pr-go

`pr-go` is a production-oriented Go GitHub App for AI-assisted PR review and approval governance. It reviews pull requests, stores review/audit history in MySQL, supports maintainer comment commands, reads repository approval policy, can auto-approve only when explicitly enabled, and exposes an admin dashboard plus operational metrics.

## V0 Scope

- Accept a GitHub pull request URL.
- Fetch pull request metadata, changed files, and patches through the GitHub REST API.
- Compress large diffs before review.
- Call an OpenAI-compatible chat completions provider.
- Produce structured findings and a local review summary with risk score.

V0 intentionally does not include a GitHub App, MySQL persistence, comment writeback, or approval state.

## Usage

```bash
go run ./cmd/pr-go --pr-url https://github.com/owner/repo/pull/123
```

For private repositories or higher GitHub API limits:

```bash
GITHUB_TOKEN=github_pat_xxx go run ./cmd/pr-go --pr-url https://github.com/owner/repo/pull/123
```

For OpenAI-compatible providers:

```bash
OPENAI_API_KEY=sk-xxx \
OPENAI_BASE_URL=https://api.openai.com/v1 \
OPENAI_MODEL=gpt-4.1-mini \
go run ./cmd/pr-go --pr-url https://github.com/owner/repo/pull/123
```

Smoke verification without an LLM key can use the mock reviewer. It still fetches PR data from GitHub:

```bash
go run ./cmd/pr-go --provider mock --output json --pr-url https://github.com/owner/repo/pull/123
```

## Production Deployment

Use `DEPLOYMENT.md` for the full production checklist. The minimum server environment is:

```bash
GITHUB_APP_ID=123456
GITHUB_APP_PRIVATE_KEY_FILE=/run/secrets/github-app.pem
GITHUB_WEBHOOK_SECRET=change-me
MYSQL_DSN='user:pass@tcp(mysql:3306)/pr_go?parseTime=true'
OPENAI_API_KEY=sk-xxx
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_MODEL=gpt-4.1-mini
PR_GO_ADMIN_TOKENS='viewer:long-random-read-token:read;ops:long-random-metrics-token:metrics'
```

Run:

```bash
go run ./cmd/pr-go --server --addr :8080
```

Production endpoints:

```text
POST /webhook
GET  /healthz
GET  /readyz
GET  /metrics?token=<admin-token>
GET  /admin?token=<admin-token>
GET  /admin/repo?full_name=owner/repo&token=<admin-token>
GET  /api/v1/repositories
GET  /api/v1/repository?full_name=owner/repo
```

GitHub App permissions:

- Repository contents: read
- Pull requests: read/write
- Issues: read/write
- Checks: read
- Commit statuses: read
- Metadata: read

Webhook events:

- Pull request
- Issue comment
- Installation
- Installation repositories

## Development

```bash
go test ./...
```

Run the optional GitHub integration test against a real pull request:

```bash
PR_GO_INTEGRATION_PR_URL=https://github.com/owner/repo/pull/123 \
GITHUB_TOKEN=github_pat_xxx \
go test ./internal/github -run TestFetchPullRequestIntegration
```

The integration test is skipped unless `PR_GO_INTEGRATION_PR_URL` is set.

## V1 Preparation

This repository also contains an early GitHub App webhook foundation in `internal/app`:

- `VerifySignature` validates `X-Hub-Signature-256` with HMAC SHA-256.
- `ParseWebhook` extracts pull request and issue comment events.
- `WebhookEvent.ShouldTriggerReview` identifies PR events that should start review.
- `WebhookEvent.Command` extracts `/ai-*` commands from PR comments.

Review output carries stable schema metadata through `schema_version`, `prompt_version`, and `model_invocation` fields so V2 persistence can map model calls and review results into MySQL without depending on rendered comments.

## V1 GitHub App MVP

Run the GitHub App webhook server:

```bash
GITHUB_APP_ID=123456 \
GITHUB_APP_PRIVATE_KEY_FILE=/path/to/private-key.pem \
GITHUB_WEBHOOK_SECRET=webhook-secret \
MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/pr_go?parseTime=true' \
OPENAI_API_KEY=sk-xxx \
go run ./cmd/pr-go --server --addr :8080
```

For local smoke testing without an LLM key:

```bash
GITHUB_APP_ID=123456 \
GITHUB_APP_PRIVATE_KEY_FILE=/path/to/private-key.pem \
GITHUB_WEBHOOK_SECRET=webhook-secret \
MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/pr_go?parseTime=true' \
go run ./cmd/pr-go --server --provider mock
```

Webhook endpoint:

```text
POST /webhook
GET  /healthz
```

V1 listens to `pull_request.opened` and `pull_request.synchronize`, fetches PR metadata, changed files, diff patches, and check status with an installation token, then posts a PR comment containing risk level, key reasons, findings, and next steps.

V1 did not approve, merge, persist to MySQL, provide a web console, or support non-GitHub providers. V2 adds MySQL persistence while keeping approve/merge and web console out of scope.

## V2 MySQL Persistence

V2 requires MySQL for server mode. The server runs the built-in schema automatically on startup; the same schema is available as `migrations/001_init_mysql.sql`.

Stored data includes:

- GitHub installations and repositories.
- Pull requests and `approval_status`.
- Webhook deliveries for delivery-id deduplication.
- Review runs with success/failure status and error messages.
- Structured findings.
- Risk scores and reasons.
- Model invocation metadata.
- Review comment publish results.
- Audit logs for successful reviews and failure paths.

Webhook requests are acknowledged after signature validation, delivery deduplication, and enqueueing. Review execution happens in background workers controlled by `--worker-count`; failed jobs retry with exponential backoff up to `--max-retries`.

V5 persists webhook work in MySQL `webhook_jobs`; workers claim due jobs from MySQL and poll for queued/retrying jobs, so process restarts do not lose accepted webhook work.

Useful server flags:

```text
--mysql-dsn       MySQL DSN for V2 persistence
--worker-count    number of async review workers
--max-retries     maximum async review attempts
--queue-poll      persistent queue poll interval
--admin-token     admin dashboard/API bearer token
--admin-tokens    semicolon-separated admin tokens: name:token:scope1,scope2
--alert-webhook   optional alert webhook URL for final job failures
```

Query recent high-risk PRs:

```sql
SELECT repo.full_name, pr.pr_number, pr.title, rs.score, rs.level, rs.created_at
FROM risk_scores rs
JOIN pull_requests pr ON pr.id = rs.pull_request_id
JOIN repositories repo ON repo.id = pr.repository_id
WHERE repo.full_name = 'owner/repo'
  AND rs.level IN ('high', 'blocker')
ORDER BY rs.created_at DESC
LIMIT 20;
```

## V3 PR Comment Commands

V3 adds maintainer-only PR comment commands. The app checks the comment actor through GitHub collaborator permissions; only `write`, `maintain`, and `admin` can execute commands.

Supported commands:

- `/ai-review`: run a new review on the current PR.
- `/ai-recheck`: re-read the latest diff and CI/checks, then run a new review.
- `/ai-risk`: show the latest persisted risk score and reasons.
- `/ai-dismiss <finding-id> <reason>`: mark an open finding as dismissed with an audit reason.
- `/ai-approve-check`: re-read the latest PR head and CI/checks, then output `建议审批`, `需要人工重点审查`, or `暂不建议审批`.

`/ai-approve-check` uses conservative rules:

- The latest successful review run must match the current PR head sha.
- CI/checks must be `success`.
- Any open high/blocker finding yields `暂不建议审批`.
- Any open medium finding or medium risk level yields `需要人工重点审查`.
- Only when the current head is reviewed, CI/checks pass, and no open high/blocker finding remains does it output `建议审批`.

V3 still does not call GitHub approve or merge APIs.

## V4 Repository Policy

V4 reads `.pr-approval-agent.yml` from the PR base commit. This follows PR-Agent's repository-level configuration idea while keeping pr-go's stateful GitHub App and MySQL audit model.

When the file is missing, pr-go uses safe defaults:

- auto approve is disabled
- CI/checks must be `success`
- high/blocker findings block approval
- auth, permission, migration, payment, secret, security, and dependency files are treated as high-risk areas
- maintainer permission is required for comment commands

If the config file is invalid, pr-go comments a readable warning on the PR, writes an audit record, and continues with safe defaults.

Example:

```yaml
version: 1

review:
  language: zh-CN
  ignore_files:
    - "**/*.lock"
    - "dist/**"
    - "vendor/**"
  high_risk_paths:
    - "internal/auth/**"
    - "migrations/**"
    - "payment/**"

approval:
  auto_approve:
    enabled: false
  require_human_when:
    - "risk_level >= high"
    - "ci_status != success"
    - "changed_files > 30"
    - "path matches internal/auth/**"
  required_checks:
    - "test"
    - "lint"
  require_tests: false

tests:
  require_changed_tests: false
  test_file_patterns:
    - "**/*_test.go"
    - "tests/**"

model:
  provider: openai-compatible
  model: default
  temperature: 0.2
```

`/ai-approve-check` applies required checks, human-review rules, and changed-test requirements. It only calls GitHub's approve API when the result is `建议审批` and `approval.auto_approve.enabled` is explicitly `true`.

## V5 Operations

V5 is the final usable app shape:

- MySQL-backed persistent webhook jobs with retry and restart recovery.
- Processing jobs locked for more than 15 minutes can be reclaimed by workers after a crash.
- Multi-repository installation tracking.
- Admin dashboard for repositories, PR risk distribution, findings, approval checks, and audit logs.
- JSON report APIs for integrations.
- Prometheus-style metrics for deliveries, jobs, review runs, approval checks, repositories, PRs, and open findings.
- `/readyz` verifies MySQL connectivity.
- Optional final-failure alert webhook via `PR_GO_ALERT_WEBHOOK_URL`.
- Admin dashboard, APIs, and metrics require `PR_GO_ADMIN_TOKEN` or scoped `PR_GO_ADMIN_TOKENS`.
- Removed GitHub App repositories are marked inactive and remain visible for audit history.
- OpenAI-compatible providers include `openai`, `openai-compatible`, `deepseek`, `siliconflow`, `ollama`, and `mock`.
