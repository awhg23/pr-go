# pr-go

`pr-go` is a Go prototype for a PR approval agent. V0 focuses on validating the review pipeline from a GitHub pull request URL to structured review findings and a local risk summary.

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
