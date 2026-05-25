package github

import (
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
