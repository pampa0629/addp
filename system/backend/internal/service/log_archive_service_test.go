package service

import (
	"reflect"
	"testing"
	"time"

	"github.com/addp/system/internal/models"
)

func TestGroupAuditLogsByTenantUsesTenantZeroForPlatformLogs(t *testing.T) {
	tenantID := uint(1)
	logs := []models.AuditLog{
		{ID: 1, TenantID: nil},
		{ID: 2, TenantID: &tenantID},
		{ID: 3, TenantID: nil},
	}

	grouped := groupAuditLogsByTenant(logs)
	if len(grouped[0]) != 2 {
		t.Fatalf("tenant_0 logs = %d, want 2", len(grouped[0]))
	}
	if len(grouped[1]) != 1 {
		t.Fatalf("tenant_1 logs = %d, want 1", len(grouped[1]))
	}
}

func TestAuditLogArchiveObjectNameFollowsTenantFirstLayout(t *testing.T) {
	got := auditLogArchiveObjectName(1, "2026", "03", "2026-03-21", 10, 20)
	want := "tenant_1/audit-logs/2026/03/logs-2026-03-21-10-20.csv"
	if got != want {
		t.Fatalf("object name = %q, want %q", got, want)
	}

	got = auditLogArchiveObjectName(0, "2026", "03", "2026-03-21", 1, 9)
	want = "tenant_0/audit-logs/2026/03/logs-2026-03-21-1-9.csv"
	if got != want {
		t.Fatalf("platform object name = %q, want %q", got, want)
	}
}

func TestAuditLogArchiveCSVIncludesTraceFields(t *testing.T) {
	header := auditLogArchiveCSVHeader()
	for _, field := range []string{"request_body", "query_params", "user_agent", "error_message", "request_id"} {
		if !containsString(header, field) {
			t.Fatalf("archive header missing %q: %#v", field, header)
		}
	}

	tenantID := uint(7)
	userID := uint(9)
	row := auditLogArchiveCSVRow(models.AuditLog{
		ID:           3,
		CreatedAt:    time.Date(2026, 3, 21, 8, 9, 10, 0, time.UTC),
		UserID:       &userID,
		Username:     "alice",
		TenantID:     &tenantID,
		HTTPMethod:   "POST",
		ResourcePath: "/api/v1/system/engines",
		HTTPStatus:   201,
		DurationMs:   42,
		EntityType:   "engine",
		EntityID:     "12",
		RequestBody:  `{"name":"demo"}`,
		QueryParams:  "force=true",
		UserAgent:    "Mozilla/5.0",
		IPAddress:    "127.0.0.1",
		LogLevel:     "INFO",
		ErrorMessage: "",
		RequestID:    "req-1",
		ModuleName:   "system",
	})

	want := []string{
		"3", "2026-03-21 08:09:10", "9", "alice", "7",
		"POST", "/api/v1/system/engines", "201", "42",
		"engine", "12",
		`{"name":"demo"}`, "force=true", "Mozilla/5.0", "127.0.0.1",
		"INFO", "", "req-1", "system",
	}
	if !reflect.DeepEqual(row, want) {
		t.Fatalf("archive row = %#v, want %#v", row, want)
	}
	if len(row) != len(header) {
		t.Fatalf("row/header length = %d/%d", len(row), len(header))
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
