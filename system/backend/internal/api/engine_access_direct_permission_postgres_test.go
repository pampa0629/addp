package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type identityReaderFunc func(context.Context, uint, authorizationfacts.IdentityQuery) (*authorizationfacts.IdentityPage, error)

func (f identityReaderFunc) QueryDataItemIdentities(c context.Context, t uint, q authorizationfacts.IdentityQuery) (*authorizationfacts.IdentityPage, error) {
	return f(c, t, q)
}

type directPermissionWaitContextKey struct{}

func exerciseDirectPermissionAPI(t *testing.T, db *gorm.DB, identity *iam.Repository, service *engineaccess.Service, actor engineaccess.Actor, engine models.Engine, otherTenantID int64) {
	t.Run("direct permission production API", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		permissions := []string{"system.engine_access_direct_permission.read", "system.engine_access_direct_permission.update"}
		var unexpected int64
		if err := db.Raw(`SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id JOIN system.roles r ON r.id=rp.role_id WHERE p.permission_key='system.engine_access_direct_permission.update' AND r.role_key<>'tenant.engine_access_delegation_administrator'`).Scan(&unexpected).Error; err != nil || unexpected != 0 {
			t.Fatal(unexpected, err)
		}
		roles := iam.NewTenantRoleService(identity, time.Now)
		if _, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: actor.TenantID, RoleKey: "custom.cannot_enable_direct", Name: "Cannot delegate permit administration", PermissionKeys: permissions, ActorPrincipalID: actor.PrincipalID}); err == nil {
			t.Fatal("custom role acquired administrator-only permission")
		}
		var role iam.Role
		if err := db.Table("system.roles").Where("role_key=?", "tenant.engine_access_delegation_administrator").Take(&role).Error; err != nil {
			t.Fatal(err)
		}
		stepUp := time.Now().Add(time.Minute)
		assignments, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: actor.TenantID, MembershipID: actor.MembershipID, RoleIDs: []int64{role.ID}, Reason: "Isolated permit administration", ActorPrincipalID: actor.PrincipalID, AssuranceLevel: iam.AssuranceLevelAAL2, StepUpExpiresAt: &stepUp})
		if err != nil {
			t.Fatal(err)
		}
		refresh := func() {
			p, err := identity.GetPrincipal(ctx, actor.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			actor.AuthorizationVersion = p.AuthorizationVersion
		}
		refresh()
		paths := []engineplugin.EngineCatalogPath{}
		for _, name := range []string{"governance.target", "second.target", "audit_failure", "expired_while_waiting"} {
			paths = append(paths, engineplugin.TabularItemPath(engine.ID, "schema", "public", name))
		}
		assertOwnerRPCUnlocked := true
		service.WithAuthorizationFactsReader(authorizationFactsReaderFunc(func(c context.Context, tenant uint, fp string) (*authorizationfacts.AuthorizationFacts, error) {
			if tenant != uint(actor.TenantID) {
				t.Fatal("cross tenant Meta call")
			}
			// The owner RPC must not hold System's IAM lock.
			if assertOwnerRPCUnlocked {
				if err := db.WithContext(c).Transaction(func(tx *gorm.DB) error {
					return tx.Exec("SELECT id FROM system.principals WHERE id=? FOR UPDATE NOWAIT", actor.PrincipalID).Error
				}); err != nil {
					return nil, err
				}
			}
			for _, path := range paths {
				facts := factstest.Single(path)
				if facts.ItemFingerprint == fp {
					return &facts, nil
				}
			}
			return nil, engineaccess.ErrAuthorizationFactsUnavailable
		}))
		identities := identityReaderFunc(func(c context.Context, tenant uint, q authorizationfacts.IdentityQuery) (*authorizationfacts.IdentityPage, error) {
			if tenant != uint(actor.TenantID) || q.EngineID != engine.ID {
				t.Fatal("wrong identity owner scope")
			}
			if err := q.Validate(); err != nil {
				return nil, err
			}
			if err := db.WithContext(c).Transaction(func(tx *gorm.DB) error {
				return tx.Exec("SELECT id FROM system.principals WHERE id=? FOR UPDATE NOWAIT", actor.PrincipalID).Error
			}); err != nil {
				return nil, err
			}
			all := []authorizationfacts.DataItemIdentity{}
			set := map[string]bool{}
			for _, fp := range q.Fingerprints {
				set[fp] = true
			}
			for _, path := range paths {
				fp := factstest.Binding(path).ItemFingerprint
				name := path.Segments[len(path.Segments)-1].Name
				if q.FingerprintFilter == "include" && !set[fp] || q.FingerprintFilter == "exclude" && set[fp] || q.Search != "" && !strings.Contains(name, q.Search) {
					continue
				}
				all = append(all, authorizationfacts.DataItemIdentity{EngineID: engine.ID, ItemFingerprint: fp, Name: name, FullName: "public/" + name, ItemType: "table"})
			}
			sort.Slice(all, func(i, j int) bool { return all[i].ItemFingerprint < all[j].ItemFingerprint })
			total := len(all)
			start := (q.Page - 1) * q.PageSize
			if start > total {
				start = total
			}
			end := start + q.PageSize
			if end > total {
				end = total
			}
			return &authorizationfacts.IdentityPage{Data: append([]authorizationfacts.DataItemIdentity{}, all[start:end]...), Total: int64(total), Page: q.Page, PageSize: q.PageSize, TotalPages: int64((total + q.PageSize - 1) / q.PageSize)}, nil
		})
		service.WithDataItemIdentityReader(identities)
		target := paths[0]
		binding := factstest.Binding(target)
		observed, err := service.GetDirectPermission(ctx, actor, int64(engine.ID), binding.ItemFingerprint)
		if err != nil {
			t.Fatal(err)
		}
		if observed.Source == "explicit" {
			t.Fatal("fixture accidentally preconfigured")
		}
		input := engineaccess.SetDirectPermissionInput{Actor: actor, EngineID: int64(engine.ID), CatalogPath: target, ItemFingerprint: binding.ItemFingerprint, ExpectedBoundaryHash: binding.BoundaryHash, Allowed: true, ExpectedSource: observed.Source, ExpectedVersion: observed.Version, Reason: "Explicit allow without any Grant"}
		projection := directPermissionAuth(permissions...)
		tenant, member := strconv.FormatInt(actor.TenantID, 10), strconv.FormatInt(actor.MembershipID, 10)
		projection.Principal.ID = strconv.FormatInt(actor.PrincipalID, 10)
		projection.Context.TenantID, projection.Context.TenantMembershipID = &tenant, &member
		projection.Authorization.AuthorizationVersion = strconv.FormatInt(actor.AuthorizationVersion, 10)
		projection.Authorization.RoleAssignments[0].Scope.TenantID = &tenant
		projection.Authorization.RoleAssignments[0].AssignmentID = strconv.FormatInt(assignments[0].ID, 10)
		router := directPermissionTestRouter(t, service, &projection)
		route := fmt.Sprintf("/api/v1/system/engines/%d/data-items/%s/direct-permission", engine.ID, input.ItemFingerprint)
		body := map[string]any{"catalog_path": target, "expected_boundary_hash": input.ExpectedBoundaryHash, "allowed": true, "expected_source": input.ExpectedSource, "expected_version": strconv.FormatInt(input.ExpectedVersion, 10), "reason": input.Reason}
		var grantsBefore int64
		db.Table("system.engine_access_grants").Count(&grantsBefore)
		response := engineDelegationTestRequest(t, router, "PUT", route, body, 200)
		var current engineaccess.DirectPermissionView
		if err := json.Unmarshal(response.Body.Bytes(), &current); err != nil || current.State != "allowed" || current.Source != "explicit" || current.Version != 1 {
			t.Fatal(current, err)
		}
		engineDelegationTestRequest(t, router, "PUT", route, body, 409)
		engineDelegationTestRequest(t, router, "GET", route, nil, 200)
		rows, total, err := service.ListDirectPermissions(ctx, actor, int64(engine.ID), 1, 1, engineaccess.DirectPermissionFilter{State: "allowed"})
		if err != nil || len(rows) != 1 || total != 1 || rows[0].ItemFingerprint != input.ItemFingerprint {
			t.Fatalf("filter-before-pagination: %+v %d %v", rows, total, err)
		}
		all, allTotal, err := service.ListDirectPermissions(ctx, actor, int64(engine.ID), 1, 20, engineaccess.DirectPermissionFilter{})
		if err != nil || allTotal != 4 || len(all) != 4 {
			t.Fatal(allTotal, all, err)
		}
		wrong := input
		wrong.CatalogPath.EngineID++
		if _, err := service.SetDirectPermission(ctx, wrong); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatal(err)
		}
		stale := input
		stale.Actor.AuthorizationVersion--
		if _, err := service.SetDirectPermission(ctx, stale); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatal(err)
		}
		expired := input
		expired.Actor.TokenExpiresAt = time.Now().Add(-time.Minute)
		if _, err := service.SetDirectPermission(ctx, expired); !errors.Is(err, commonapi.ErrUnauthorized) {
			t.Fatal(err)
		}
		otherTenant := uint(otherTenantID)
		foreignEngine := models.Engine{Name: "Foreign permit fixture", EngineType: "postgresql", TenantID: &otherTenant, LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{}, IdentityKey: models.JSONString(`{"host":"foreign.invalid","database":"fixture"}`)}
		if err := db.Table("system.engines").Create(&foreignEngine).Error; err != nil {
			t.Fatal(err)
		}
		foreign := input
		foreign.EngineID = int64(foreignEngine.ID)
		foreign.CatalogPath = engineplugin.TabularItemPath(foreignEngine.ID, "schema", "public", "governance.target")
		foreign.ItemFingerprint, foreign.ExpectedBoundaryHash = factstest.Binding(foreign.CatalogPath).ItemFingerprint, factstest.Binding(foreign.CatalogPath).BoundaryHash
		if _, err := service.SetDirectPermission(ctx, foreign); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatal(err)
		}
		if _, err := service.GetDirectPermission(ctx, actor, foreign.EngineID, foreign.ItemFingerprint); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatal(err)
		}
		exerciseDirectPermissionLockExpiry(t, db, service, input)
		// Concurrent administrators cannot overwrite an unseen change; audit failure rolls back.
		input.ExpectedSource, input.ExpectedVersion, input.Allowed = "explicit", 1, false
		invalid := input
		status := 99
		invalid.Audit.HTTPStatus = &status
		if _, err := service.SetDirectPermission(ctx, invalid); err == nil {
			t.Fatal("failed audit committed permit")
		}
		// NOWAIT above proves this request holds no IAM lock during RPC.
		// During concurrent same-actor commands another request may legitimately
		// hold that row; do not attribute that other transaction to this RPC.
		assertOwnerRPCUnlocked = false
		outcomes := make(chan error, 2)
		for range 2 {
			go func() { _, err := service.SetDirectPermission(ctx, input); outcomes <- err }()
		}
		success, conflict := 0, 0
		for range 2 {
			err := <-outcomes
			if err == nil {
				success++
			} else if errors.Is(err, engineaccess.ErrDirectPermissionVersion) {
				conflict++
			} else {
				t.Fatal(err)
			}
		}
		assertOwnerRPCUnlocked = true
		if success != 1 || conflict != 1 {
			t.Fatal(success, conflict)
		}
		input.ExpectedVersion = 2
		if row, err := service.SetDirectPermission(ctx, input); err != nil || row.Version != 2 {
			t.Fatal(row, err)
		}
		var audits int64
		db.Table("system.audit_logs").Where("event_name=?", "system.engine_access_direct_permission.changed").Count(&audits)
		if audits != 2 {
			t.Fatal("unexpected audit count", audits)
		}
		var grantsAfter int64
		db.Table("system.engine_access_grants").Count(&grantsAfter)
		if grantsBefore != grantsAfter {
			t.Fatal("permission setting issued Grant")
		}
		// Configuration changing across an owner request invalidates the list observation.
		service.WithDataItemIdentityReader(identityReaderFunc(func(c context.Context, tenant uint, q authorizationfacts.IdentityQuery) (*authorizationfacts.IdentityPage, error) {
			change := input
			change.Allowed = true
			if _, err := service.SetDirectPermission(ctx, change); err != nil {
				return nil, err
			}
			return identities(c, tenant, q)
		}))
		if _, _, err := service.ListDirectPermissions(ctx, actor, int64(engine.ID), 1, 20, engineaccess.DirectPermissionFilter{}); !errors.Is(err, engineaccess.ErrDirectPermissionVersion) {
			t.Fatal("mixed observation accepted", err)
		}
		service.WithDataItemIdentityReader(identities)
		if _, err := roles.RevokeAssignment(ctx, iam.RevokeTenantRoleAssignmentInput{TenantID: actor.TenantID, AssignmentID: assignments[0].ID, ActorPrincipalID: actor.PrincipalID, Reason: "End isolated permit authority"}); err != nil {
			t.Fatal(err)
		}
		refresh()
		input.Actor = actor
		if _, err := service.SetDirectPermission(ctx, input); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatal("revoked permission accepted", err)
		}
		if _, _, err := service.ListDirectPermissions(ctx, actor, int64(engine.ID), 1, 20, engineaccess.DirectPermissionFilter{}); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatal("revoked list permission accepted", err)
		}
	})
}

// Wait on the real target advisory lock, not a service mock or host sleep.
// Qualification is valid before waiting and expires before that lock is freed.
func exerciseDirectPermissionLockExpiry(t *testing.T, db *gorm.DB, service *engineaccess.Service, input engineaccess.SetDirectPermissionInput) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	input.CatalogPath = engineplugin.TabularItemPath(uint(input.EngineID), "schema", "public", "expired_while_waiting")
	input.ItemFingerprint, input.ExpectedBoundaryHash = factstest.Binding(input.CatalogPath).ItemFingerprint, factstest.Binding(input.CatalogPath).BoundaryHash
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
			key := fmt.Sprintf("engine_access_target|%d|%d|%s", input.Actor.TenantID, input.EngineID, input.ItemFingerprint)
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
		if tx.Statement.Context.Value(directPermissionWaitContextKey{}) != true || !strings.Contains(tx.Statement.SQL.String(), "pg_advisory_xact_lock") {
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
		_, err := service.SetDirectPermission(context.WithValue(ctx, directPermissionWaitContextKey{}, true), input)
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
	if err := db.Table("system.engine_access_direct_permissions").Where("tenant_id=? AND catalog_path=?::jsonb", input.Actor.TenantID, string(path)).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("expiry committed requirement: count=%d err=%v", count, err)
	}
}
