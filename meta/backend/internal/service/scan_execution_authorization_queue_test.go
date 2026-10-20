package service

import (
	"context"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/scantask"
)

// Publication is only a queue barrier. A claimed execution must still consume
// its current System authorization before any source I/O.
func TestMetaPendingScanAwaitsExecutionAuthorizationPublication(t *testing.T) {
	for _, depth := range []string{"basic", "deep"} {
		t.Run(depth, func(t *testing.T) {
			db := openObjectCatalogScanTestDB(t)
			createTaskExecutionTable(t, db)
			now := time.Now().UTC()
			run := scantask.NewManualExecution(7, 32, 9, 0, "s3", nil, nil, nil, depth, false, commonExecution.ModuleMeta, now)
			principal, membership, version := int64(32), int64(43), int64(1)
			run.ActorPrincipalID = &principal
			run.ActorTenantMembershipID = &membership
			run.IssuedAuthorizationVersion = &version
			if err := db.Create(run).Error; err != nil {
				t.Fatalf("create pending scan: %v", err)
			}

			svc := NewScanExecutionService(db, nil, nil, nil)
			claimed, lease, err := svc.ClaimNextBoundedExecution(context.Background(), "meta-authorization-publication-test", now, time.Minute)
			if err != nil || claimed != nil || lease != nil {
				t.Fatalf("unpublished authorization was claimable: execution=%v lease=%v error=%v", claimed != nil, lease != nil, err)
			}
			var pending commonExecution.TaskExecution
			if err := db.Where("execution_id = ?", run.ExecutionID).First(&pending).Error; err != nil {
				t.Fatal(err)
			}
			if pending.Status != commonExecution.ExecutionStatusPending || pending.Attempt != 0 || pending.StartedAt != nil || pending.LeaseToken != nil {
				t.Fatal("waiting for authorization changed execution state or acquired a lease")
			}

			// Synthetic authorization facts exercise publication only; this does
			// not assert that an ID grants source access or skip System checks.
			authorizationID := int64(19)
			expires := now.Add(time.Minute)
			updated := db.Model(&commonExecution.TaskExecution{}).
				Where("execution_id = ? AND status = ? AND attempt = 0", run.ExecutionID, commonExecution.ExecutionStatusPending).
				Updates(map[string]interface{}{"execution_authorization_id": authorizationID, "authorization_expires_at": expires})
			if updated.Error != nil || updated.RowsAffected != 1 {
				t.Fatalf("publish authorization: rows=%d error=%v", updated.RowsAffected, updated.Error)
			}
			claimed, lease, err = svc.ClaimNextBoundedExecution(context.Background(), "meta-authorization-publication-test", now, time.Minute)
			if err != nil || claimed == nil || lease == nil {
				t.Fatalf("published scan was not claimable: execution=%v lease=%v error=%v", claimed != nil, lease != nil, err)
			}
			if claimed.ExecutionID != run.ExecutionID || claimed.Attempt != 1 || claimed.Status != commonExecution.ExecutionStatusRunning ||
				claimed.ExecutionAuthorizationID == nil || *claimed.ExecutionAuthorizationID != authorizationID || lease.Token == "" {
				t.Fatal("claim did not preserve the original execution and published authorization")
			}
		})
	}
}
