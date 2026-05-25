package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/awhg23/pr-go/internal/review"
)

type OpenAIReviewer struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

func NewOpenAIReviewerFromEnv() *OpenAIReviewer {
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "gpt-4.1-mini"
	}
	return &OpenAIReviewer{
		apiKey:  os.Getenv("OPENAI_API_KEY"),
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (r *OpenAIReviewer) Review(ctx context.Context, input review.Input) (review.Result, error) {
	if r.apiKey == "" {
		return review.Result{}, errors.New("OPENAI_API_KEY is required for provider=openai; use --provider mock for offline verification")
	}

	body := chatRequest{
		Model: r.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt()},
			{Role: "user", Content: userPrompt(input)},
		},
		Temperature: 0.2,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return review.Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return review.Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return review.Result{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return review.Result{}, fmt.Errorf("llm provider returned %s", resp.Status)
	}

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return review.Result{}, err
	}
	if len(parsed.Choices) == 0 {
		return review.Result{}, errors.New("llm provider returned no choices")
	}

	var result review.Result
	content := stripJSONFence(parsed.Choices[0].Message.Content)
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return review.Result{}, fmt.Errorf("parse structured review JSON: %w", err)
	}
	normalizeFindings(&result)
	return result, nil
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func systemPrompt() string {
	return `You are a conservative PR approval review agent. Return only JSON matching:
{
  "summary": "short review summary",
  "findings": [
    {
      "id": "F-001",
      "file_path": "path",
      "line_number": 0,
      "severity": "low|medium|high|blocker",
      "category": "security|bug|test|maintainability|docs|other",
      "title": "short title",
      "reason": "why this matters",
      "suggestion": "actionable next step"
    }
  ]
}
Prefer fewer high-value findings. Do not approve the PR.`
}

func userPrompt(input review.Input) string {
	return fmt.Sprintf(`Review this GitHub pull request.

Repository: %s/%s
PR: #%d
Title: %s
Author: %s
Base SHA: %s
Head SHA: %s
Changed files: %d
Additions: %d
Deletions: %d
Diff truncated: %t

Description:
%s

Diff:
%s`, input.Owner, input.Repo, input.Number, input.Title, input.AuthorLogin, input.BaseSHA, input.HeadSHA,
		input.ChangedCount, input.TotalAdditions, input.TotalDeletions, input.DiffTruncated, input.Description, input.Diff)
}

func stripJSONFence(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}

func normalizeFindings(result *review.Result) {
	for i := range result.Findings {
		if result.Findings[i].ID == "" {
			result.Findings[i].ID = fmt.Sprintf("F-%03d", i+1)
		}
		if result.Findings[i].Severity == "" {
			result.Findings[i].Severity = "medium"
		}
		if result.Findings[i].Category == "" {
			result.Findings[i].Category = "other"
		}
	}
}
