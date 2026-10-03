package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"gorm.io/gorm"
)

// Called by the existing AgainstPostgres owner fixture and CI gate.
func exerciseHandlingScopeAPI(t *testing.T, db *gorm.DB, identity *iam.Repository, service *engineaccess.Service, actor engineaccess.Actor, engine models.Engine, otherTenantID int64) {
	t.Run("independent human source handling scope", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		refresh := func() {
			t.Helper()
			p, err := identity.GetPrincipal(ctx, actor.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			actor.AuthorizationVersion = p.AuthorizationVersion
		}
		refresh()
		if _, err := service.GetHandlingScope(ctx, actor, int64(engine.ID)); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("existing governance permission implied handling: %v", err)
		}
		var implicit int64
		if err := db.Raw(`SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='system.engine_access_fulfillment.create'`).Scan(&implicit).Error; err != nil || implicit != 0 {
			t.Fatalf("implicit handling grants=%d %v", implicit, err)
		}
		roles := iam.NewTenantRoleService(identity, time.Now)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: actor.TenantID, RoleKey: "custom.source_handler", Name: "Source handling", ScopeTypes: []string{"tenant"}, PermissionKeys: []string{"system.engine_access_fulfillment.create"}, ActorPrincipalID: actor.PrincipalID})
		if err != nil {
			t.Fatal(err)
		}
		stepUp := time.Now().Add(time.Minute)
		assignments, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: actor.TenantID, MembershipID: actor.MembershipID, RoleIDs: []int64{role.ID}, ScopeType: "tenant", Reason: "Explicit source handler", ActorPrincipalID: actor.PrincipalID, AssuranceLevel: iam.AssuranceLevelAAL2, StepUpExpiresAt: &stepUp})
		if err != nil {
			t.Fatal(err)
		}
		refresh()
		target := engineplugin.TabularItemPath(engine.ID, "schema", "public", "governance.target")
		if _, err := service.GetHandlingScope(ctx, actor, int64(engine.ID)); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("permission without delegation=%v", err)
		}
		if _, err := service.GetHandlingRequirement(ctx, actor, target); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("observation permission without delegation=%v", err)
		}
		delegation, err := service.Create(ctx, engineaccess.CreateInput{Actor: actor, EngineID: int64(engine.ID), TenantMembershipID: actor.MembershipID, ExpiresAt: time.Now().Add(time.Hour), Reason: "Explicit handler management scope"})
		if err != nil {
			t.Fatal(err)
		}
		refresh()
		view, err := service.GetHandlingScope(ctx, actor, int64(engine.ID))
		if err != nil || view.Operator.PrincipalID != actor.PrincipalID || view.Operator.AuthorizationVersion != actor.AuthorizationVersion || view.EngineID != int64(engine.ID) || view.VerifiedAt.IsZero() {
			t.Fatalf("scope=%+v err=%v", view, err)
		}
		// The prior owner fixture initialized this exact target, then revoked its
		// governance role. Handling must not imply engine configuration reading.
		if _, _, err := service.ListApprovalRequirements(ctx, actor, int64(engine.ID), 1, 20); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("handling expanded to configuration enumeration: %v", err)
		}
		var beforeFacts, beforeAudits int64
		if err := db.Table("system.engine_access_approval_requirements").Count(&beforeFacts).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.audit_logs").Count(&beforeAudits).Error; err != nil {
			t.Fatal(err)
		}
		requirement, err := service.GetHandlingRequirement(ctx, actor, target)
		if err != nil || requirement.Mode != "catalog" || requirement.RequirementVersion != 1 {
			t.Fatalf("exact observation=%+v err=%v", requirement, err)
		}
		neighbour := engineplugin.TabularItemPath(engine.ID, "schema", "other", "unknown.target")
		if _, err := service.GetHandlingRequirement(ctx, actor, neighbour); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatalf("neighbour inherited or initialized a requirement: %v", err)
		}
		var afterFacts, afterAudits int64
		if err := db.Table("system.engine_access_approval_requirements").Count(&afterFacts).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.audit_logs").Count(&afterAudits).Error; err != nil {
			t.Fatal(err)
		}
		if beforeFacts != afterFacts || beforeAudits != afterAudits {
			t.Fatal("observation wrote a requirement or governance audit")
		}
		auth := testIAMActorContext("tenant")
		auth.Principal.ID = fmt.Sprint(actor.PrincipalID)
		tenant, member := fmt.Sprint(actor.TenantID), fmt.Sprint(actor.MembershipID)
		auth.Context.TenantID, auth.Context.TenantMembershipID = &tenant, &member
		auth.Authorization.AuthorizationVersion = fmt.Sprint(actor.AuthorizationVersion)
		auth.Authorization.RoleAssignments = []commonauth.RoleAssignment{{AssignmentID: "1", RoleKey: role.RoleKey, SourceType: "manual", ValidFrom: time.Now().Add(-time.Minute), Scope: commonauth.AssignmentScope{Type: "tenant", TenantID: &tenant}, Permissions: []string{"system.engine_access_fulfillment.create"}}}
		router := handlingScopeTestRouter(t, service, &auth)
		minimalRouter := approvalRequirementTestRouter(t, service, &auth)
		minimalRoute := fmt.Sprintf("/api/v1/system/engines/%d/access_handling_requirement", engine.ID)
		minimal := engineDelegationTestRequest(t, minimalRouter, "POST", minimalRoute, EngineAccessHandlingRequirementRequest{Version: target.Version, Segments: target.Segments}, 200)
		var observation map[string]any
		if err := json.Unmarshal(minimal.Body.Bytes(), &observation); err != nil || len(observation) != 2 || observation["mode"] != "catalog" || observation["requirement_version"] != "1" {
			t.Fatalf("minimal contract=%v err=%v", observation, err)
		}
		response := engineDelegationTestRequest(t, router, "GET", fmt.Sprintf("/api/v1/system/engines/%d/access_handling_scope", engine.ID), nil, 200)
		var data map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil || data["engine_id"] != fmt.Sprint(engine.ID) {
			t.Fatalf("scope contract=%v err=%v", data, err)
		}
		stale := actor
		stale.AuthorizationVersion--
		if _, err := service.GetHandlingScope(ctx, stale, int64(engine.ID)); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("stale operator=%v", err)
		}
		if _, err := service.GetHandlingRequirement(ctx, stale, target); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("stale observation=%v", err)
		}
		expired := actor
		expired.TokenExpiresAt = time.Now().Add(-time.Second)
		if _, err := service.GetHandlingScope(ctx, expired, int64(engine.ID)); !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatalf("expired token=%v", err)
		}
		if _, err := service.GetHandlingRequirement(ctx, expired, target); !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatalf("expired observation=%v", err)
		}
		other := actor
		other.TenantID = otherTenantID
		if _, err := service.GetHandlingScope(ctx, other, int64(engine.ID)); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("cross-tenant provenance=%v", err)
		}
		if _, err := service.GetHandlingRequirement(ctx, other, target); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("cross tenant observation=%v", err)
		}
		exerciseHandlingScopeLockExpiry(t, db, actor, int64(engine.ID), func(ctx context.Context, actor engineaccess.Actor) error {
			_, err := service.GetHandlingScope(ctx, actor, int64(engine.ID))
			return err
		})
		exerciseHandlingScopeLockExpiry(t, db, actor, int64(engine.ID), func(ctx context.Context, actor engineaccess.Actor) error {
			_, err := service.GetHandlingRequirement(ctx, actor, target)
			return err
		})
		if _, err := roles.RevokeAssignment(ctx, iam.RevokeTenantRoleAssignmentInput{TenantID: actor.TenantID, AssignmentID: assignments[0].ID,
			ActorPrincipalID: actor.PrincipalID, Reason: "Remove independent human handling permission"}); err != nil {
			t.Fatal(err)
		}
		refresh()
		if _, err := service.GetHandlingScope(ctx, actor, int64(engine.ID)); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("revoked handling permission=%v", err)
		}
		if _, err := service.GetHandlingRequirement(ctx, actor, target); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("revoked observation permission=%v", err)
		}
		if _, err := service.Revoke(ctx, engineaccess.RevokeInput{Actor: actor, EngineID: int64(engine.ID), ID: delegation.ID, Version: 1, Reason: "Remove handling management scope"}); err != nil {
			t.Fatal(err)
		}
		refresh()
		if _, err := service.GetHandlingScope(ctx, actor, int64(engine.ID)); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("revoked management scope=%v", err)
		}
		if _, err := service.GetHandlingRequirement(ctx, actor, target); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("revoked observation scope=%v", err)
		}
	})
}

type handlingScopeWaitContextKey struct{}

func exerciseHandlingScopeLockExpiry(t *testing.T, db *gorm.DB, actor engineaccess.Actor, engineID int64, observe func(context.Context, engineaccess.Actor) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	locked, release, holderDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	go func() {
		holderDone <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var id int64
			if err := tx.Raw("SELECT id FROM system.engines WHERE id=? FOR UPDATE", engineID).Scan(&id).Error; err != nil {
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
	const callback = "test:handling_scope_engine_wait"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(handlingScopeWaitContextKey{}) != true || tx.Statement.Table != "engines" {
			return
		}
		if locking, exists := tx.Statement.Clauses["FOR"]; !exists || !strings.Contains(fmt.Sprint(locking.Expression), "SHARE") {
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
	defer db.Callback().Query().Remove(callback)
	var now time.Time
	if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
		t.Fatal(err)
	}
	actor.TokenExpiresAt = now.Add(time.Second)
	done := make(chan error, 1)
	go func() {
		err := observe(context.WithValue(ctx, handlingScopeWaitContextKey{}, true), actor)
		done <- err
	}()
	var backendPID int64
	select {
	case backendPID = <-pid:
	case err := <-done:
		t.Fatalf("scope did not wait on engine: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for {
		var waiting, expired bool
		if err := db.WithContext(ctx).Raw(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=? AND NOT granted), clock_timestamp() >= ?`, backendPID, actor.TokenExpiresAt).Row().Scan(&waiting, &expired); err != nil {
			t.Fatal(err)
		}
		if waiting && expired {
			break
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal("engine wait and token expiry were not observed")
		}
	}
	unblock()
	if err := <-holderDone; err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, commonapi.ErrUnauthorized) {
		t.Fatalf("scope accepted expired token after engine wait: %v", err)
	}
}
