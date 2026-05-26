package app

import (
	"context"
	"strings"
	"testing"

	"github.com/awhg23/pr-go/internal/github"
	"github.com/awhg23/pr-go/internal/store"
)

func TestIsMaintainerPermission(t *testing.T) {
	for _, permission := range []string{"write", "maintain", "admin"} {
		if !IsMaintainerPermission(permission) {
			t.Fatalf("permission %q should be allowed", permission)
		}
	}
	for _, permission := range []string{"read", "triage", ""} {
		if IsMaintainerPermission(permission) {
			t.Fatalf("permission %q should be denied", permission)
		}
	}
}

func TestParseDismissArgs(t *testing.T) {
	id, reason, ok := ParseDismissArgs("F-001 false positive after manual review")
	if !ok {
		t.Fatal("expected args to parse")
	}
	if id != "F-001" || reason != "false positive after manual review" {
		t.Fatalf("id/reason = %q/%q", id, reason)
	}
	if _, _, ok := ParseDismissArgs("F-001"); ok {
		t.Fatal("expected missing reason to be invalid")
	}
}

func TestHandleRiskCommandWithoutRiskScore(t *testing.T) {
	st := &commandStore{}
	gh := &fakeCommandGitHub{}
	server := &Server{store: st}
	cmdCtx := commandContext{Ref: github.PullRequestRef{Owner: "owner", Repo: "repo", Number: 1}, PRID: 10, CommandID: 20, GitHub: gh}

	if err := server.handleRiskCommand(context.Background(), cmdCtx); err != nil {
		t.Fatalf("handleRiskCommand error = %v", err)
	}
	if st.finishStatus != "success" {
		t.Fatalf("finish status = %q, want success", st.finishStatus)
	}
	if len(gh.comments) != 1 || !strings.Contains(gh.comments[0], "请先运行 `/ai-review`") {
		t.Fatalf("comment = %#v, want review hint", gh.comments)
	}
}

func TestHandleRiskCommandWithRiskScore(t *testing.T) {
	st := &commandStore{
		risk:    store.RiskSnapshot{ReviewRunID: 7, Level: "medium", Score: 42, Reasons: []string{"CI failed"}},
		hasRisk: true,
	}
	gh := &fakeCommandGitHub{}
	server := &Server{store: st}
	cmdCtx := commandContext{Ref: github.PullRequestRef{Owner: "owner", Repo: "repo", Number: 1}, PRID: 10, CommandID: 20, GitHub: gh}

	if err := server.handleRiskCommand(context.Background(), cmdCtx); err != nil {
		t.Fatalf("handleRiskCommand error = %v", err)
	}
	if len(gh.comments) != 1 || !strings.Contains(gh.comments[0], "Score: `42`") {
		t.Fatalf("comment = %#v, want risk score", gh.comments)
	}
}

func TestHandleDismissCommandRequiresReason(t *testing.T) {
	st := &commandStore{}
	gh := &fakeCommandGitHub{}
	server := &Server{store: st}
	event := WebhookEvent{Event: EventIssueComment, Issue: testIssueWithPR(1), Comment: &Comment{Body: "/ai-dismiss F-001"}}
	cmdCtx := commandContext{Ref: github.PullRequestRef{Owner: "owner", Repo: "repo", Number: 1}, PRID: 10, CommandID: 20, GitHub: gh}

	if err := server.handleDismissCommand(context.Background(), event, cmdCtx); err != nil {
		t.Fatalf("handleDismissCommand error = %v", err)
	}
	if st.finishStatus != "failed" || st.finishError != "invalid arguments" {
		t.Fatalf("finish = %q/%q, want failed invalid arguments", st.finishStatus, st.finishError)
	}
}

func TestHandleDismissCommandFindingNotFound(t *testing.T) {
	st := &commandStore{}
	gh := &fakeCommandGitHub{}
	server := &Server{store: st}
	event := WebhookEvent{Event: EventIssueComment, Issue: testIssueWithPR(1), Comment: &Comment{Body: "/ai-dismiss F-404 false positive"}}
	cmdCtx := commandContext{Ref: github.PullRequestRef{Owner: "owner", Repo: "repo", Number: 1}, PRID: 10, CommandID: 20, GitHub: gh}

	if err := server.handleDismissCommand(context.Background(), event, cmdCtx); err != nil {
		t.Fatalf("handleDismissCommand error = %v", err)
	}
	if st.finishStatus != "failed" || st.finishError != "finding not found" {
		t.Fatalf("finish = %q/%q, want failed finding not found", st.finishStatus, st.finishError)
	}
}

func TestHandleDismissCommandSuccess(t *testing.T) {
	st := &commandStore{dismissed: true}
	gh := &fakeCommandGitHub{}
	server := &Server{store: st}
	event := WebhookEvent{Event: EventIssueComment, Issue: testIssueWithPR(1), Comment: &Comment{Body: "/ai-dismiss F-001 false positive"}}
	cmdCtx := commandContext{Ref: github.PullRequestRef{Owner: "owner", Repo: "repo", Number: 1}, Actor: "maintainer", PRID: 10, CommandID: 20, GitHub: gh}

	if err := server.handleDismissCommand(context.Background(), event, cmdCtx); err != nil {
		t.Fatalf("handleDismissCommand error = %v", err)
	}
	if st.finishStatus != "success" {
		t.Fatalf("finish status = %q, want success", st.finishStatus)
	}
	if st.dismissActor != "maintainer" || st.dismissReason != "false positive" {
		t.Fatalf("dismiss audit = %q/%q, want actor/reason", st.dismissActor, st.dismissReason)
	}
	if st.auditAction != "finding_dismissed" {
		t.Fatalf("audit action = %q, want finding_dismissed", st.auditAction)
	}
}

func TestHandleApproveCheckCommandSavesDecision(t *testing.T) {
	st := &commandStore{
		risk:    store.RiskSnapshot{Level: "low", Score: 10},
		hasRisk: true,
		run:     store.ReviewRunSnapshot{ID: 7, HeadSHA: "sha"},
		hasRun:  true,
	}
	gh := &fakeCommandGitHub{
		pr:     github.PullRequest{Ref: github.PullRequestRef{Owner: "owner", Repo: "repo", Number: 1}, Title: "PR", AuthorLogin: "octo", HeadSHA: "sha"},
		checks: github.ChecksSummary{State: "success"},
	}
	server := &Server{store: st}
	cmdCtx := commandContext{Ref: github.PullRequestRef{Owner: "owner", Repo: "repo", Number: 1}, Actor: "maintainer", RepoID: 5, PRID: 10, CommandID: 20, GitHub: gh}

	if err := server.handleApproveCheckCommand(context.Background(), cmdCtx); err != nil {
		t.Fatalf("handleApproveCheckCommand error = %v", err)
	}
	if st.approval.Result != ApprovalRecommended {
		t.Fatalf("approval result = %q, want %q", st.approval.Result, ApprovalRecommended)
	}
	if st.approvalStatus != "approval_recommended" {
		t.Fatalf("approval status = %q, want approval_recommended", st.approvalStatus)
	}
	if len(gh.comments) != 1 || !strings.Contains(gh.comments[0], ApprovalRecommended) {
		t.Fatalf("comment = %#v, want approval recommendation", gh.comments)
	}
}

func TestHandleApproveCheckCommandAutoApprovesWhenPolicyEnabled(t *testing.T) {
	st := &commandStore{
		risk:    store.RiskSnapshot{Level: "low", Score: 10},
		hasRisk: true,
		run:     store.ReviewRunSnapshot{ID: 7, HeadSHA: "sha"},
		hasRun:  true,
	}
	gh := &fakeCommandGitHub{
		pr: github.PullRequest{
			Ref:         github.PullRequestRef{Owner: "owner", Repo: "repo", Number: 1},
			Title:       "PR",
			AuthorLogin: "octo",
			BaseSHA:     "base",
			HeadSHA:     "sha",
		},
		checks:      github.ChecksSummary{State: "success"},
		policyFound: true,
		policyRaw: `version: 1
approval:
  auto_approve:
    enabled: true
`,
	}
	server := &Server{store: st}
	cmdCtx := commandContext{Ref: github.PullRequestRef{Owner: "owner", Repo: "repo", Number: 1}, Actor: "maintainer", RepoID: 5, PRID: 10, CommandID: 20, GitHub: gh}

	if err := server.handleApproveCheckCommand(context.Background(), cmdCtx); err != nil {
		t.Fatalf("handleApproveCheckCommand error = %v", err)
	}
	if !gh.approved {
		t.Fatal("expected approve API to be called")
	}
	if !st.approval.AutoApproved {
		t.Fatal("approval check should record auto approved")
	}
	if st.approvalStatus != "auto_approved" {
		t.Fatalf("approval status = %q, want auto_approved", st.approvalStatus)
	}
	if len(gh.comments) != 1 || !strings.Contains(gh.comments[0], "Auto Approve: `approved`") {
		t.Fatalf("comment = %#v, want auto approve success", gh.comments)
	}
}

func TestHandleApproveCheckInvalidPolicyCommentsWarningAndUsesDefault(t *testing.T) {
	st := &commandStore{
		risk:    store.RiskSnapshot{Level: "low", Score: 10},
		hasRisk: true,
		run:     store.ReviewRunSnapshot{ID: 7, HeadSHA: "sha"},
		hasRun:  true,
	}
	gh := &fakeCommandGitHub{
		pr:          github.PullRequest{Ref: github.PullRequestRef{Owner: "owner", Repo: "repo", Number: 1}, BaseSHA: "base", HeadSHA: "sha"},
		checks:      github.ChecksSummary{State: "success"},
		policyFound: true,
		policyRaw:   "version: nope\n",
	}
	server := &Server{store: st}
	cmdCtx := commandContext{Ref: github.PullRequestRef{Owner: "owner", Repo: "repo", Number: 1}, Actor: "maintainer", RepoID: 5, PRID: 10, CommandID: 20, GitHub: gh}

	if err := server.handleApproveCheckCommand(context.Background(), cmdCtx); err != nil {
		t.Fatalf("handleApproveCheckCommand error = %v", err)
	}
	if gh.approved {
		t.Fatal("invalid config should fall back to default auto approve disabled")
	}
	if len(gh.comments) != 2 || !strings.Contains(gh.comments[0], "配置无效") {
		t.Fatalf("comments = %#v, want config warning then result", gh.comments)
	}
}

type commandStore struct {
	fakeStore
	risk           store.RiskSnapshot
	hasRisk        bool
	run            store.ReviewRunSnapshot
	hasRun         bool
	findings       []store.FindingSnapshot
	dismissed      bool
	dismissActor   string
	dismissReason  string
	finishStatus   string
	finishResult   string
	finishError    string
	approval       store.ApprovalCheck
	approvalStatus string
	auditAction    string
}

func testIssueWithPR(number int) *Issue {
	marker := any(struct{}{})
	return &Issue{Number: number, PullRequest: &marker}
}

func (s *commandStore) FinishCommentCommand(_ context.Context, _ int64, status, resultMessage, errorMessage string) error {
	s.finishStatus = status
	s.finishResult = resultMessage
	s.finishError = errorMessage
	return nil
}

func (s *commandStore) LatestRiskScore(context.Context, int64) (store.RiskSnapshot, bool, error) {
	return s.risk, s.hasRisk, nil
}

func (s *commandStore) LatestSuccessfulReviewRun(context.Context, int64) (store.ReviewRunSnapshot, bool, error) {
	return s.run, s.hasRun, nil
}

func (s *commandStore) ListOpenFindings(context.Context, int64) ([]store.FindingSnapshot, error) {
	return s.findings, nil
}

func (s *commandStore) DismissFinding(_ context.Context, _ int64, _ string, actor, reason string) (bool, error) {
	s.dismissActor = actor
	s.dismissReason = reason
	return s.dismissed, nil
}

func (s *commandStore) SaveApprovalCheck(_ context.Context, check store.ApprovalCheck) error {
	s.approval = check
	return nil
}

func (s *commandStore) UpdatePullRequestApprovalStatus(_ context.Context, _ int64, status string) error {
	s.approvalStatus = status
	return nil
}

func (s *commandStore) Audit(_ context.Context, log store.AuditLog) error {
	s.auditAction = log.Action
	return nil
}

type fakeCommandGitHub struct {
	permission  string
	comments    []string
	pr          github.PullRequest
	checks      github.ChecksSummary
	policyRaw   string
	policyFound bool
	approved    bool
}

func (g *fakeCommandGitHub) FetchCollaboratorPermission(context.Context, github.PullRequestRef, string) (string, error) {
	return g.permission, nil
}

func (g *fakeCommandGitHub) CreateIssueComment(_ context.Context, _ github.PullRequestRef, body string) error {
	g.comments = append(g.comments, body)
	return nil
}

func (g *fakeCommandGitHub) FetchPullRequest(context.Context, github.PullRequestRef) (github.PullRequest, error) {
	return g.pr, nil
}

func (g *fakeCommandGitHub) FetchChecksSummary(context.Context, github.PullRequestRef, string) (github.ChecksSummary, error) {
	return g.checks, nil
}

func (g *fakeCommandGitHub) FetchTextFile(context.Context, github.PullRequestRef, string, string) (string, bool, error) {
	return g.policyRaw, g.policyFound, nil
}

func (g *fakeCommandGitHub) ApprovePullRequest(context.Context, github.PullRequestRef, string) error {
	g.approved = true
	return nil
}
