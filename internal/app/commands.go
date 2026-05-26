package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/awhg23/pr-go/internal/github"
	"github.com/awhg23/pr-go/internal/policy"
	"github.com/awhg23/pr-go/internal/store"
)

const (
	ApprovalRecommended = "建议审批"
	HumanReviewRequired = "需要人工重点审查"
	ApprovalBlocked     = "暂不建议审批"
)

type commandContext struct {
	Owner     string
	Repo      string
	Ref       github.PullRequestRef
	Actor     string
	RepoID    int64
	PRID      int64
	CommandID int64
	GitHub    commandGitHubClient
}

type commandGitHubClient interface {
	FetchCollaboratorPermission(context.Context, github.PullRequestRef, string) (string, error)
	CreateIssueComment(context.Context, github.PullRequestRef, string) error
	FetchPullRequest(context.Context, github.PullRequestRef) (github.PullRequest, error)
	FetchChecksSummary(context.Context, github.PullRequestRef, string) (github.ChecksSummary, error)
	FetchTextFile(context.Context, github.PullRequestRef, string, string) (string, bool, error)
	ApprovePullRequest(context.Context, github.PullRequestRef, string) error
}

func (s *Server) handleCommand(ctx context.Context, event WebhookEvent) error {
	command := event.Command()
	if command == "" {
		return nil
	}

	cmdCtx, err := s.prepareCommand(ctx, event)
	if err != nil {
		return err
	}

	permission, err := cmdCtx.GitHub.FetchCollaboratorPermission(ctx, cmdCtx.Ref, cmdCtx.Actor)
	if err != nil {
		_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
		return fmt.Errorf("fetch collaborator permission: %w", err)
	}
	if !IsMaintainerPermission(permission) {
		message := fmt.Sprintf("权限不足：`%s` 需要仓库维护者权限。当前权限：`%s`。", command, permission)
		if err := cmdCtx.GitHub.CreateIssueComment(ctx, cmdCtx.Ref, message); err != nil {
			_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
			return fmt.Errorf("publish permission denied comment: %w", err)
		}
		_ = s.auditCommand(ctx, cmdCtx, "command_permission_denied", map[string]string{"command": command, "permission": permission})
		return s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", message, "permission denied")
	}

	switch command {
	case "/ai-review":
		return s.handleReviewCommand(ctx, event, cmdCtx, "comment.ai-review")
	case "/ai-recheck":
		return s.handleReviewCommand(ctx, event, cmdCtx, "comment.ai-recheck")
	case "/ai-risk":
		return s.handleRiskCommand(ctx, cmdCtx)
	case "/ai-dismiss":
		return s.handleDismissCommand(ctx, event, cmdCtx)
	case "/ai-approve-check":
		return s.handleApproveCheckCommand(ctx, cmdCtx)
	default:
		message := "不支持的命令。支持：`/ai-review`、`/ai-risk`、`/ai-approve-check`、`/ai-dismiss <finding-id> <reason>`、`/ai-recheck`。"
		if err := cmdCtx.GitHub.CreateIssueComment(ctx, cmdCtx.Ref, message); err != nil {
			_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
			return fmt.Errorf("publish unsupported command comment: %w", err)
		}
		return s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", message, "unsupported command")
	}
}

func (s *Server) prepareCommand(ctx context.Context, event WebhookEvent) (commandContext, error) {
	if event.Installation.ID == 0 {
		return commandContext{}, fmt.Errorf("webhook missing installation id")
	}
	if event.Issue == nil || event.Issue.PullRequest == nil {
		return commandContext{}, fmt.Errorf("webhook comment is not on a pull request")
	}
	owner, repo := event.RepositoryOwnerRepo()
	if owner == "" || repo == "" {
		return commandContext{}, fmt.Errorf("webhook missing repository owner/name")
	}
	actor := event.Actor()
	if actor == "" {
		return commandContext{}, fmt.Errorf("webhook missing command actor")
	}

	installationDBID, err := s.store.UpsertInstallation(ctx, store.Installation{
		InstallationID: event.Installation.ID,
		AccountLogin:   event.Repository.Owner.Login,
		AccountType:    "repository",
	})
	if err != nil {
		return commandContext{}, fmt.Errorf("record installation: %w", err)
	}
	repoID, err := s.store.UpsertRepository(ctx, store.Repository{
		InstallationDBID: installationDBID,
		Owner:            owner,
		Name:             repo,
		FullName:         event.Repository.FullName,
	})
	if err != nil {
		return commandContext{}, fmt.Errorf("record repository: %w", err)
	}
	prID, err := s.store.EnsurePullRequest(ctx, repoID, event.Issue.Number)
	if err != nil {
		return commandContext{}, fmt.Errorf("ensure pull request placeholder: %w", err)
	}
	commandID, err := s.store.CreateCommentCommand(ctx, store.CommentCommand{
		PullRequestID: prID,
		Command:       event.Command(),
		Args:          event.CommandArgs(),
		Actor:         actor,
		Status:        "running",
		DeliveryID:    event.DeliveryID,
	})
	if err != nil {
		return commandContext{}, fmt.Errorf("record comment command: %w", err)
	}
	token, err := s.auth.InstallationToken(ctx, event.Installation.ID)
	if err != nil {
		_ = s.store.FinishCommentCommand(ctx, commandID, "failed", "", err.Error())
		return commandContext{}, fmt.Errorf("create installation token: %w", err)
	}
	ref := github.PullRequestRef{Owner: owner, Repo: repo, Number: event.Issue.Number}
	return commandContext{
		Owner:     owner,
		Repo:      repo,
		Ref:       ref,
		Actor:     actor,
		RepoID:    repoID,
		PRID:      prID,
		CommandID: commandID,
		GitHub:    github.NewClient(token),
	}, nil
}

func (s *Server) handleReviewCommand(ctx context.Context, event WebhookEvent, cmdCtx commandContext, triggerType string) error {
	event.PullRequest = &PullRequest{Number: cmdCtx.Ref.Number}
	if err := s.reviewPullRequest(ctx, event, triggerType, cmdCtx.Actor); err != nil {
		_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
		return err
	}
	message := fmt.Sprintf("已执行 `%s`。", event.Command())
	return s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "success", message, "")
}

func (s *Server) handleRiskCommand(ctx context.Context, cmdCtx commandContext) error {
	risk, ok, err := s.store.LatestRiskScore(ctx, cmdCtx.PRID)
	if err != nil {
		_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
		return err
	}
	message := RenderRiskCommandComment(risk, ok)
	if err := cmdCtx.GitHub.CreateIssueComment(ctx, cmdCtx.Ref, message); err != nil {
		_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
		return err
	}
	return s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "success", message, "")
}

func (s *Server) handleDismissCommand(ctx context.Context, event WebhookEvent, cmdCtx commandContext) error {
	findingID, reason, ok := ParseDismissArgs(event.CommandArgs())
	if !ok {
		message := "命令格式错误：请使用 `/ai-dismiss <finding-id> <reason>`，reason 必填。"
		if err := cmdCtx.GitHub.CreateIssueComment(ctx, cmdCtx.Ref, message); err != nil {
			_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
			return err
		}
		return s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", message, "invalid arguments")
	}
	dismissed, err := s.store.DismissFinding(ctx, cmdCtx.PRID, findingID, cmdCtx.Actor, reason)
	if err != nil {
		_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
		return err
	}
	if !dismissed {
		message := fmt.Sprintf("未找到可关闭的 open finding：`%s`。", findingID)
		if err := cmdCtx.GitHub.CreateIssueComment(ctx, cmdCtx.Ref, message); err != nil {
			_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
			return err
		}
		return s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", message, "finding not found")
	}

	_ = s.auditCommand(ctx, cmdCtx, "finding_dismissed", map[string]string{"finding_id": findingID, "reason": reason})
	message := fmt.Sprintf("已关闭 finding `%s`。原因：%s", findingID, reason)
	if err := cmdCtx.GitHub.CreateIssueComment(ctx, cmdCtx.Ref, message); err != nil {
		_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
		return err
	}
	return s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "success", message, "")
}

func (s *Server) handleApproveCheckCommand(ctx context.Context, cmdCtx commandContext) error {
	pr, err := cmdCtx.GitHub.FetchPullRequest(ctx, cmdCtx.Ref)
	if err != nil {
		_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
		return err
	}
	policyConfig, policyWarnings := loadRepositoryPolicy(ctx, cmdCtx.GitHub, cmdCtx.Ref, pr.BaseSHA)
	cmdCtx.PRID, err = s.store.UpsertPullRequest(ctx, store.PullRequest{
		RepositoryID:   cmdCtx.RepoID,
		Number:         pr.Ref.Number,
		Title:          pr.Title,
		AuthorLogin:    pr.AuthorLogin,
		BaseSHA:        pr.BaseSHA,
		HeadSHA:        pr.HeadSHA,
		State:          "open",
		ApprovalStatus: "approval_checking",
	})
	if err != nil {
		_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
		return err
	}
	for _, warning := range policyWarnings {
		if err := cmdCtx.GitHub.CreateIssueComment(ctx, cmdCtx.Ref, warning); err != nil {
			_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
			return err
		}
		_ = s.auditCommand(ctx, cmdCtx, "policy_config_warning", map[string]string{"warning": warning})
	}
	checks, err := cmdCtx.GitHub.FetchChecksSummary(ctx, cmdCtx.Ref, pr.HeadSHA)
	if err != nil {
		checks = github.ChecksSummary{State: "unknown", Details: []string{"checks unavailable"}}
	}
	run, hasRun, err := s.store.LatestSuccessfulReviewRun(ctx, cmdCtx.PRID)
	if err != nil {
		_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
		return err
	}
	risk, hasRisk, err := s.store.LatestRiskScore(ctx, cmdCtx.PRID)
	if err != nil {
		_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
		return err
	}
	findings, err := s.store.ListOpenFindings(ctx, cmdCtx.PRID)
	if err != nil {
		_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
		return err
	}

	changedPaths := changedFilePaths(pr.Files)
	decision := DecideApproval(pr.HeadSHA, checks, run, hasRun, risk, hasRisk, findings, policyConfig, changedPaths)
	if decision.Result == ApprovalRecommended && policyConfig.Approval.AutoApprove.Enabled {
		body := "AI approval check passed and repository policy explicitly enabled auto approve."
		if err := cmdCtx.GitHub.ApprovePullRequest(ctx, cmdCtx.Ref, body); err != nil {
			decision.AutoApproveError = err.Error()
			decision.ApprovalStatus = "auto_approve_failed"
			decision.Reasons = append(decision.Reasons, "自动 approve 失败："+err.Error())
			_ = s.auditCommand(ctx, cmdCtx, "auto_approve_failed", map[string]string{
				"error":           err.Error(),
				"head_sha":        pr.HeadSHA,
				"config_snapshot": policy.SnapshotJSON(policyConfig),
			})
		} else {
			decision.AutoApproved = true
			decision.ApprovalStatus = "auto_approved"
			_ = s.auditCommand(ctx, cmdCtx, "auto_approved", map[string]string{
				"head_sha":        pr.HeadSHA,
				"config_snapshot": policy.SnapshotJSON(policyConfig),
			})
		}
	}
	if err := s.store.SaveApprovalCheck(ctx, store.ApprovalCheck{
		PullRequestID: cmdCtx.PRID,
		ReviewRunID:   decision.ReviewRunID,
		TriggeredBy:   cmdCtx.Actor,
		Result:        decision.Result,
		Reasons:       decision.Reasons,
		AutoApproved:  decision.AutoApproved,
	}); err != nil {
		_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
		return err
	}
	if err := s.store.UpdatePullRequestApprovalStatus(ctx, cmdCtx.PRID, decision.ApprovalStatus); err != nil {
		_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
		return err
	}

	message := RenderApproveCheckComment(decision, checks, risk, hasRisk, policyConfig)
	if err := cmdCtx.GitHub.CreateIssueComment(ctx, cmdCtx.Ref, message); err != nil {
		_ = s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "failed", "", err.Error())
		return err
	}
	_ = s.auditCommand(ctx, cmdCtx, "approval_check_completed", map[string]string{"result": decision.Result})
	return s.store.FinishCommentCommand(ctx, cmdCtx.CommandID, "success", message, "")
}

func changedFilePaths(files []github.ChangedFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Filename)
	}
	return paths
}

func (s *Server) auditCommand(ctx context.Context, cmdCtx commandContext, action string, detail map[string]string) error {
	raw, _ := json.Marshal(detail)
	return s.store.Audit(ctx, store.AuditLog{
		RepositoryID:  cmdCtx.RepoID,
		PullRequestID: cmdCtx.PRID,
		Actor:         cmdCtx.Actor,
		Action:        action,
		DetailJSON:    string(raw),
	})
}

func IsMaintainerPermission(permission string) bool {
	switch permission {
	case "write", "maintain", "admin":
		return true
	default:
		return false
	}
}

func ParseDismissArgs(args string) (string, string, bool) {
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], strings.Join(fields[1:], " "), true
}
