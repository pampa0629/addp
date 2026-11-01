package api

import (
	"context"
	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"

	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type managementCheckFunc func(context.Context, int64, string) (*authorization.EngineAccessHandlingScope, error)

func (f managementCheckFunc) GetMetaScanCancellationScope(ctx context.Context, id int64, token string) (*authorization.EngineAccessHandlingScope, error) {
	return f(ctx, id, token)
}

func TestScanCancelChecksExactUserAndEngineBeforeRecordingIntent(t *testing.T) {
	for _, test := range []struct {
		name, permission, tenant, body, suffix string
		remote, want                           int
		wrongIdentity                          bool
	}{
		{name: "accepted", permission: "meta.scan_task.cancel", tenant: "7", want: 202},
		{name: "execute is not cancel", permission: "meta.scan_task.execute", tenant: "7", want: 403},
		{name: "other tenant", permission: "meta.scan_task.cancel", tenant: "8", want: 404},
		{name: "scope denied", permission: "meta.scan_task.cancel", tenant: "7", remote: 403, want: 403},
		{name: "dependency down", permission: "meta.scan_task.cancel", tenant: "7", remote: 503, want: 503},
		{name: "identity mismatch", permission: "meta.scan_task.cancel", tenant: "7", want: 403, wrongIdentity: true},
		{name: "body override", permission: "meta.scan_task.cancel", tenant: "7", body: `{"engine_id":19}`, want: 400},
		{name: "query override", permission: "meta.scan_task.cancel", tenant: "7", suffix: "?engine_id=19", want: 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := metatest.OpenMetadataDB(t)
			if err := executiontest.EnsureSQLiteStore(db); err != nil {
				t.Fatal(err)
			}
			taskID := "22"
			run := execution.TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Module: "meta", TaskType: "scan", ExecutionBoundary: "bounded", Source: "meta", SourceTaskID: &taskID, Status: "running", TriggerType: "scheduled", Attempt: 1, ExecutionConfig: commonmodels.JSONMap{"engine_id": 9, "engine_type": "postgresql", "scan_depth": "basic", "password": "must-not-leak"}}
			if err := db.Create(&run).Error; err != nil {
				t.Fatal(err)
			}
			calls := 0
			h := &Handler{executionService: service.NewScanExecutionService(db, nil, nil, nil), scanCancellationScope: managementCheckFunc(func(_ context.Context, id int64, token string) (*authorization.EngineAccessHandlingScope, error) {
				calls++
				if id != 9 || token != "addp_at_exact_user" {
					t.Fatal("lost exact target or credential")
				}
				if test.remote != 0 {
					return nil, &client.TenantAPIError{StatusCode: test.remote, Path: "must-not-leak"}
				}
				membership := int64(1)
				if test.wrongIdentity {
					membership = 2
				}
				return &authorization.EngineAccessHandlingScope{TenantID: 7, EngineID: 9, Operator: authorization.SharingFulfillmentOperator{PrincipalID: 91, MembershipID: membership, AuthorizationVersion: 1}, VerifiedAt: time.Now()}, nil
			})}
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware(), func(c *gin.Context) {
				if err := commonAuth.SetAuthContextForGin(c, authtest.NewTenantUserAuthContext(test.tenant, "91", []string{test.permission})); err != nil {
					t.Fatal(err)
				}
				c.Next()
			})
			router.POST("/executions/:execution_id/cancel", commonAuth.MustNewContextGuard("tenant"), commonAuth.MustNewPermissionGuard("meta.scan_task.cancel"), h.CancelScanExecution)
			req := httptest.NewRequest("POST", "/executions/"+run.ExecutionID+"/cancel"+test.suffix, strings.NewReader(test.body))
			req.Header.Set("Authorization", "Bearer addp_at_exact_user")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != test.want || strings.Contains(response.Body.String(), "must-not-leak") {
				t.Fatalf("response %d %s", response.Code, response.Body.String())
			}
			var after execution.TaskExecution
			if err := db.Where("execution_id=?", run.ExecutionID).Take(&after).Error; err != nil {
				t.Fatal(err)
			}
			_, intent := after.Metadata["scan_cancellation"]
			if intent != (test.want == 202) || after.Status != "running" || after.CompletedAt != nil {
				t.Fatal("wrong persisted cancellation boundary")
			}
			if test.want == 202 && calls != 1 {
				t.Fatal("missing current scope check")
			}
			if (test.want == 400 || test.want == 404 || test.permission != "meta.scan_task.cancel") && calls != 0 {
				t.Fatal("invalid request reached System")
			}
		})
	}
}
