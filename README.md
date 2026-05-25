# pr-go

`pr-go` is a Go prototype for a PR approval agent. V0 validates the local CLI review pipeline, and V1 adds a GitHub App webhook MVP that can review pull requests and publish risk comments.

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
OPENAI_API_KEY=sk-xxx \
go run ./cmd/pr-go --server --addr :8080
```

For local smoke testing without an LLM key:

```bash
GITHUB_APP_ID=123456 \
GITHUB_APP_PRIVATE_KEY_FILE=/path/to/private-key.pem \
GITHUB_WEBHOOK_SECRET=webhook-secret \
go run ./cmd/pr-go --server --provider mock
```

Webhook endpoint:

```text
POST /webhook
GET  /healthz
```

V1 listens to `pull_request.opened` and `pull_request.synchronize`, fetches PR metadata, changed files, diff patches, and check status with an installation token, then posts a PR comment containing risk level, key reasons, findings, and next steps.

V1 does not approve, merge, persist to MySQL, provide a web console, or support non-GitHub providers.
