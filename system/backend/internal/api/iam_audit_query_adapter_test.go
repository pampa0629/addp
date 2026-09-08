package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

const auditExportRegressionTotal = 10001

type paginatedAuditExportService struct{}

func (paginatedAuditExportService) List(
	_ context.Context,
	_ iam.AuditQuery,
	page int,
	pageSize int,
) ([]iam.AuditLog, int64, error) {
	start := (page - 1) * pageSize
	end := min(start+pageSize, auditExportRegressionTotal)
	logs := make([]iam.AuditLog, 0, end-start)
	principalType := iam.PrincipalTypeServicePrincipal
	for index := start; index < end; index++ {
		logs = append(logs, iam.AuditLog{
			ID:            int64(index + 1),
			PrincipalType: &principalType,
			EventName:     "oauth.token.issued",
			Result:        iam.AuditResultSucceeded,
			RiskLevel:     iam.AuditRiskMedium,
			ModuleName:    "system",
			Details:       []byte(`{}`),
			CreatedAt:     time.Unix(int64(index), 0).UTC(),
		})
	}
	return logs, auditExportRegressionTotal, nil
}

func (paginatedAuditExportService) Get(context.Context, int64, *int64) (*iam.AuditLog, error) {
	return nil, nil
}

func (service paginatedAuditExportService) Export(
	ctx context.Context,
	query iam.AuditQuery,
	batchSize int,
	visit func([]iam.AuditLog) error,
) (int64, error) {
	var exported int64
	for page := 1; ; page++ {
		logs, _, err := service.List(ctx, query, page, batchSize)
		if err != nil {
			return exported, err
		}
		if len(logs) == 0 {
			return exported, nil
		}
		if err := visit(logs); err != nil {
			return exported, err
		}
		exported += int64(len(logs))
		if len(logs) < batchSize {
			return exported, nil
		}
	}
}

func (paginatedAuditExportService) Summary(context.Context, iam.AuditQuery) (*iam.AuditSummary, error) {
	return nil, nil
}

func (paginatedAuditExportService) Trends(context.Context, iam.AuditQuery) ([]iam.AuditTrendPoint, error) {
	return nil, nil
}

func TestAuditQueryFromRequestIncludesEntityFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(
		"GET",
		"/platform/audit/events?entity_type=cleanup_task&entity_id=cleanup-1&principal_type=user",
		nil,
	)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	query, err := auditQueryFromRequest(context, false)
	if err != nil {
		t.Fatalf("parse audit query: %v", err)
	}
	if query.EntityType != "cleanup_task" || query.EntityID != "cleanup-1" {
		t.Fatalf("audit entity filters = %q/%q", query.EntityType, query.EntityID)
	}
	if query.PrincipalType == nil || *query.PrincipalType != iam.PrincipalTypeUser {
		t.Fatalf("audit principal type = %#v", query.PrincipalType)
	}
}

func TestAuditExportDoesNotSilentlyTruncateAfterManagementPageLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, err := NewIAMAuditHandler(paginatedAuditExportService{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/platform/audit/events/export?format=csv", nil)

	handler.PlatformExport(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	records, err := csv.NewReader(recorder.Body).ReadAll()
	if err != nil {
		t.Fatalf("parse exported CSV: %v", err)
	}
	if got, want := len(records), auditExportRegressionTotal+1; got != want {
		t.Fatalf("CSV rows including header = %d, want %d", got, want)
	}
	if got, want := recorder.Header().Get("X-ADDP-Export-Count"), strconv.Itoa(auditExportRegressionTotal); got != want {
		t.Fatalf("X-ADDP-Export-Count = %q, want %q", got, want)
	}
}

func TestAuditJSONExportUsesTheSameCompleteResultSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, err := NewIAMAuditHandler(paginatedAuditExportService{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/platform/audit/events/export?format=json", nil)

	handler.PlatformExport(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var events []IAMAuditEventResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &events); err != nil {
		t.Fatalf("parse exported JSON: %v", err)
	}
	if got, want := len(events), auditExportRegressionTotal; got != want {
		t.Fatalf("JSON events = %d, want %d", got, want)
	}
	if got, want := recorder.Header().Get("X-ADDP-Export-Count"), strconv.Itoa(auditExportRegressionTotal); got != want {
		t.Fatalf("X-ADDP-Export-Count = %q, want %q", got, want)
	}
}
