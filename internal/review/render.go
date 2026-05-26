package review

import (
	"fmt"
	"strings"

	"github.com/awhg23/pr-go/internal/github"
)

func RenderMarkdown(input Input, result Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# PR Review Summary\n\n")
	fmt.Fprintf(&b, "- Repository: `%s/%s`\n", input.Owner, input.Repo)
	fmt.Fprintf(&b, "- Pull Request: #%d `%s`\n", input.Number, input.Title)
	fmt.Fprintf(&b, "- Author: `%s`\n", input.AuthorLogin)
	fmt.Fprintf(&b, "- Schema: `%s`, Prompt: `%s`\n", result.SchemaVersion, result.PromptVersion)
	fmt.Fprintf(&b, "- Files: %d, additions: %d, deletions: %d\n", input.ChangedCount, input.TotalAdditions, input.TotalDeletions)
	if len(input.IgnoredFiles) > 0 {
		fmt.Fprintf(&b, "- Policy ignored files: %s\n", strings.Join(input.IgnoredFiles, ", "))
	}
	for _, warning := range input.PolicyWarnings {
		fmt.Fprintf(&b, "- Policy warning: %s\n", warning)
	}
	if input.DiffTruncated {
		fmt.Fprintf(&b, "- Diff: compressed to %d bytes, omitted about %d bytes\n", input.MaxDiffBytes, input.OmittedBytes)
	}
	fmt.Fprintf(&b, "\n## Risk\n\n")
	fmt.Fprintf(&b, "- Level: `%s`\n", result.Risk.Level)
	fmt.Fprintf(&b, "- Score: `%d`\n", result.Risk.Score)
	fmt.Fprintf(&b, "- Reasons:\n")
	for _, reason := range result.Risk.Reasons {
		fmt.Fprintf(&b, "  - %s\n", reason)
	}

	fmt.Fprintf(&b, "\n## Summary\n\n%s\n", strings.TrimSpace(result.Summary))
	fmt.Fprintf(&b, "\n## Findings\n\n")
	if len(result.Findings) == 0 {
		fmt.Fprintf(&b, "No structured findings returned.\n")
		return b.String()
	}
	for _, finding := range result.Findings {
		location := finding.FilePath
		if finding.LineNumber > 0 {
			location = fmt.Sprintf("%s:%d", finding.FilePath, finding.LineNumber)
		}
		fmt.Fprintf(&b, "### [%s] %s\n\n", strings.ToUpper(finding.Severity), finding.Title)
		fmt.Fprintf(&b, "- ID: `%s`\n", finding.ID)
		fmt.Fprintf(&b, "- Location: `%s`\n", location)
		fmt.Fprintf(&b, "- Category: `%s`\n", finding.Category)
		fmt.Fprintf(&b, "- Reason: %s\n", finding.Reason)
		fmt.Fprintf(&b, "- Suggestion: %s\n\n", finding.Suggestion)
	}
	return b.String()
}

func RenderGitHubComment(input Input, result Result, checks github.ChecksSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## PR Approval Agent\n\n")
	fmt.Fprintf(&b, "**Risk Level:** `%s`  \n", strings.ToUpper(result.Risk.Level))
	fmt.Fprintf(&b, "**Risk Score:** `%d`  \n", result.Risk.Score)
	fmt.Fprintf(&b, "**CI/Checks:** `%s`\n\n", checks.State)
	if len(input.PolicyWarnings) > 0 {
		fmt.Fprintf(&b, "### Policy Warnings\n\n")
		for _, warning := range input.PolicyWarnings {
			fmt.Fprintf(&b, "- %s\n", warning)
		}
		fmt.Fprintf(&b, "\n")
	}

	fmt.Fprintf(&b, "### Key Reasons\n\n")
	for _, reason := range result.Risk.Reasons {
		fmt.Fprintf(&b, "- %s\n", reason)
	}
	if len(checks.Details) > 0 {
		fmt.Fprintf(&b, "- Check details: %s\n", strings.Join(checks.Details, ", "))
	}

	fmt.Fprintf(&b, "\n### Summary\n\n%s\n\n", strings.TrimSpace(result.Summary))
	fmt.Fprintf(&b, "### Findings\n\n")
	if len(result.Findings) == 0 {
		fmt.Fprintf(&b, "No structured findings returned.\n\n")
	} else {
		for _, finding := range result.Findings {
			location := finding.FilePath
			if finding.LineNumber > 0 {
				location = fmt.Sprintf("%s:%d", finding.FilePath, finding.LineNumber)
			}
			fmt.Fprintf(&b, "- **[%s] %s** `%s`\n", strings.ToUpper(finding.Severity), finding.Title, location)
			fmt.Fprintf(&b, "  - Reason: %s\n", finding.Reason)
			fmt.Fprintf(&b, "  - Suggestion: %s\n", finding.Suggestion)
		}
		fmt.Fprintf(&b, "\n")
	}

	fmt.Fprintf(&b, "### Next Steps\n\n")
	switch result.Risk.Level {
	case "low":
		fmt.Fprintf(&b, "- Maintainers can continue with normal review.\n")
	case "medium":
		fmt.Fprintf(&b, "- Maintainers should review the findings before approval.\n")
	default:
		fmt.Fprintf(&b, "- Maintainers should not approve until the blocking/high-risk items are resolved or dismissed by policy.\n")
	}
	if checks.State != "success" {
		fmt.Fprintf(&b, "- Wait for CI/checks to become successful before final approval.\n")
	}
	fmt.Fprintf(&b, "- Auto approve is disabled unless repository policy explicitly enables it and the final approval check passes.\n")
	return b.String()
}
