package app

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"

	"github.com/awhg23/pr-go/internal/store"
)

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.store.Ping(r.Context()); err != nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready\n"))
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	metrics, err := s.store.Metrics(r.Context())
	if err != nil {
		http.Error(w, "load metrics", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(renderMetrics(metrics)))
}

func (s *Server) handleAdminHome(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	repos, err := s.store.ListRepositorySummaries(r.Context(), 100)
	if err != nil {
		http.Error(w, "load repositories", http.StatusInternalServerError)
		return
	}
	data := struct {
		Repositories []store.RepositorySummary
		Token        string
	}{Repositories: repos, Token: r.URL.Query().Get("token")}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = adminHomeTemplate.Execute(w, data)
}

func (s *Server) handleAdminRepo(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	fullName := strings.TrimSpace(r.URL.Query().Get("full_name"))
	if fullName == "" {
		http.Error(w, "full_name is required", http.StatusBadRequest)
		return
	}
	report, err := s.store.RepositoryReport(r.Context(), fullName, 100)
	if err != nil {
		http.Error(w, "load repository report", http.StatusInternalServerError)
		return
	}
	data := struct {
		Report store.RepositoryReport
		Token  string
	}{Report: report, Token: r.URL.Query().Get("token")}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = adminRepoTemplate.Execute(w, data)
}

func (s *Server) handleRepositoriesAPI(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	repos, err := s.store.ListRepositorySummaries(r.Context(), 100)
	if err != nil {
		http.Error(w, "load repositories", http.StatusInternalServerError)
		return
	}
	writeJSON(w, repos)
}

func (s *Server) handleRepositoryAPI(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	fullName := strings.TrimSpace(r.URL.Query().Get("full_name"))
	if fullName == "" {
		http.Error(w, "full_name is required", http.StatusBadRequest)
		return
	}
	report, err := s.store.RepositoryReport(r.Context(), fullName, 100)
	if err != nil {
		http.Error(w, "load repository report", http.StatusInternalServerError)
		return
	}
	writeJSON(w, report)
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.AdminToken == "" {
		http.NotFound(w, r)
		return false
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		got = r.URL.Query().Get("token")
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.AdminToken)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}

func renderMetrics(metrics store.MetricsSnapshot) string {
	var b strings.Builder
	writeMetricMap(&b, "pr_go_webhook_deliveries", "status", metrics.DeliveriesByStatus)
	writeMetricMap(&b, "pr_go_webhook_jobs", "status", metrics.JobsByStatus)
	writeMetricMap(&b, "pr_go_review_runs", "status", metrics.ReviewRunsByStatus)
	writeMetricMap(&b, "pr_go_approval_checks", "result", metrics.ApprovalChecksByResult)
	fmt.Fprintf(&b, "pr_go_repositories_total %d\n", metrics.TotalRepositories)
	fmt.Fprintf(&b, "pr_go_pull_requests_total %d\n", metrics.TotalPullRequests)
	fmt.Fprintf(&b, "pr_go_open_findings_total %d\n", metrics.TotalOpenFindings)
	return b.String()
}

func writeMetricMap(b *strings.Builder, name string, label string, values map[string]int) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(b, "%s{%s=%q} %d\n", name, label, key, values[key])
	}
}

var adminHomeTemplate = template.Must(template.New("admin-home").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>PR Approval Agent</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 32px; color: #17202a; }
    table { border-collapse: collapse; width: 100%; margin-top: 18px; }
    th, td { border-bottom: 1px solid #d8dee4; padding: 10px; text-align: left; }
    th { background: #f6f8fa; }
    a { color: #0969da; text-decoration: none; }
  </style>
</head>
<body>
  <h1>PR Approval Agent</h1>
  <p>Repositories with recorded activity.</p>
  <table>
    <thead><tr><th>Repository</th><th>Installation</th><th>Open PRs</th><th>High Risk PRs</th><th>Last Activity</th></tr></thead>
    <tbody>
      {{range .Repositories}}
      <tr>
        <td><a href="/admin/repo?full_name={{.FullName | urlquery}}{{if $.Token}}&token={{$.Token | urlquery}}{{end}}">{{.FullName}}</a></td>
        <td>{{.InstallationID}}</td>
        <td>{{.OpenPRs}}</td>
        <td>{{.HighRiskPRs}}</td>
        <td>{{.LastActivity}}</td>
      </tr>
      {{else}}
      <tr><td colspan="5">No repositories recorded yet.</td></tr>
      {{end}}
    </tbody>
  </table>
</body>
</html>`))

var adminRepoTemplate = template.Must(template.New("admin-repo").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>{{.Report.RepositoryFullName}} - PR Approval Agent</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 32px; color: #17202a; }
    table { border-collapse: collapse; width: 100%; margin: 18px 0 32px; }
    th, td { border-bottom: 1px solid #d8dee4; padding: 10px; text-align: left; vertical-align: top; }
    th { background: #f6f8fa; }
    code { background: #f6f8fa; padding: 2px 4px; border-radius: 4px; }
    a { color: #0969da; text-decoration: none; }
  </style>
</head>
<body>
  <p><a href="/admin{{if .Token}}?token={{.Token | urlquery}}{{end}}">Back to repositories</a></p>
  <h1>{{.Report.RepositoryFullName}}</h1>
  <h2>Risk Distribution</h2>
  <table><thead><tr><th>Level</th><th>Count</th></tr></thead><tbody>
    {{range .Report.RiskDistribution}}<tr><td>{{.Level}}</td><td>{{.Count}}</td></tr>{{else}}<tr><td colspan="2">No risk scores yet.</td></tr>{{end}}
  </tbody></table>
  <h2>Pull Requests</h2>
  <table><thead><tr><th>PR</th><th>Title</th><th>Author</th><th>Status</th><th>Risk</th><th>Updated</th></tr></thead><tbody>
    {{range .Report.PullRequests}}<tr><td>#{{.Number}}</td><td>{{.Title}}</td><td>{{.AuthorLogin}}</td><td>{{.ApprovalStatus}}</td><td>{{.RiskLevel}} {{.RiskScore}}</td><td>{{.UpdatedAt}}</td></tr>{{else}}<tr><td colspan="6">No pull requests yet.</td></tr>{{end}}
  </tbody></table>
  <h2>Recent Findings</h2>
  <table><thead><tr><th>PR</th><th>ID</th><th>Severity</th><th>Status</th><th>File</th><th>Title</th></tr></thead><tbody>
    {{range .Report.Findings}}<tr><td>#{{.PRNumber}}</td><td><code>{{.FindingID}}</code></td><td>{{.Severity}}</td><td>{{.Status}}</td><td>{{.FilePath}}</td><td>{{.Title}}</td></tr>{{else}}<tr><td colspan="6">No findings yet.</td></tr>{{end}}
  </tbody></table>
  <h2>Approval Checks</h2>
  <table><thead><tr><th>PR</th><th>Result</th><th>Auto Approved</th><th>Triggered By</th><th>Created</th></tr></thead><tbody>
    {{range .Report.ApprovalChecks}}<tr><td>#{{.PRNumber}}</td><td>{{.Result}}</td><td>{{.AutoApproved}}</td><td>{{.TriggeredBy}}</td><td>{{.CreatedAt}}</td></tr>{{else}}<tr><td colspan="5">No approval checks yet.</td></tr>{{end}}
  </tbody></table>
  <h2>Audit Logs</h2>
  <table><thead><tr><th>PR</th><th>Actor</th><th>Action</th><th>Detail</th><th>Created</th></tr></thead><tbody>
    {{range .Report.AuditLogs}}<tr><td>{{if .PRNumber}}#{{.PRNumber}}{{end}}</td><td>{{.Actor}}</td><td>{{.Action}}</td><td><code>{{.DetailJSON}}</code></td><td>{{.CreatedAt}}</td></tr>{{else}}<tr><td colspan="5">No audit logs yet.</td></tr>{{end}}
  </tbody></table>
</body>
</html>`))
