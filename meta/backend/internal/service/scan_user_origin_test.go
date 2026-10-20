package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
)

func withMetaTestUserOrigin(ctx context.Context, tenantID, userID uint) context.Context {
	facts := authtest.NewTenantUserAuthContext(strconv.FormatUint(uint64(tenantID), 10), strconv.FormatUint(uint64(userID), 10), []string{"meta.scan_task.execute"})
	return commonExecution.WithUserOrigin(ctx, facts)
}

func TestManualScanRequiresVerifiedHumanOriginBeforeDependencies(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{"missing", context.Background()},
		{"different user", withMetaTestUserOrigin(context.Background(), 7, 33)},
		{"different tenant", withMetaTestUserOrigin(context.Background(), 8, 32)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewScanExecutionService(nil, nil, nil, nil)
			run, err := svc.CreateManualRun(tc.ctx, 7, 32, &models.ScanRequest{EngineID: 9, ScanDepth: "basic"})
			if run != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("unverified source reached dependencies: run=%v error=%v", run != nil, err)
			}
			run, err = svc.CreateTaskManualRun(tc.ctx, &models.ScanTask{ID: 1, TenantID: 7, EngineID: 9}, 32)
			if run != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("unverified task source reached dependencies: run=%v error=%v", run != nil, err)
			}
		})
	}
}

func TestManualScanPersistsCurrentUserOriginWithoutGrant(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createTaskExecutionTable(t, db)
	tenantID := uint(7)
	engines := NewEngineService(db, nil)
	engines.cacheEngine(tenantID, &commonModels.Engine{ID: 9, TenantID: &tenantID, EngineType: "s3", LifecycleState: "active"})
	svc := NewScanExecutionService(db, nil, engines, nil)
	facts := authtest.NewTenantUserAuthContext("7", "32", []string{"meta.scan_task.execute"})
	membership := "53"
	facts.Context.TenantMembershipID = &membership
	facts.Authorization.AuthorizationVersion = "17"
	ctx := commonExecution.WithUserOrigin(context.Background(), facts)
	run, err := svc.CreateManualRun(ctx, tenantID, 32, &models.ScanRequest{EngineID: 9, ScanDepth: "basic"})
	if err != nil {
		t.Fatal(err)
	}
	var stored commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", run.ExecutionID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.TriggeredBy == nil || *stored.TriggeredBy != 32 || stored.ActorPrincipalID == nil || *stored.ActorPrincipalID != 32 ||
		stored.ActorTenantMembershipID == nil || *stored.ActorTenantMembershipID != 53 ||
		stored.IssuedAuthorizationVersion == nil || *stored.IssuedAuthorizationVersion != 17 {
		t.Fatal("current human source was not persisted exactly")
	}
	if stored.ExecutionAuthorizationID != nil || stored.AuthorizationExpiresAt != nil || stored.ParentExecutionID != nil || stored.LeaseToken != nil {
		t.Fatal("human provenance manufactured authorization, parent or lease")
	}
}
