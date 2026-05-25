package review

import "strings"

func ScoreRisk(input Input, findings []Finding) Risk {
	score := 10
	reasons := []string{"base review risk starts at 10"}

	for _, finding := range findings {
		switch strings.ToLower(finding.Severity) {
		case "blocker":
			score += 45
			reasons = append(reasons, "blocker finding: "+finding.Title)
		case "high":
			score += 30
			reasons = append(reasons, "high finding: "+finding.Title)
		case "medium":
			score += 15
			reasons = append(reasons, "medium finding: "+finding.Title)
		case "low":
			score += 5
		}
	}

	if input.ChangedCount > 30 {
		score += 15
		reasons = append(reasons, "large PR changes more than 30 files")
	}
	if input.TotalAdditions+input.TotalDeletions > 1000 {
		score += 10
		reasons = append(reasons, "large line churn above 1000 lines")
	}
	if input.DiffTruncated {
		score += 10
		reasons = append(reasons, "diff was compressed before review")
	}
	switch input.CheckStatus {
	case "failure":
		score += 20
		reasons = append(reasons, "CI/check status is failing")
	case "pending", "unknown":
		score += 10
		reasons = append(reasons, "CI/check status is not confirmed")
	}
	for _, file := range input.ChangedFiles {
		if isHighRiskPath(file.Path) {
			score += 15
			reasons = append(reasons, "high-risk path changed: "+file.Path)
			break
		}
	}

	if score > 100 {
		score = 100
	}
	return Risk{Score: score, Level: riskLevel(score), Reasons: reasons}
}

func riskLevel(score int) string {
	switch {
	case score >= 85:
		return "blocker"
	case score >= 65:
		return "high"
	case score >= 35:
		return "medium"
	default:
		return "low"
	}
}

func isHighRiskPath(path string) bool {
	path = strings.ToLower(path)
	patterns := []string{"auth", "permission", "migrations", "migration", "payment", "secret", "security", "go.mod", "package.json"}
	for _, pattern := range patterns {
		if strings.Contains(path, pattern) {
			return true
		}
	}
	return false
}
