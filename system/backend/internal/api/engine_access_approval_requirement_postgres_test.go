package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Reuses the migrated, cleaned System owner fixture; no new database or source
// connection. The enclosing AgainstPostgres test is selected by the IAM gate.
func exerciseApprovalRequirementAPI(t *testing.T, db *gorm.DB, identity *iam.Repository, service *engineaccess.Service, actor engineaccess.Actor, engine models.Engine, otherTenantID int64) {
	t.Run("approval requirement production API", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		permissions := []string{"system.engine_access_approval_requirement.initialize", "system.engine_access_approval_requirement.read"}
		var defaults int64
		if err := db.Raw(`SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id JOIN system.roles r ON r.id=rp.role_id WHERE p.permission_key LIKE 'system.engine_access_approval_requirement.%' AND r.role_key <> 'tenant.source_data_authorizer'`).Scan(&defaults).Error; err != nil || defaults != 0 {
			t.Fatalf("implicit default grant=%d err=%v", defaults, err)
		}
		roles := iam.NewTenantRoleService(identity, time.Now)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: actor.TenantID, RoleKey: "custom.source_governance", Name: "Source governance fixture",
			ScopeTypes: []string{"tenant"}, PermissionKeys: permissions, ActorPrincipalID: actor.PrincipalID})
		if err != nil {
			t.Fatal(err)
		}
		stepUp := time.Now().Add(time.Minute)
		assignments, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: actor.TenantID, MembershipID: actor.MembershipID,
			RoleIDs: []int64{role.ID}, ScopeType: "tenant", Reason: "Explicit fixture governance", ActorPrincipalID: actor.PrincipalID,
			AssuranceLevel: iam.AssuranceLevelAAL2, StepUpExpiresAt: &stepUp})
		if err != nil {
			t.Fatal(err)
		}
		refreshActor := func() {
			t.Helper()
			current, err := identity.GetPrincipal(ctx, actor.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			actor.AuthorizationVersion = current.AuthorizationVersion
		}
		refreshActor()
		target := engineplugin.TabularItemPath(engine.ID, "schema", "public", "governance.target")
		input := engineaccess.InitializeApprovalRequirementInput{Actor: actor, EngineID: int64(engine.ID), CatalogPath: target, Mode: "catalog", Reason: "Explicit Catalog governance"}
		if _, err := service.InitializeApprovalRequirement(ctx, input); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("permission without delegation=%v", err)
		}
		delegation, err := service.Create(ctx, engineaccess.CreateInput{Actor: actor, EngineID: int64(engine.ID), TenantMembershipID: actor.MembershipID,
			ExpiresAt: time.Now().Add(time.Hour), Reason: "Explicit fixture target administration"})
		if err != nil {
			t.Fatal(err)
		}
		refreshActor()
		input.Actor = actor
		projection := testIAMActorContext("tenant")
		tenant, member := strconv.FormatInt(actor.TenantID, 10), strconv.FormatInt(actor.MembershipID, 10)
		projection.Principal.ID = strconv.FormatInt(actor.PrincipalID, 10)
		projection.Context.TenantID, projection.Context.TenantMembershipID = &tenant, &member
		projection.Authorization.AuthorizationVersion = strconv.FormatInt(actor.AuthorizationVersion, 10)
		projection.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: strconv.FormatInt(assignments[0].ID, 10), RoleKey: role.RoleKey,
			Scope: commonauth.AssignmentScope{Type: "tenant", TenantID: &tenant}, SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Permissions: permissions}}
		router := approvalRequirementTestRouter(t, service, &projection)
		path := fmt.Sprintf("/api/v1/system/engines/%d/access_approval_requirements", engine.ID)
		response := engineDelegationTestRequest(t, router, "POST", path, map[string]any{"catalog_path": target, "mode": "catalog", "reason": input.Reason}, 201)
		var initialized engineaccess.ApprovalRequirementView
		if err := json.Unmarshal(response.Body.Bytes(), &initialized); err != nil || initialized.ID == uuid.Nil || initialized.Mode != "catalog" || initialized.Version != 1 {
			t.Fatalf("initialized=%+v err=%v", initialized, err)
		}
		engineDelegationTestRequest(t, router, "POST", path, map[string]any{"catalog_path": target, "mode": "independent", "reason": "Overwrite"}, 409)
		engineDelegationTestRequest(t, router, "GET", path+"/"+initialized.ID.String(), nil, 200)
		projection.Authorization.RoleAssignments[0].Permissions = permissions[1:]
		engineDelegationTestRequest(t, router, "GET", path, nil, 200)
		engineDelegationTestRequest(t, router, "POST", path, map[string]any{"catalog_path": target, "mode": "catalog", "reason": input.Reason}, 403)
		projection.Authorization.RoleAssignments[0].Permissions = permissions[:1]
		engineDelegationTestRequest(t, router, "GET", path, nil, 403)
		projection.Authorization.RoleAssignments[0].Permissions = permissions
		engineDelegationTestRequest(t, router, "GET", path+"/"+uuid.NewString(), nil, 404)
		list := engineDelegationTestRequest(t, router, "GET", path, nil, 200)
		var page struct {
			Data  []engineaccess.ApprovalRequirementView `json:"data"`
			Total int64                                  `json:"total"`
		}
		if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil || page.Total != 1 || len(page.Data) != 1 {
			t.Fatalf("page=%+v err=%v", page, err)
		}
		wrong := input
		wrong.CatalogPath.EngineID++
		if _, err := service.InitializeApprovalRequirement(ctx, wrong); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("path engine mismatch=%v", err)
		}
		missing := input
		missing.EngineID++
		missing.CatalogPath.EngineID++
		if _, err := service.InitializeApprovalRequirement(ctx, missing); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatalf("other engine=%v", err)
		}
		otherTenant := uint(otherTenantID)
		foreignEngine := models.Engine{Name: "Other tenant governance", EngineType: "postgresql", TenantID: &otherTenant, LifecycleState: "active",
			ConnectionInfo: models.ConnectionInfo{}, IdentityKey: models.JSONString(`{"host":"other-governance.invalid","database":"fixture"}`)}
		if err := db.Table("system.engines").Create(&foreignEngine).Error; err != nil {
			t.Fatal(err)
		}
		foreign := input
		foreign.EngineID = int64(foreignEngine.ID)
		foreign.CatalogPath = engineplugin.TabularItemPath(foreignEngine.ID, "schema", "public", "governance.target")
		if _, err := service.InitializeApprovalRequirement(ctx, foreign); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatalf("cross tenant initialization=%v", err)
		}
		if _, err := service.GetApprovalRequirement(ctx, actor, foreign.EngineID, initialized.ID); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatalf("cross tenant read=%v", err)
		}
		stale := input
		stale.Actor.AuthorizationVersion--
		if _, err := service.InitializeApprovalRequirement(ctx, stale); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("stale actor=%v", err)
		}
		expired := input
		expired.Actor.TokenExpiresAt = time.Now().Add(-time.Minute)
		if _, err := service.InitializeApprovalRequirement(ctx, expired); !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatalf("expired token=%v", err)
		}
		exerciseApprovalRequirementLockExpiry(t, db, service, input)
		// Distinct exact targets do not inherit their neighbour's requirement;
		// concurrent initialization of one target commits one fact/audit only.
		concurrent := input
		concurrent.CatalogPath = engineplugin.TabularItemPath(engine.ID, "schema", "other", "governance.target")
		outcomes := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := service.InitializeApprovalRequirement(ctx, concurrent)
				outcomes <- err
			}()
		}
		wg.Wait()
		close(outcomes)
		successes, conflicts := 0, 0
		for err := range outcomes {
			if err == nil {
				successes++
			} else if errors.Is(err, engineaccess.ErrApprovalRequirementExists) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("concurrent %d successes %d conflicts", successes, conflicts)
		}
		var facts, audits int64
		if err := db.Table("system.engine_access_approval_requirements").Where("tenant_id=?", actor.TenantID).Count(&facts).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.audit_logs").Where("event_name='system.engine_access_approval_requirement.changed'").Count(&audits).Error; err != nil {
			t.Fatal(err)
		}
		if facts != 2 || audits != 2 {
			t.Fatalf("facts=%d audits=%d", facts, audits)
		}
		// Failure of the same-transaction audit leaves no requirement.
		rollback := input
		rollback.CatalogPath = engineplugin.TabularItemPath(engine.ID, "schema", "public", "audit_failure")
		invalidStatus := 99
		rollback.Audit.HTTPStatus = &invalidStatus
		if _, err := service.InitializeApprovalRequirement(ctx, rollback); err == nil {
			t.Fatal("audit failure committed requirement")
		}
		if err := db.Table("system.engine_access_approval_requirements").Where("tenant_id=?", actor.TenantID).Count(&facts).Error; err != nil {
			t.Fatal(err)
		}
		if facts != 2 {
			t.Fatal("audit failure left a partial requirement")
		}
		// No Catalog connection or source access is involved in first independent
		// configuration. The same authority writes an arrangement, never a Grant.
		var grantsBefore, grantsAfter int64
		if err := db.Table("system.engine_access_grants").Count(&grantsBefore).Error; err != nil {
			t.Fatal(err)
		}
		independentTarget := engineplugin.TabularItemPath(engine.ID, "schema", "public", "independent.target")
		response = engineDelegationTestRequest(t, router, "POST", path, map[string]any{"catalog_path": independentTarget, "mode": "independent", "reason": "No Catalog approval required"}, 201)
		var independent engineaccess.ApprovalRequirementView
		if err := json.Unmarshal(response.Body.Bytes(), &independent); err != nil || independent.Mode != "independent" || independent.Version != 1 {
			t.Fatalf("independent configuration=%+v err=%v", independent, err)
		}
		engineDelegationTestRequest(t, router, "POST", path, map[string]any{"catalog_path": independentTarget, "mode": "catalog", "reason": "Must not replace independent mode"}, 409)
		engineDelegationTestRequest(t, router, "POST", path, map[string]any{"catalog_path": independentTarget, "reason": "Missing explicit mode"}, 400)
		if err := db.Table("system.engine_access_grants").Count(&grantsAfter).Error; err != nil || grantsAfter != grantsBefore {
			t.Fatalf("configuration created Grant: before=%d after=%d err=%v", grantsBefore, grantsAfter, err)
		}
		var details map[string]any
		var auditDetails string
		if err := db.Table("system.audit_logs").Select("details").Where("event_name=? AND entity_id=?", "system.engine_access_approval_requirement.changed", independent.ID.String()).Scan(&auditDetails).Error; err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(auditDetails), &details); err != nil || details["mode"] != "independent" || details["successor_principal_id"] != nil {
			t.Fatalf("first configuration invented a handoff successor: %v err=%v", details, err)
		}
		if _, err := service.Revoke(ctx, engineaccess.RevokeInput{Actor: actor, EngineID: int64(engine.ID), ID: delegation.ID, Version: delegation.Version,
			Reason: "End fixture administration"}); err != nil {
			t.Fatal(err)
		}
		refreshActor()
		input.Actor = actor
		if _, err := service.InitializeApprovalRequirement(ctx, input); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("revoked management delegation=%v", err)
		}
		if _, _, err := service.ListApprovalRequirements(ctx, actor, int64(engine.ID), 1, 20); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("revoked management delegation read=%v", err)
		}
		delegation, err = service.Create(ctx, engineaccess.CreateInput{Actor: actor, EngineID: int64(engine.ID), TenantMembershipID: actor.MembershipID,
			ExpiresAt: time.Now().Add(time.Hour), Reason: "Separate functional revocation fixture"})
		if err != nil {
			t.Fatal(err)
		}
		// A forged/stale projection cannot replace current IAM permission.
		if _, err := roles.RevokeAssignment(ctx, iam.RevokeTenantRoleAssignmentInput{TenantID: actor.TenantID, AssignmentID: assignments[0].ID,
			ActorPrincipalID: actor.PrincipalID, Reason: "End governance fixture"}); err != nil {
			t.Fatal(err)
		}
		refreshActor()
		input.Actor = actor
		if _, err := service.InitializeApprovalRequirement(ctx, input); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("revoked functional permission=%v", err)
		}
		if _, _, err := service.ListApprovalRequirements(ctx, actor, int64(engine.ID), 1, 20); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("revoked functional read permission=%v", err)
		}
		if _, err := service.Revoke(ctx, engineaccess.RevokeInput{Actor: actor, EngineID: int64(engine.ID), ID: delegation.ID, Version: delegation.Version,
			Reason: "End functional revocation fixture"}); err != nil {
			t.Fatal(err)
		}
	})
}

type approvalRequirementWaitContextKey struct{}

// Wait on the real target advisory lock, not a service mock or host sleep.
// Qualification is valid before waiting and expires before that lock is freed.
func exerciseApprovalRequirementLockExpiry(t *testing.T, db *gorm.DB, service *engineaccess.Service, input engineaccess.InitializeApprovalRequirementInput) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	input.CatalogPath = engineplugin.TabularItemPath(uint(input.EngineID), "schema", "public", "expired_while_waiting")
	path, err := commonauth.EncodeSharingTarget(input.CatalogPath)
	if err != nil {
		t.Fatal(err)
	}
	locked, release, holderDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	go func() {
		holderDone <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			key := fmt.Sprintf("engine_access_target|%d|%s", input.Actor.TenantID, path)
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
				return err
			}
			close(locked)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-locked:
	case err := <-holderDone:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	pid := make(chan int64, 1)
	const callback = "test:approval_requirement_target_wait"
	if err := db.Callback().Raw().Before("gorm:raw").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(approvalRequirementWaitContextKey{}) != true || !strings.Contains(tx.Statement.SQL.String(), "pg_advisory_xact_lock") {
			return
		}
		var backendPID int64
		if err := tx.Statement.ConnPool.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&backendPID); err != nil {
			tx.AddError(err)
			return
		}
		pid <- backendPID
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Raw().Remove(callback)
	var now time.Time
	if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
		t.Fatal(err)
	}
	input.Actor.TokenExpiresAt = now.Add(time.Second)
	done := make(chan error, 1)
	go func() {
		_, err := service.InitializeApprovalRequirement(context.WithValue(ctx, approvalRequirementWaitContextKey{}, true), input)
		done <- err
	}()
	var backendPID int64
	select {
	case backendPID = <-pid:
	case err := <-done:
		t.Fatalf("qualification failed before target wait: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for {
		var waiting, expired bool
		if err := db.WithContext(ctx).Raw(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=? AND locktype='advisory' AND NOT granted), clock_timestamp() >= ?`, backendPID, input.Actor.TokenExpiresAt).Row().Scan(&waiting, &expired); err != nil {
			t.Fatal(err)
		}
		if waiting && expired {
			break
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("target wait/expiry was not observed")
		}
	}
	unblock()
	if err := <-holderDone; err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, commonapi.ErrUnauthorized) {
		t.Fatalf("expired while target locked=%v", err)
	}
	var count int64
	if err := db.Table("system.engine_access_approval_requirements").Where("tenant_id=? AND catalog_path=?::jsonb", input.Actor.TenantID, string(path)).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("expiry committed requirement: count=%d err=%v", count, err)
	}
}
