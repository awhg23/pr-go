package policy

import (
	"strings"
	"testing"
)

func TestParsePolicyConfig(t *testing.T) {
	raw := `version: 1
review:
  language: zh-CN
  ignore_files:
    - "**/*.lock"
  high_risk_paths:
    - "internal/auth/**"
approval:
  auto_approve:
    enabled: true
  require_human_when:
    - "changed_files > 30"
  required_checks:
    - "test"
tests:
  require_changed_tests: true
model:
  provider: openai-compatible
  model: gpt-test
  temperature: 0.3
`
	cfg, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}
	if cfg.Review.Language != "zh-CN" {
		t.Fatalf("language = %q", cfg.Review.Language)
	}
	if !cfg.Approval.AutoApprove.Enabled {
		t.Fatal("auto approve should be enabled")
	}
	if cfg.Model.Temperature == nil || *cfg.Model.Temperature != 0.3 {
		t.Fatalf("temperature = %#v", cfg.Model.Temperature)
	}
	if !MatchAny(cfg.Review.IgnoreFiles, "web/yarn.lock") {
		t.Fatal("ignore glob should match nested lock file")
	}
	if !MatchAny(cfg.Review.HighRiskPaths, "internal/auth/token.go") {
		t.Fatal("high-risk path should match")
	}
}

func TestParseInvalidPolicyConfig(t *testing.T) {
	_, err := Parse("version: nope\n")
	if err == nil {
		t.Fatal("expected invalid version error")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Fatalf("error = %v, want version detail", err)
	}
}

func TestDefaultPolicyIsSafe(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Approval.AutoApprove.Enabled {
		t.Fatal("default auto approve must be disabled")
	}
	if !MatchAny(cfg.Review.HighRiskPaths, "migrations/001.sql") {
		t.Fatal("default policy should treat migrations as high risk")
	}
	if !ChangedTestFileExists([]string{"main_test.go"}, cfg.Tests.TestFilePatterns) {
		t.Fatal("default test pattern should match root Go test files")
	}
}

func TestRequiredCheckReasons(t *testing.T) {
	reasons := RequiredCheckReasons([]string{"test", "lint"}, []string{"test=success", "lint=failure"})
	if len(reasons) != 1 || !strings.Contains(reasons[0], "lint") {
		t.Fatalf("reasons = %#v, want lint failure", reasons)
	}
	reasons = RequiredCheckReasons([]string{"test"}, []string{"lint=success"})
	if len(reasons) != 1 || !strings.Contains(reasons[0], "missing") {
		t.Fatalf("reasons = %#v, want missing test", reasons)
	}
}

func TestHumanReviewRuleReasons(t *testing.T) {
	reasons := HumanReviewRuleReasons(
		[]string{"changed_files > 1", "path matches internal/auth/**"},
		"low",
		"success",
		[]string{"README.md", "internal/auth/token.go"},
	)
	if len(reasons) != 2 {
		t.Fatalf("reasons = %#v, want two matches", reasons)
	}
}
