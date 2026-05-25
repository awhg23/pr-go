package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const apiBaseURL = "https://api.github.com"

type PullRequestRef struct {
	Owner  string
	Repo   string
	Number int
}

type PullRequest struct {
	Ref         PullRequestRef
	Title       string
	Body        string
	AuthorLogin string
	BaseSHA     string
	HeadSHA     string
	Files       []ChangedFile
}

type ChecksSummary struct {
	State   string
	Details []string
}

type ChangedFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
	Patch     string `json:"patch"`
}

type Client struct {
	token      string
	httpClient *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) FetchPullRequest(ctx context.Context, ref PullRequestRef) (PullRequest, error) {
	var payload struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		User  struct {
			Login string `json:"login"`
		} `json:"user"`
		Base struct {
			SHA string `json:"sha"`
		} `json:"base"`
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}

	prPath := fmt.Sprintf("/repos/%s/%s/pulls/%d", ref.Owner, ref.Repo, ref.Number)
	if err := c.getJSON(ctx, prPath, &payload); err != nil {
		return PullRequest{}, fmt.Errorf("fetch pull request: %w", err)
	}

	files, err := c.fetchFiles(ctx, ref)
	if err != nil {
		return PullRequest{}, err
	}

	return PullRequest{
		Ref:         ref,
		Title:       payload.Title,
		Body:        payload.Body,
		AuthorLogin: payload.User.Login,
		BaseSHA:     payload.Base.SHA,
		HeadSHA:     payload.Head.SHA,
		Files:       files,
	}, nil
}

func (c *Client) FetchChecksSummary(ctx context.Context, ref PullRequestRef, sha string) (ChecksSummary, error) {
	if sha == "" {
		return ChecksSummary{State: "unknown", Details: []string{"head sha is empty"}}, nil
	}

	var statusPayload struct {
		State    string `json:"state"`
		Statuses []struct {
			Context string `json:"context"`
			State   string `json:"state"`
		} `json:"statuses"`
	}
	statusPath := fmt.Sprintf("/repos/%s/%s/commits/%s/status", ref.Owner, ref.Repo, sha)
	if err := c.getJSON(ctx, statusPath, &statusPayload); err != nil {
		return ChecksSummary{}, fmt.Errorf("fetch combined status: %w", err)
	}

	var checkPayload struct {
		TotalCount int `json:"total_count"`
		CheckRuns  []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"check_runs"`
	}
	checksPath := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs?per_page=100", ref.Owner, ref.Repo, sha)
	if err := c.getJSON(ctx, checksPath, &checkPayload); err != nil {
		return ChecksSummary{}, fmt.Errorf("fetch check runs: %w", err)
	}

	return summarizeChecks(statusPayload.State, statusPayload.Statuses, checkPayload.CheckRuns), nil
}

func (c *Client) CreateIssueComment(ctx context.Context, ref PullRequestRef, body string) error {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", ref.Owner, ref.Repo, ref.Number)
	payload := map[string]string{"body": body}
	return c.postJSON(ctx, path, payload, nil)
}

func (c *Client) fetchFiles(ctx context.Context, ref PullRequestRef) ([]ChangedFile, error) {
	var all []ChangedFile
	for page := 1; ; page++ {
		var files []ChangedFile
		path := fmt.Sprintf("/repos/%s/%s/pulls/%d/files?per_page=100&page=%d", ref.Owner, ref.Repo, ref.Number, page)
		if err := c.getJSON(ctx, path, &files); err != nil {
			return nil, fmt.Errorf("fetch pull request files: %w", err)
		}
		all = append(all, files...)
		if len(files) < 100 {
			return all, nil
		}
	}
}

func (c *Client) getJSON(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github api returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func (c *Client) postJSON(ctx context.Context, path string, src any, dst any) error {
	body, err := json.Marshal(src)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github api returned %s", resp.Status)
	}
	if dst == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func summarizeChecks(statusState string, statuses []struct {
	Context string `json:"context"`
	State   string `json:"state"`
}, checkRuns []struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}) ChecksSummary {
	details := make([]string, 0, len(statuses)+len(checkRuns))
	state := "success"
	seen := false

	apply := func(name, value string) {
		if name == "" {
			name = "check"
		}
		if value == "" {
			value = "unknown"
		}
		seen = true
		details = append(details, fmt.Sprintf("%s=%s", name, value))
		switch value {
		case "failure", "error", "cancelled", "timed_out", "action_required":
			state = "failure"
		case "pending", "queued", "in_progress", "requested", "waiting", "unknown":
			if state != "failure" {
				state = "pending"
			}
		}
	}

	if statusState != "" && statusState != "success" {
		apply("combined-status", statusState)
	}
	for _, status := range statuses {
		apply(status.Context, status.State)
	}
	for _, run := range checkRuns {
		value := run.Conclusion
		if run.Status != "completed" {
			value = run.Status
		}
		apply(run.Name, value)
	}
	if !seen {
		return ChecksSummary{State: "unknown", Details: []string{"no status checks found"}}
	}
	return ChecksSummary{State: state, Details: details}
}

func ParsePullRequestURL(raw string) (PullRequestRef, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "www.")
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(parts) < 5 || parts[0] != "github.com" || parts[3] != "pull" {
		return PullRequestRef{}, fmt.Errorf("invalid GitHub PR URL %q", raw)
	}
	number, err := strconv.Atoi(parts[4])
	if err != nil || number <= 0 {
		return PullRequestRef{}, fmt.Errorf("invalid pull request number in %q", raw)
	}
	return PullRequestRef{Owner: parts[1], Repo: parts[2], Number: number}, nil
}
