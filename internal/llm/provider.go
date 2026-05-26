package llm

import (
	"context"
	"fmt"

	"github.com/awhg23/pr-go/internal/review"
)

type Reviewer interface {
	Review(context.Context, review.Input) (review.Result, error)
}

type Options struct {
	Model       string
	Temperature *float64
}

func NewReviewer(provider string) (Reviewer, error) {
	return NewReviewerWithOptions(provider, Options{})
}

func NewReviewerWithOptions(provider string, options Options) (Reviewer, error) {
	switch provider {
	case "openai", "openai-compatible", "deepseek", "siliconflow", "ollama", "":
		return NewOpenAIReviewerFromEnvWithOptions(options), nil
	case "mock":
		return MockReviewer{}, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", provider)
	}
}
