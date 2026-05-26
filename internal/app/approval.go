package app

import (
	"fmt"
	"strings"

	"github.com/awhg23/pr-go/internal/github"
	"github.com/awhg23/pr-go/internal/store"
)

type ApprovalDecision struct {
	Result         string
	Reasons        []string
	ApprovalStatus string
	ReviewRunID    int64
}

func DecideApproval(
	currentHead string,
	checks github.ChecksSummary,
	run store.ReviewRunSnapshot,
	hasRun bool,
	risk store.RiskSnapshot,
	hasRisk bool,
	findings []store.FindingSnapshot,
) ApprovalDecision {
	var reasons []string
	reviewRunID := int64(0)
	blocked := false
	humanReview := false

	if !hasRun {
		blocked = true
		reasons = append(reasons, "最新 PR 还没有成功审查记录，请先运行 /ai-review 或 /ai-recheck。")
	} else {
		reviewRunID = run.ID
		if run.HeadSHA != currentHead {
			blocked = true
			reasons = append(reasons, "最新 commit 尚未完成审查，请先运行 /ai-recheck。")
		}
	}
	if checks.State != "success" {
		blocked = true
		reasons = append(reasons, fmt.Sprintf("CI/check 状态不是 success：%s。", checks.State))
	}

	for _, finding := range findings {
		switch strings.ToLower(finding.Severity) {
		case "blocker", "high":
			blocked = true
			reasons = append(reasons, fmt.Sprintf("存在未解决 %s finding：%s。", finding.Severity, finding.FindingID))
		case "medium":
			humanReview = true
			reasons = append(reasons, fmt.Sprintf("存在未解决 medium finding：%s。", finding.FindingID))
		}
	}
	if hasRisk {
		switch risk.Level {
		case "blocker", "high":
			blocked = true
			reasons = append(reasons, fmt.Sprintf("当前风险等级为 %s。", risk.Level))
		case "medium":
			humanReview = true
			reasons = append(reasons, "当前风险等级为 medium。")
		}
	} else {
		humanReview = true
		reasons = append(reasons, "没有可用的风险评分记录。")
	}

	if blocked {
		return ApprovalDecision{Result: ApprovalBlocked, Reasons: reasons, ApprovalStatus: "approval_blocked", ReviewRunID: reviewRunID}
	}
	if humanReview {
		return ApprovalDecision{Result: HumanReviewRequired, Reasons: reasons, ApprovalStatus: "human_review_required", ReviewRunID: reviewRunID}
	}
	return ApprovalDecision{
		Result:         ApprovalRecommended,
		Reasons:        []string{"最新 commit 已审查、CI/check 通过，且无未解决 high/blocker finding。"},
		ApprovalStatus: "approval_recommended",
		ReviewRunID:    reviewRunID,
	}
}

func RenderApproveCheckComment(decision ApprovalDecision, checks github.ChecksSummary, risk store.RiskSnapshot, hasRisk bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## AI Approve Check\n\n")
	fmt.Fprintf(&b, "**Result:** `%s`\n\n", decision.Result)
	fmt.Fprintf(&b, "- CI/Checks: `%s`\n", checks.State)
	if hasRisk {
		fmt.Fprintf(&b, "- Risk: `%s` (%d)\n", risk.Level, risk.Score)
	} else {
		fmt.Fprintf(&b, "- Risk: `unknown`\n")
	}
	fmt.Fprintf(&b, "\n### Reasons\n\n")
	for _, reason := range decision.Reasons {
		fmt.Fprintf(&b, "- %s\n", reason)
	}
	fmt.Fprintf(&b, "\nV3 只输出审批建议，不会自动 approve 或 merge。\n")
	return b.String()
}

func RenderRiskCommandComment(risk store.RiskSnapshot, ok bool) string {
	if !ok {
		return "当前 PR 还没有风险评分记录。请先运行 `/ai-review`。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## AI Risk\n\n")
	fmt.Fprintf(&b, "- Level: `%s`\n", risk.Level)
	fmt.Fprintf(&b, "- Score: `%d`\n", risk.Score)
	fmt.Fprintf(&b, "- Review Run: `%d`\n", risk.ReviewRunID)
	fmt.Fprintf(&b, "\n### Reasons\n\n")
	for _, reason := range risk.Reasons {
		fmt.Fprintf(&b, "- %s\n", reason)
	}
	return b.String()
}
