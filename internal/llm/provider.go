package llm

import (
	"context"
	"fmt"

	"github.com/awhg23/pr-go/internal/review"
)

type Reviewer interface {
	Review(context.Context, review.Input) (review.Result, error)
}

func NewReviewer(provider string) (Reviewer, error) {
	switch provider {
	case "openai", "":
		return NewOpenAIReviewerFromEnv(), nil
	case "mock":
		return MockReviewer{}, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", provider)
	}
}
