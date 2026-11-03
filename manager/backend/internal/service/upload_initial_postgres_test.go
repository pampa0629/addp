package service

import (
	"context"
	"encoding/json"
	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"strings"
	"testing"
	"time"
)

type uploadIdentificationStub struct {
	calls  int
	status string
	err    error
	scan   string
}

func (f *uploadIdentificationStub) EnsureUploadBasicScan(_ context.Context, _ uint, id string) (*client.MetaUploadIdentification, error) {
	f.calls++
	return &client.MetaUploadIdentification{UploadID: id, ScanExecutionID: f.scan, Status: f.status}, f.err
}
func TestIntegrationPostgresManagerUploadContinuationNeverReplaysWrites(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Manager PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(os.Getenv("MANAGER_POSTGRES_TEST_DSN")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending to issued", true: "explicit refusal stops"}[denied], func(t *testing.T) {
			identification := &uploadIdentificationStub{scan: uuid.NewString(), status: "pending"}
			grants := 0
			svc := &UploadService{db: db, uploadIdentification: identification, issueUploadGrant: func(_ context.Context, tenant uint, id, fp string) (*authorization.UploadInitialResult, error) {
				grants++
				requestID := uuid.New()
				now := time.Now().UTC()
				return &authorization.UploadInitialResult{TenantID: int64(tenant), UploadID: id, ItemFingerprint: fp, Outcome: "issued", Action: "metadata_read", RequestID: &requestID, GrantedAt: &now}, nil
			}}
			provider := &evidenceUploadProvider{disposition: "created"}
			tenant := uint(710000777)
			engine := &commonmodels.Engine{ID: 9, EngineType: "nfs", TenantID: &tenant, Version: 2}
			loc, _ := resourcetree.ParseURI("addp://engine/9/path/dir?type=directory")
			request := &UploadRequest{TenantID: tenant, TargetNodeLocator: "addp://engine/9/path/dir?type=directory", Files: []UploadFile{{FileName: "new.csv", Reader: strings.NewReader("value")}}}
			ctx := execution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("710000777", "31", []string{"manager.data_item.create"}))
			result, err := svc.uploadNFS(ctx, request, loc, engine, provider, plugin.FileDirectoryPath(9, "dir"))
			if err != nil || result.UploadID == "" {
				t.Fatalf("upload=%+v %v", result, err)
			}
			t.Cleanup(func() {
				if e := db.Where("tenant_id=? AND execution_id=? AND module=? AND task_type=?", tenant, result.UploadID, execution.ModuleManager, execution.TaskTypeUpload).Delete(&execution.TaskExecution{}).Error; e != nil {
					t.Error(e)
				}
				var count int64
				if e := db.Model(&execution.TaskExecution{}).Where("tenant_id=? AND execution_id=?", tenant, result.UploadID).Count(&count).Error; e != nil || count != 0 {
					t.Errorf("upload cleanup=%d err=%v", count, e)
				}
			})
			run, err := svc.GetOwnUpload(t.Context(), tenant, 31, result.UploadID)
			if err != nil || run.Status != "success" {
				t.Fatal(err)
			}
			identification.err = &client.TenantAPIError{StatusCode: 503, ErrorCode: "upload_scan_unavailable"}
			svc.continueUploads(t.Context())
			if provider.calls != 1 || grants != 0 {
				t.Fatal("temporary outage replayed source write or granted")
			}
			identification.err = nil
			svc.continueUploads(t.Context())
			run, err = svc.GetOwnUpload(t.Context(), tenant, 31, result.UploadID)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(run.Metadata["upload_initial"])
			if !strings.Contains(string(raw), identification.scan) {
				t.Fatal("pending identification reference lost")
			}
			if denied {
				identification.err = &client.TenantAPIError{StatusCode: 403}
			} else {
				identification.status = "success"
			}
			svc.continueUploads(t.Context())
			calls := identification.calls
			svc.continueUploads(t.Context())
			run, err = svc.GetOwnUpload(t.Context(), tenant, 31, result.UploadID)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ = json.Marshal(run.Metadata["upload_initial"])
			want := "settled"
			wantGrants := 1
			if denied {
				want = "denied"
				wantGrants = 0
			}
			if !strings.Contains(string(raw), `"status":"`+want+`"`) || run.Status != "success" || provider.calls != 1 || grants != wantGrants || identification.calls != calls {
				t.Fatalf("state=%s physical=%s writes=%d grants=%d calls=%d/%d", raw, run.Status, provider.calls, grants, identification.calls, calls)
			}
			receipt, err := svc.ReadUploadReceipt(t.Context(), tenant, result.UploadID)
			if err != nil || receipt.Writes[0].SizeBytes != 5 {
				t.Fatal("continuation changed immutable write fact", err)
			}
		})
	}
}
