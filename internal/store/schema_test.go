package store

import (
	"strings"
	"testing"
)

func TestSchemaStatementsContainV2Tables(t *testing.T) {
	joined := strings.Join(SchemaStatements(), "\n")
	for _, table := range []string{
		"github_installations",
		"repositories",
		"pull_requests",
		"webhook_deliveries",
		"review_runs",
		"review_findings",
		"risk_scores",
		"model_invocations",
		"review_comments",
		"audit_logs",
	} {
		if !strings.Contains(joined, table) {
			t.Fatalf("schema missing table %s", table)
		}
	}
}
