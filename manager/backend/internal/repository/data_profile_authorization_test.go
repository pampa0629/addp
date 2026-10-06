package repository

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func profileBindingFixture(t *testing.T, db *gorm.DB, tenant int) (*DataProfileExecutionRepository, *commonExecution.TaskExecution, commonExecution.ManagerProfileReadScope, *commonClient.IssuedManagerProfileAuthorization) {
	t.Helper()
	repo := NewDataProfileExecutionRepository(db)
	principal, membership, version := int64(9), int64(12), int64(3)
	set, err := plugin.NewQueryReadSet(plugin.TabularItemPath(11, "schema", "public", "C"))
	if err != nil {
		t.Fatal(err)
	}
	execution := newDataProfileRepositoryTestExecution(uuid.NewString(), time.Now().UTC())
	execution.TenantID = tenant
	execution.ExecutionBoundary = commonExecution.ExecutionBoundaryBounded
	execution.ActorPrincipalID, execution.ActorTenantMembershipID, execution.IssuedAuthorizationVersion = &principal, &membership, &version
	execution.ExecutionConfig = commonModels.JSONMap{"config_version": "data-profile-config/v6", "target_key": uuid.NewString(), "engine_id": 11, "read_set": set, "pages": []map[string]int{{"offset": 0, "limit": 10}}, "condition_value": json.Number("9007199254740993")}
	if err := db.Create(execution).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Where("tenant_id = ? AND execution_id = ?", tenant, execution.ExecutionID).Delete(&commonExecution.TaskExecution{}).Error; err != nil {
			t.Error(err)
		}
	})
	raw, err := repo.GetRawExecutionConfig(t.Context(), tenant, execution.ExecutionID)
	if err != nil || !strings.Contains(string(raw), "9007199254740993") {
		t.Fatalf("raw configuration lost integer precision: %s %v", raw, err)
	}
	scope, err := commonExecution.NewManagerProfileReadScope(raw, set)
	if err != nil {
		t.Fatal(err)
	}
	issued := &commonClient.IssuedManagerProfileAuthorization{ID: "41", TenantID: strconv.Itoa(tenant), ExecutionID: execution.ExecutionID, Audience: "manager", SourceReadScope: *scope.Clone(), ExpiresAt: time.Now().Add(time.Minute)}
	return repo, execution, *scope, issued
}

func TestDataProfileAuthorizationBindingRejectsChangedFacts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		updates map[string]interface{}
	}{
		{"actor", map[string]interface{}{"actor_principal_id": 10}},
		{"membership", map[string]interface{}{"actor_tenant_membership_id": 13}},
		{"version", map[string]interface{}{"issued_authorization_version": 4}},
		{"claimed", map[string]interface{}{"status": commonExecution.ExecutionStatusRunning, "attempt": 1}},
		{"config", map[string]interface{}{"execution_config": commonModels.JSONMap{"config_version": "data-profile-config/v6", "changed": true}}},
		{"source", map[string]interface{}{"source": "develop"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newDataProfileRepositoryTestDB(t)
			repo, e, scope, issued := profileBindingFixture(t, db, 7)
			if err := db.Model(&commonExecution.TaskExecution{}).Where("id = ?", e.ID).Updates(tc.updates).Error; err != nil {
				t.Fatal(err)
			}
			if err := repo.BindSourceAuthorization(t.Context(), e, scope, issued); !errors.Is(err, commonAPI.ErrConflict) {
				t.Fatalf("changed facts bound: %v", err)
			}
			stored, err := repo.GetByExecutionID(t.Context(), 7, e.ExecutionID)
			if err != nil || stored.ExecutionAuthorizationID != nil || stored.AuthorizationExpiresAt != nil {
				t.Fatalf("partial binding: %#v %v", stored, err)
			}
		})
	}
}

func TestDataProfileAuthorizationBindingAndUnboundCleanup(t *testing.T) {
	db := newDataProfileRepositoryTestDB(t)
	repo, e, scope, issued := profileBindingFixture(t, db, 7)
	queue := NewBoundedExecutionQueueRepository(db)
	if claimed, _, err := queue.ClaimNext(t.Context(), []string{commonExecution.TaskTypeDataProfiling}, "test", time.Now(), time.Minute); err != nil || claimed != nil {
		t.Fatalf("unbound execution claimed: %#v %v", claimed, err)
	}
	embedding := newDataProfileRepositoryTestExecution(uuid.NewString(), time.Now().UTC())
	embedding.TaskType = commonExecution.TaskTypeEmbedding
	if err := db.Create(embedding).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, _, err := queue.ClaimNext(t.Context(), []string{commonExecution.TaskTypeDataProfiling, commonExecution.TaskTypeEmbedding}, "test", time.Now(), time.Minute); err != nil || claimed == nil || claimed.ExecutionID != embedding.ExecutionID {
		t.Fatalf("unbound profile blocked another task type: %#v %v", claimed, err)
	}
	if err := repo.BindSourceAuthorization(t.Context(), e, scope, issued); err != nil {
		t.Fatal(err)
	}
	if err := repo.FailUnbound(t.Context(), e); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("bound execution closed: %v", err)
	}
	if err := repo.BindSourceAuthorization(t.Context(), e, scope, issued); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("bound execution overwritten: %v", err)
	}
	if claimed, _, err := queue.ClaimNext(t.Context(), []string{commonExecution.TaskTypeDataProfiling}, "test", time.Now(), time.Minute); err != nil || claimed == nil || claimed.ExecutionID != e.ExecutionID {
		t.Fatalf("bound execution not claimable: %#v %v", claimed, err)
	}
	_, unbound, _, _ := profileBindingFixture(t, db, 7)
	if err := repo.FailUnbound(t.Context(), unbound); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetByExecutionID(t.Context(), 7, unbound.ExecutionID)
	if err != nil || stored.Status != commonExecution.ExecutionStatusFailed || stored.CompletedAt == nil {
		t.Fatalf("unbound cleanup: %#v %v", stored, err)
	}
}

func TestDataProfileAuthorizationBindingRejectsInvalidIssuance(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*commonClient.IssuedManagerProfileAuthorization)
	}{
		{"expired", func(v *commonClient.IssuedManagerProfileAuthorization) { v.ExpiresAt = time.Now().Add(-time.Minute) }},
		{"tenant", func(v *commonClient.IssuedManagerProfileAuthorization) { v.TenantID = "8" }},
		{"audience", func(v *commonClient.IssuedManagerProfileAuthorization) { v.Audience = "system" }},
		{"execution", func(v *commonClient.IssuedManagerProfileAuthorization) { v.ExecutionID = uuid.NewString() }},
		{"reference", func(v *commonClient.IssuedManagerProfileAuthorization) { v.ID = "0" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newDataProfileRepositoryTestDB(t)
			repo, e, scope, issued := profileBindingFixture(t, db, 7)
			tc.change(issued)
			if err := repo.BindSourceAuthorization(t.Context(), e, scope, issued); !errors.Is(err, commonAPI.ErrConflict) {
				t.Fatalf("invalid issuance bound: %v", err)
			}
			stored, err := repo.GetByExecutionID(t.Context(), 7, e.ExecutionID)
			if err != nil || stored.ExecutionAuthorizationID != nil || stored.AuthorizationExpiresAt != nil {
				t.Fatalf("invalid issuance persisted: %#v %v", stored, err)
			}
		})
	}
}

func TestManagerQueueRecoversOnlyAbandonedUnboundProfiles(t *testing.T) {
	db := newDataProfileRepositoryTestDB(t)
	_, old, _, _ := profileBindingFixture(t, db, 7)
	_, fresh, _, _ := profileBindingFixture(t, db, 7)
	repo, bound, scope, issued := profileBindingFixture(t, db, 7)
	if err := repo.BindSourceAuthorization(t.Context(), bound, scope, issued); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Model(&commonExecution.TaskExecution{}).Where("id IN ?", []int64{old.ID, bound.ID}).Update("created_at", now.Add(-3*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	count, err := NewBoundedExecutionQueueRepository(db).RecoverUnleased(t.Context(), now, 100)
	if err != nil || count != 1 {
		t.Fatalf("recovered=%d %v", count, err)
	}
	for _, e := range []*commonExecution.TaskExecution{old, fresh, bound} {
		stored, err := repo.GetByExecutionID(t.Context(), 7, e.ExecutionID)
		want := commonExecution.ExecutionStatusPending
		if e == old {
			want = commonExecution.ExecutionStatusFailed
		}
		if err != nil || stored.Status != want || stored.Attempt != 0 {
			t.Fatalf("recovery changed wrong execution: %#v %v", stored, err)
		}
	}
}

func TestIntegrationPostgresManagerProfileConcurrentAuthorizationBinding(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	tenant := int(time.Now().UnixNano()%100000000 + 970000000)
	repo, e, scope, issued := profileBindingFixture(t, db, tenant)
	queue := NewBoundedExecutionQueueRepository(db)
	if claimed, _, err := queue.ClaimNext(t.Context(), []string{commonExecution.TaskTypeDataProfiling}, "binding-test", time.Now(), time.Minute); err != nil || claimed != nil {
		t.Fatalf("unbound claimed: %#v %v", claimed, err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- repo.BindSourceAuthorization(t.Context(), e, scope, issued) }()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, commonAPI.ErrConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("binding winners=%d", winners)
	}
	stored, err := repo.GetByExecutionID(t.Context(), tenant, e.ExecutionID)
	if err != nil || stored.ExecutionAuthorizationID == nil || *stored.ExecutionAuthorizationID != 41 || stored.AuthorizationExpiresAt == nil || stored.Attempt != 0 {
		t.Fatalf("invalid binding: %#v %v", stored, err)
	}
}
