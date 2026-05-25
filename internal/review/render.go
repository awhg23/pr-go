package review

import (
	"fmt"
	"strings"
)

func RenderMarkdown(input Input, result Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# PR Review Summary\n\n")
	fmt.Fprintf(&b, "- Repository: `%s/%s`\n", input.Owner, input.Repo)
	fmt.Fprintf(&b, "- Pull Request: #%d `%s`\n", input.Number, input.Title)
	fmt.Fprintf(&b, "- Author: `%s`\n", input.AuthorLogin)
	fmt.Fprintf(&b, "- Schema: `%s`, Prompt: `%s`\n", result.SchemaVersion, result.PromptVersion)
	fmt.Fprintf(&b, "- Files: %d, additions: %d, deletions: %d\n", input.ChangedCount, input.TotalAdditions, input.TotalDeletions)
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
