package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	EventPullRequest              = "pull_request"
	EventIssueComment             = "issue_comment"
	EventInstallation             = "installation"
	EventInstallationRepositories = "installation_repositories"
)

type WebhookEvent struct {
	Event               string
	Action              string
	DeliveryID          string
	Repository          Repository
	Installation        Installation
	PullRequest         *PullRequest
	Issue               *Issue
	Comment             *Comment
	Repositories        []Repository
	RepositoriesAdded   []Repository
	RepositoriesRemoved []Repository
	Sender              Sender
	RawPayload          []byte
}

type Repository struct {
	FullName string `json:"full_name"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
	Name string `json:"name"`
}

type Installation struct {
	ID int64 `json:"id"`
}

type PullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Head    struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

type Issue struct {
	Number      int  `json:"number"`
	PullRequest *any `json:"pull_request"`
}

type Comment struct {
	Body string `json:"body"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
}

type Sender struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

func VerifySignature(secret string, body []byte, signatureHeader string) error {
	if secret == "" {
		return errors.New("webhook secret is required")
	}
	if signatureHeader == "" {
		return errors.New("missing X-Hub-Signature-256 header")
	}
	const prefix = "sha256="
	if !strings.HasPrefix(signatureHeader, prefix) {
		return errors.New("unsupported signature header")
	}

	want, err := hex.DecodeString(strings.TrimPrefix(signatureHeader, prefix))
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	got := mac.Sum(nil)
	if !hmac.Equal(got, want) {
		return errors.New("invalid webhook signature")
	}
	return nil
}

func ParseWebhook(headers http.Header, body []byte, secret string) (WebhookEvent, error) {
	if err := VerifySignature(secret, body, headers.Get("X-Hub-Signature-256")); err != nil {
		return WebhookEvent{}, err
	}

	var payload struct {
		Action              string       `json:"action"`
		Repository          Repository   `json:"repository"`
		Installation        Installation `json:"installation"`
		PullRequest         *PullRequest `json:"pull_request"`
		Issue               *Issue       `json:"issue"`
		Comment             *Comment     `json:"comment"`
		Repositories        []Repository `json:"repositories"`
		RepositoriesAdded   []Repository `json:"repositories_added"`
		RepositoriesRemoved []Repository `json:"repositories_removed"`
		Sender              Sender       `json:"sender"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return WebhookEvent{}, fmt.Errorf("parse webhook payload: %w", err)
	}

	return WebhookEvent{
		Event:               headers.Get("X-GitHub-Event"),
		Action:              payload.Action,
		DeliveryID:          headers.Get("X-GitHub-Delivery"),
		Repository:          payload.Repository,
		Installation:        payload.Installation,
		PullRequest:         payload.PullRequest,
		Issue:               payload.Issue,
		Comment:             payload.Comment,
		Repositories:        payload.Repositories,
		RepositoriesAdded:   payload.RepositoriesAdded,
		RepositoriesRemoved: payload.RepositoriesRemoved,
		Sender:              payload.Sender,
		RawPayload:          append([]byte(nil), body...),
	}, nil
}

func (e WebhookEvent) ShouldTriggerReview() bool {
	if e.Event != EventPullRequest || e.PullRequest == nil {
		return false
	}
	return e.Action == "opened" || e.Action == "synchronize"
}

func (e WebhookEvent) Command() string {
	if e.Event != EventIssueComment || e.Issue == nil || e.Issue.PullRequest == nil || e.Comment == nil {
		return ""
	}
	body := strings.TrimSpace(e.Comment.Body)
	if !strings.HasPrefix(body, "/ai-") {
		return ""
	}
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func (e WebhookEvent) ShouldTriggerCommand() bool {
	return e.Event == EventIssueComment && e.Action == "created" && e.Command() != ""
}

func (e WebhookEvent) ShouldTriggerInstallation() bool {
	switch e.Event {
	case EventInstallation:
		return e.Action == "created" || e.Action == "deleted"
	case EventInstallationRepositories:
		return e.Action == "added" || e.Action == "removed"
	default:
		return false
	}
}

func (e WebhookEvent) CommandArgs() string {
	if e.Command() == "" || e.Comment == nil {
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(e.Comment.Body))
	if len(fields) <= 1 {
		return ""
	}
	return strings.Join(fields[1:], " ")
}

func (e WebhookEvent) Actor() string {
	if e.Comment != nil && e.Comment.User.Login != "" {
		return e.Comment.User.Login
	}
	return e.Sender.Login
}
