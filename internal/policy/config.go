package policy

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const FileName = ".pr-approval-agent.yml"

type Config struct {
	Version  int            `json:"version"`
	Review   ReviewConfig   `json:"review"`
	Approval ApprovalConfig `json:"approval"`
	Tests    TestsConfig    `json:"tests"`
	Model    ModelConfig    `json:"model"`
}

type ReviewConfig struct {
	Language      string   `json:"language"`
	IgnoreFiles   []string `json:"ignore_files"`
	HighRiskPaths []string `json:"high_risk_paths"`
}

type ApprovalConfig struct {
	AutoApprove      AutoApproveConfig `json:"auto_approve"`
	RequireHumanWhen []string          `json:"require_human_when"`
	RequiredChecks   []string          `json:"required_checks"`
}

type AutoApproveConfig struct {
	Enabled bool `json:"enabled"`
}

type TestsConfig struct {
	RequireChangedTests bool     `json:"require_changed_tests"`
	TestFilePatterns    []string `json:"test_file_patterns"`
}

type ModelConfig struct {
	Provider    string   `json:"provider"`
	Model       string   `json:"model"`
	Temperature *float64 `json:"temperature,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		Version: 1,
		Review: ReviewConfig{
			Language: "en",
			HighRiskPaths: []string{
				"**/auth/**",
				"**/permission/**",
				"**/permissions/**",
				"migrations/**",
				"db/migrations/**",
				"payment/**",
				"payments/**",
				"go.mod",
				"go.sum",
				"package.json",
				"package-lock.json",
				"yarn.lock",
				"pnpm-lock.yaml",
			},
		},
		Tests: TestsConfig{
			TestFilePatterns: []string{
				"**/*_test.go",
				"**/test/**",
				"**/tests/**",
				"tests/**",
				"**/*.spec.*",
				"**/*.test.*",
			},
		},
		Model: ModelConfig{Provider: "default", Model: "default"},
	}
}

func Parse(raw string) (Config, error) {
	cfg := DefaultConfig()
	section := ""
	subsection := ""
	listKey := ""

	for idx, original := range strings.Split(raw, "\n") {
		lineNo := idx + 1
		if strings.Contains(original, "\t") {
			return Config{}, fmt.Errorf("line %d: tabs are not supported", lineNo)
		}
		line := stripComment(original)
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := countIndent(line)
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "- ") {
			if listKey == "" {
				return Config{}, fmt.Errorf("line %d: list item without a known list key", lineNo)
			}
			value := unquote(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			if value == "" {
				return Config{}, fmt.Errorf("line %d: empty list item", lineNo)
			}
			appendList(&cfg, listKey, value)
			continue
		}

		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return Config{}, fmt.Errorf("line %d: expected key: value", lineNo)
		}
		key = strings.TrimSpace(key)
		value = unquote(strings.TrimSpace(value))
		if key == "" {
			return Config{}, fmt.Errorf("line %d: empty key", lineNo)
		}
		listKey = ""

		switch indent {
		case 0:
			section = key
			subsection = ""
			if value != "" {
				if err := assignScalar(&cfg, key, value, lineNo); err != nil {
					return Config{}, err
				}
			}
		case 2:
			if section == "" {
				return Config{}, fmt.Errorf("line %d: nested key without section", lineNo)
			}
			subsection = key
			fullKey := section + "." + key
			if value == "" && isListKey(fullKey) {
				listKey = fullKey
				continue
			}
			if value != "" {
				if err := assignScalar(&cfg, fullKey, value, lineNo); err != nil {
					return Config{}, err
				}
			}
		case 4:
			if section == "" || subsection == "" {
				return Config{}, fmt.Errorf("line %d: nested key without parent section", lineNo)
			}
			fullKey := section + "." + subsection + "." + key
			if value == "" && isListKey(fullKey) {
				listKey = fullKey
				continue
			}
			if value != "" {
				if err := assignScalar(&cfg, fullKey, value, lineNo); err != nil {
					return Config{}, err
				}
			}
		default:
			return Config{}, fmt.Errorf("line %d: unsupported indentation %d", lineNo, indent)
		}
	}
	return cfg, validate(cfg)
}

func ProviderName(defaultProvider string, configured string) string {
	switch strings.TrimSpace(configured) {
	case "", "default":
		return defaultProvider
	case "openai-compatible":
		return "openai"
	default:
		return configured
	}
}

func SnapshotJSON(cfg Config) string {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func FilterIgnored(paths []string, patterns []string) (kept []string, ignored []string) {
	for _, p := range paths {
		if MatchAny(patterns, p) {
			ignored = append(ignored, p)
			continue
		}
		kept = append(kept, p)
	}
	return kept, ignored
}

func MatchAny(patterns []string, name string) bool {
	for _, pattern := range patterns {
		if Match(pattern, name) {
			return true
		}
	}
	return false
}

func Match(pattern string, name string) bool {
	pattern = strings.TrimSpace(pattern)
	name = strings.Trim(strings.ReplaceAll(name, "\\", "/"), "/")
	if pattern == "" {
		return false
	}
	pattern = strings.Trim(strings.ReplaceAll(pattern, "\\", "/"), "/")
	if ok, err := path.Match(pattern, name); err == nil && ok {
		return true
	}
	re, err := globRegex(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(name)
}

func RequiredCheckReasons(required []string, details []string) []string {
	if len(required) == 0 {
		return nil
	}
	statusByName := map[string]string{}
	for _, detail := range details {
		name, status, ok := strings.Cut(detail, "=")
		if !ok {
			continue
		}
		statusByName[strings.ToLower(strings.TrimSpace(name))] = strings.ToLower(strings.TrimSpace(status))
	}
	var reasons []string
	for _, check := range required {
		key := strings.ToLower(strings.TrimSpace(check))
		if key == "" {
			continue
		}
		status, ok := statusByName[key]
		if !ok {
			reasons = append(reasons, fmt.Sprintf("required check `%s` is missing", check))
			continue
		}
		if status != "success" {
			reasons = append(reasons, fmt.Sprintf("required check `%s` is %s", check, status))
		}
	}
	return reasons
}

func ChangedTestFileExists(paths []string, patterns []string) bool {
	return anyPathMatches(paths, patterns)
}

func HumanReviewRuleReasons(rules []string, riskLevel string, ciStatus string, changedPaths []string) []string {
	var reasons []string
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		switch {
		case rule == "":
			continue
		case strings.EqualFold(rule, "ci_status != success") && ciStatus != "success":
			reasons = append(reasons, "repository policy requires human review when CI/checks are not successful")
		case strings.HasPrefix(rule, "changed_files > "):
			thresholdRaw := strings.TrimSpace(strings.TrimPrefix(rule, "changed_files > "))
			threshold, err := strconv.Atoi(thresholdRaw)
			if err == nil && len(changedPaths) > threshold {
				reasons = append(reasons, fmt.Sprintf("repository policy requires human review when changed_files > %d", threshold))
			}
		case strings.HasPrefix(rule, "path matches "):
			pattern := strings.TrimSpace(strings.TrimPrefix(rule, "path matches "))
			if anyPathMatches(changedPaths, []string{pattern}) {
				reasons = append(reasons, "repository policy requires human review for path match: "+pattern)
			}
		case strings.HasPrefix(rule, "risk_level >= "):
			level := strings.TrimSpace(strings.TrimPrefix(rule, "risk_level >= "))
			if riskRank(riskLevel) >= riskRank(level) {
				reasons = append(reasons, "repository policy requires human review for risk level >= "+level)
			}
		}
	}
	return reasons
}

func anyPathMatches(paths []string, patterns []string) bool {
	for _, p := range paths {
		if MatchAny(patterns, p) {
			return true
		}
	}
	return false
}

func stripComment(line string) string {
	inQuote := rune(0)
	for i, r := range line {
		switch r {
		case '\'', '"':
			if inQuote == 0 {
				inQuote = r
			} else if inQuote == r {
				inQuote = 0
			}
		case '#':
			if inQuote == 0 {
				return line[:i]
			}
		}
	}
	return line
}

func countIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func unquote(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'")
	return value
}

func isListKey(key string) bool {
	switch key {
	case "review.ignore_files", "review.high_risk_paths", "approval.require_human_when", "approval.required_checks", "tests.test_file_patterns":
		return true
	default:
		return false
	}
}

func appendList(cfg *Config, key, value string) {
	switch key {
	case "review.ignore_files":
		cfg.Review.IgnoreFiles = append(cfg.Review.IgnoreFiles, value)
	case "review.high_risk_paths":
		if len(cfg.Review.HighRiskPaths) == len(DefaultConfig().Review.HighRiskPaths) {
			cfg.Review.HighRiskPaths = nil
		}
		cfg.Review.HighRiskPaths = append(cfg.Review.HighRiskPaths, value)
	case "approval.require_human_when":
		cfg.Approval.RequireHumanWhen = append(cfg.Approval.RequireHumanWhen, value)
	case "approval.required_checks":
		cfg.Approval.RequiredChecks = append(cfg.Approval.RequiredChecks, value)
	case "tests.test_file_patterns":
		if len(cfg.Tests.TestFilePatterns) == len(DefaultConfig().Tests.TestFilePatterns) {
			cfg.Tests.TestFilePatterns = nil
		}
		cfg.Tests.TestFilePatterns = append(cfg.Tests.TestFilePatterns, value)
	}
}

func assignScalar(cfg *Config, key, value string, lineNo int) error {
	switch key {
	case "version":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("line %d: version must be an integer", lineNo)
		}
		cfg.Version = parsed
	case "review.language":
		cfg.Review.Language = value
	case "approval.auto_approve.enabled":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("line %d: auto_approve.enabled must be true or false", lineNo)
		}
		cfg.Approval.AutoApprove.Enabled = parsed
	case "tests.require_changed_tests", "approval.require_tests":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("line %d: require_changed_tests must be true or false", lineNo)
		}
		cfg.Tests.RequireChangedTests = parsed
	case "model.provider":
		cfg.Model.Provider = value
	case "model.model":
		cfg.Model.Model = value
	case "model.temperature":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("line %d: model.temperature must be a number", lineNo)
		}
		cfg.Model.Temperature = &parsed
	}
	return nil
}

func validate(cfg Config) error {
	if cfg.Version != 1 {
		return fmt.Errorf("unsupported version %d", cfg.Version)
	}
	switch cfg.Model.Provider {
	case "", "default", "openai", "openai-compatible", "deepseek", "siliconflow", "ollama", "mock":
	default:
		return fmt.Errorf("unsupported model.provider %q", cfg.Model.Provider)
	}
	if cfg.Model.Temperature != nil && (*cfg.Model.Temperature < 0 || *cfg.Model.Temperature > 2) {
		return fmt.Errorf("model.temperature must be between 0 and 2")
	}
	return nil
}

func globRegex(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		if ch == '*' {
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					b.WriteString("(?:.*/)?")
					i++
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
			continue
		}
		if ch == '?' {
			b.WriteString("[^/]")
			continue
		}
		b.WriteString(regexp.QuoteMeta(string(ch)))
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func riskRank(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "blocker":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}
