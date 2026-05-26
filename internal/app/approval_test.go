package app

import (
	"testing"

	"github.com/awhg23/pr-go/internal/github"
	"github.com/awhg23/pr-go/internal/policy"
	"github.com/awhg23/pr-go/internal/store"
)

func TestDecideApprovalRequiresReviewedHead(t *testing.T) {
	decision := DecideApproval(
		"new",
		github.ChecksSummary{State: "success"},
		store.ReviewRunSnapshot{ID: 1, HeadSHA: "old"},
		true,
		store.RiskSnapshot{Level: "low", Score: 10},
		true,
		nil,
		policy.DefaultConfig(),
		nil,
	)
	if decision.Result != ApprovalBlocked {
		t.Fatalf("result = %q, want blocked", decision.Result)
	}
}

func TestDecideApprovalBlocksFailedChecks(t *testing.T) {
	decision := DecideApproval(
		"sha",
		github.ChecksSummary{State: "failure"},
		store.ReviewRunSnapshot{ID: 1, HeadSHA: "sha"},
		true,
		store.RiskSnapshot{Level: "low", Score: 10},
		true,
		nil,
		policy.DefaultConfig(),
		nil,
	)
	if decision.Result != ApprovalBlocked {
		t.Fatalf("result = %q, want blocked", decision.Result)
	}
}

func TestDecideApprovalBlocksHighFinding(t *testing.T) {
	decision := DecideApproval(
		"sha",
		github.ChecksSummary{State: "success"},
		store.ReviewRunSnapshot{ID: 1, HeadSHA: "sha"},
		true,
		store.RiskSnapshot{Level: "low", Score: 10},
		true,
		[]store.FindingSnapshot{{FindingID: "F-001", Severity: "high"}},
		policy.DefaultConfig(),
		nil,
	)
	if decision.Result != ApprovalBlocked {
		t.Fatalf("result = %q, want blocked", decision.Result)
	}
}

func TestDecideApprovalRequiresHumanReviewForMedium(t *testing.T) {
	decision := DecideApproval(
		"sha",
		github.ChecksSummary{State: "success"},
		store.ReviewRunSnapshot{ID: 1, HeadSHA: "sha"},
		true,
		store.RiskSnapshot{Level: "medium", Score: 40},
		true,
		nil,
		policy.DefaultConfig(),
		nil,
	)
	if decision.Result != HumanReviewRequired {
		t.Fatalf("result = %q, want human review", decision.Result)
	}
}

func TestDecideApprovalRecommendsApproval(t *testing.T) {
	decision := DecideApproval(
		"sha",
		github.ChecksSummary{State: "success"},
		store.ReviewRunSnapshot{ID: 1, HeadSHA: "sha"},
		true,
		store.RiskSnapshot{Level: "low", Score: 10},
		true,
		nil,
		policy.DefaultConfig(),
		nil,
	)
	if decision.Result != ApprovalRecommended {
		t.Fatalf("result = %q, want recommended", decision.Result)
	}
}

func TestDecideApprovalBlocksMissingRequiredCheck(t *testing.T) {
	cfg := policy.DefaultConfig()
	cfg.Approval.RequiredChecks = []string{"test"}
	decision := DecideApproval(
		"sha",
		github.ChecksSummary{State: "success", Details: []string{"lint=success"}},
		store.ReviewRunSnapshot{ID: 1, HeadSHA: "sha"},
		true,
		store.RiskSnapshot{Level: "low", Score: 10},
		true,
		nil,
		cfg,
		nil,
	)
	if decision.Result != ApprovalBlocked {
		t.Fatalf("result = %q, want blocked", decision.Result)
	}
}

func TestDecideApprovalRequiresHumanReviewForMissingTests(t *testing.T) {
	cfg := policy.DefaultConfig()
	cfg.Tests.RequireChangedTests = true
	decision := DecideApproval(
		"sha",
		github.ChecksSummary{State: "success"},
		store.ReviewRunSnapshot{ID: 1, HeadSHA: "sha"},
		true,
		store.RiskSnapshot{Level: "low", Score: 10},
		true,
		nil,
		cfg,
		[]string{"main.go"},
	)
	if decision.Result != HumanReviewRequired {
		t.Fatalf("result = %q, want human review", decision.Result)
	}
}
