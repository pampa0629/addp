package service

import (
	"context"
	"encoding/json"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/scanresource"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"testing"
	"time"
)

// Runs inside the existing owner-facts PG transaction. No source credentials,
// external NFS traffic or ACL fixture is needed to verify first-item evidence.
func exerciseUploadIdentificationFacts(t *testing.T, db *gorm.DB) {
	t.Run("uploaded logical item requires every committed new member", func(t *testing.T) {
		for index, format := range []string{"stl", "csv", "3ds"} {
			t.Run(format, func(t *testing.T) {
				item := seedFileAuthorizationFacts(t, db, uint(88000+index), format)
				id := uuid.NewString()
				receipt := client.UploadReceipt{SchemaVersion: client.UploadReceiptSchema, UploadID: id, TenantID: 7, EngineID: item.EngineID, EngineVersion: "1", Uploader: client.UploadUserOrigin{PrincipalID: 31, MembershipID: 41, AuthorizationVersion: 1}, CommittedAt: time.Now().UTC(), Writes: []plugin.ContentWriteEvidence{{Target: plugin.FileItemPath(item.EngineID, item.FullName), Disposition: "created", Committed: true, SizeBytes: 5}}}
				if format == "3ds" {
					receipt.Writes = append(receipt.Writes, plugin.ContentWriteEvidence{Target: plugin.FileItemPath(item.EngineID, "models/TEST.PNG"), Disposition: "created", Committed: true, SizeBytes: 6})
				}
				run := &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Source: "manager", TriggerType: "manual", ParentExecutionID: &id, Status: "success", CreatedAt: item.CreatedAt.Add(-time.Second), UpdatedAt: time.Now().UTC(), ExecutionConfig: commonmodels.JSONMap{}}
				run.ExecutionConfig["upload_receipt"] = receipt
				run.ExecutionConfig["upload_new_candidates"] = []string{item.Fingerprint}
				svc := &ScanExecutionService{db: db}
				result, err := svc.uploadIdentification(t.Context(), db, run, 7, id)
				if err != nil || len(result.NewItems) != 1 || result.NewItems[0].ItemFingerprint != item.Fingerprint {
					t.Fatalf("new item=%+v err=%v", result, err)
				}
				if format == "csv" && result.NewItems[0].MetadataTarget == nil {
					t.Fatal("unsupported content became readable")
				}
				if format == "stl" && result.NewItems[0].FileComposition == nil {
					t.Fatal("supported content lost complete read boundary")
				}
				receipt.Writes[len(receipt.Writes)-1].Disposition = "replaced"
				run.ExecutionConfig["upload_receipt"] = receipt
				result, err = svc.uploadIdentification(t.Context(), db, run, 7, id)
				if err != nil || len(result.NewItems) != 0 {
					t.Fatal("replacement/old composition gained ownership", err)
				}
				receipt.Writes[len(receipt.Writes)-1].Disposition = "created"
				run.ExecutionConfig["upload_receipt"] = receipt
				run.CreatedAt = item.CreatedAt.Add(time.Second)
				result, err = svc.uploadIdentification(t.Context(), db, run, 7, id)
				if err != nil || len(result.NewItems) != 0 {
					t.Fatal("existing logical item gained ownership", err)
				}
				run.Status = "running"
				result, err = svc.uploadIdentification(t.Context(), db, run, 7, id)
				if err != nil || len(result.NewItems) != 0 {
					t.Fatal("unfinished scan asserted new item", err)
				}
				run.Status = "success"
				receipt.TenantID = 8
				run.ExecutionConfig["upload_receipt"] = receipt
				if _, err := svc.uploadIdentification(t.Context(), db, run, 7, id); err == nil {
					t.Fatal("foreign receipt accepted")
				}
				if _, err := json.Marshal(result); err != nil {
					t.Fatal(err)
				}
			})
		}
	})
}

type uploadReceiptReaderFunc func(context.Context, uint, string) (*client.UploadReceipt, error)

func (f uploadReceiptReaderFunc) ReadUploadReceipt(c context.Context, t uint, id string) (*client.UploadReceipt, error) {
	return f(c, t, id)
}
func exerciseUploadLeaseScope(t *testing.T, db *gorm.DB) {
	t.Run("upload identification rechecks exact owner and active worker lease", func(t *testing.T) {
		target := plugin.FileItemPath(90001, "uploads/new.csv")
		id := uuid.NewString()
		tenant := uint(7)
		receipt := client.UploadReceipt{SchemaVersion: client.UploadReceiptSchema, UploadID: id, TenantID: 7, EngineID: 90001, EngineVersion: "3", Uploader: client.UploadUserOrigin{PrincipalID: 31, MembershipID: 41, AuthorizationVersion: 1}, CommittedAt: time.Now().UTC(), Writes: []plugin.ContentWriteEvidence{{Target: target, Disposition: "created", Committed: true, SizeBytes: 4}}}
		actor, member, version := int64(31), int64(41), int64(1)
		token, owner := uuid.NewString(), "meta-upload-test"
		expiry := time.Now().UTC().Add(time.Minute)
		run := &execution.TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Module: "meta", TaskType: "scan", Status: "running", ExecutionBoundary: "bounded", Source: "manager", TriggerType: "manual", ParentExecutionID: &id, ActorPrincipalID: &actor, ActorTenantMembershipID: &member, IssuedAuthorizationVersion: &version, Attempt: 1, LeaseToken: &token, LeaseOwner: &owner, LeaseExpiresAt: &expiry, ExecutionConfig: commonmodels.JSONMap{"scan_depth": "basic", "upload_receipt": receipt}}
		if err := db.Create(run).Error; err != nil {
			t.Fatal(err)
		}
		calls := 0
		svc := (&ScanExecutionService{db: db}).WithUploadReceiptReader(uploadReceiptReaderFunc(func(context.Context, uint, string) (*client.UploadReceipt, error) {
			calls++
			r := receipt
			return &r, nil
		}))
		lease := execution.Lease{ExecutionID: run.ExecutionID, TenantID: 7, Attempt: 1, Token: token, Owner: owner}
		ctx, err := svc.uploadReadContext(t.Context(), run, lease)
		if err != nil {
			t.Fatal(err)
		}
		engine := &commonmodels.Engine{ID: 90001, TenantID: &tenant, EngineType: "nfs", Version: 3}
		if err := scanresource.CheckCollectionRead(ctx, engine); err != nil {
			t.Fatal(err)
		}
		if err := scanresource.CheckRefreshContent(ctx, target); err != nil {
			t.Fatal(err)
		}
		if err := scanresource.CheckRefreshEnumeration(ctx, plugin.FileDirectoryPath(90001, "uploads"), false); err == nil {
			t.Fatal("upload identification enumerated parent")
		}
		wrong := *engine
		wrong.ID++
		if err := scanresource.CheckCollectionRead(ctx, &wrong); err == nil {
			t.Fatal("upload admitted another engine")
		}
		if err := db.Model(run).Update("actor_tenant_membership_id", 42).Error; err != nil {
			t.Fatal(err)
		}
		if err := scanresource.CheckCollectionRead(ctx, engine); err == nil {
			t.Fatal("changed actor crossed next source read")
		}
		if err := db.Model(run).Updates(map[string]any{"actor_tenant_membership_id": 41, "lease_expires_at": time.Now().UTC().Add(-time.Second)}).Error; err != nil {
			t.Fatal(err)
		}
		if err := scanresource.CheckCollectionRead(ctx, engine); err == nil {
			t.Fatal("expired lease crossed next source read")
		}
		if calls != 1 {
			t.Fatal("unchanged committed receipt was repeatedly fetched for every file")
		}
	})
}
