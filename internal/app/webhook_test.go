package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	header := signature("secret", body)
	if err := VerifySignature("secret", body, header); err != nil {
		t.Fatalf("VerifySignature returned error: %v", err)
	}
}

func TestVerifySignatureRejectsInvalidSignature(t *testing.T) {
	if err := VerifySignature("secret", []byte(`{}`), "sha256=deadbeef"); err == nil {
		t.Fatal("expected invalid signature error")
	}
}

func TestParseWebhookPullRequestTrigger(t *testing.T) {
	body := []byte(`{"action":"opened","repository":{"full_name":"owner/repo"},"pull_request":{"number":12,"html_url":"https://github.com/owner/repo/pull/12","head":{"sha":"abc"}}}`)
	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", signature("secret", body))
	headers.Set("X-GitHub-Event", EventPullRequest)
	headers.Set("X-GitHub-Delivery", "delivery-1")

	event, err := ParseWebhook(headers, body, "secret")
	if err != nil {
		t.Fatalf("ParseWebhook returned error: %v", err)
	}
	if !event.ShouldTriggerReview() {
		t.Fatal("expected pull_request.opened to trigger review")
	}
	if event.PullRequest.Number != 12 || event.Repository.FullName != "owner/repo" {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestPullRequestSynchronizeTrigger(t *testing.T) {
	event := WebhookEvent{Event: EventPullRequest, Action: "synchronize", PullRequest: &PullRequest{}}
	if !event.ShouldTriggerReview() {
		t.Fatal("expected pull_request.synchronize to trigger review")
	}
	event.Action = "reopened"
	if event.ShouldTriggerReview() {
		t.Fatal("did not expect pull_request.reopened in V1 trigger set")
	}
}

func TestRepositoryOwnerRepoFromFullName(t *testing.T) {
	event := WebhookEvent{Repository: Repository{FullName: "owner/repo"}}
	owner, repo := event.RepositoryOwnerRepo()
	if owner != "owner" || repo != "repo" {
		t.Fatalf("owner/repo = %s/%s, want owner/repo", owner, repo)
	}
}

func TestParseWebhookCommand(t *testing.T) {
	body := []byte(`{"action":"created","issue":{"number":12,"pull_request":{}},"comment":{"body":"/ai-review now","user":{"login":"maintainer"}}}`)
	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", signature("secret", body))
	headers.Set("X-GitHub-Event", EventIssueComment)

	event, err := ParseWebhook(headers, body, "secret")
	if err != nil {
		t.Fatalf("ParseWebhook returned error: %v", err)
	}
	if got := event.Command(); got != "/ai-review" {
		t.Fatalf("Command() = %q, want /ai-review", got)
	}
}

func signature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
