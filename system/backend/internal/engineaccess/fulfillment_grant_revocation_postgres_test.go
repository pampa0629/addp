package engineaccess

import (
	"context"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Extends the existing standard disposable-PG first-acceptance fixture, not a
// development database or a separate migration/authorization bypass.
func exerciseGrantRevocations(t *testing.T, db *gorm.DB, acceptor *Service, runtime FulfillmentRuntimeActor,
	base shared.SharingFulfillmentBinding, roles *iam.TenantRoleService, handlingRoleID, adminID int64,
	newOperator func(*testing.T, time.Duration) (userProvenance, time.Time), seedDelegation func(*testing.T, int64, time.Time) *Delegation,
) {
	t.Run("specific Grant withdrawal", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		runtime.TokenExpiresAt = time.Now().Add(time.Minute)
		service := NewService(NewRepository(db), nil) // No live-catalog capability or remote reader.
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: runtime.TenantID,
			RoleKey: "custom.grant_revoker", Name: "Explicit withdrawal fixture", ScopeTypes: []string{"tenant"},
			PermissionKeys: []string{"system.engine_access_grant.revoke"}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		qualified := func(t *testing.T, lifetime time.Duration) (Actor, *Delegation) {
			t.Helper()
			user, expires := newOperator(t, time.Hour)
			delegation := seedDelegation(t, user.MembershipID, expires.Add(-time.Second))
			input := iam.CreateTenantRoleAssignmentsInput{TenantID: runtime.TenantID, MembershipID: user.MembershipID,
				RoleIDs: []int64{role.ID}, ScopeType: "tenant", ActorPrincipalID: adminID, Reason: "Explicit fixture withdrawal"}
			if lifetime > 0 {
				until := time.Now().Add(lifetime)
				input.ValidUntil = &until
			}
			if _, err := roles.CreateAssignments(ctx, input); err != nil {
				t.Fatal(err)
			}
			current, err := iam.NewRepository(db).GetPrincipal(ctx, user.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			return Actor{TenantID: runtime.TenantID, PrincipalID: user.PrincipalID, MembershipID: user.MembershipID,
				AuthorizationVersion: current.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}, delegation
		}
		issue := func(t *testing.T, binding shared.SharingFulfillmentBinding) *fulfillmentGrant {
			t.Helper()
			id := uuid.New()
			if _, err := acceptor.AcceptFulfillment(ctx, runtime, id, binding); err != nil {
				t.Fatal(err)
			}
			g, err := service.writeAcceptedGrant(ctx, runtime, id, binding)
			if err != nil {
				t.Fatal(err)
			}
			return g
		}
		inputFor := func(actor Actor, id uuid.UUID) RevokeGrantInput {
			return RevokeGrantInput{Actor: actor, EngineID: int64(base.Path.EngineID), RequestID: id, Reason: "Withdraw this Grant"}
		}
		assertCount := func(t *testing.T, id uuid.UUID, want int64) {
			t.Helper()
			for _, table := range []string{"system.engine_access_grant_revocations", "system.audit_logs"} {
				query := db.Table(table).Where("request_id=?", id)
				if table == "system.audit_logs" {
					query = db.Table(table).Where("event_name='system.engine_access_grant.revoked' AND entity_id=?", id.String())
				}
				var n int64
				if err := query.Count(&n).Error; err != nil || n != want {
					t.Fatalf("%s count=%d want=%d err=%v", table, n, want, err)
				}
			}
		}
		finiteGrant := func(t *testing.T) (*fulfillmentGrant, time.Time) {
			t.Helper()
			var expires time.Time
			if err := db.Raw("SELECT clock_timestamp() + interval '1.5 seconds'").Scan(&expires).Error; err != nil {
				t.Fatal(err)
			}
			binding := base
			binding.ExpiryMode, binding.ExpiresAt = shared.SharingExpiryAtTime, &expires
			return issue(t, binding), expires
		}
		waitExpiry := func(db *gorm.DB, expires time.Time) error {
			// Reset the audit INSERT statement after attaching the context while
			// retaining its transaction; otherwise its bind variables leak here.
			return db.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Exec("SELECT pg_sleep(GREATEST(EXTRACT(EPOCH FROM (?::timestamptz - clock_timestamp())) + 0.03, 0))", expires).Error
		}
		t.Run("expired first withdrawal writes neither history nor audit", func(t *testing.T) {
			actor, _ := qualified(t, 0)
			g, expires := finiteGrant(t)
			if err := waitExpiry(db, expires); err != nil {
				t.Fatal(err)
			}
			if row, err := service.RevokeGrant(ctx, inputFor(actor, g.RequestID)); row != nil || !errors.Is(err, ErrGrantRevocationExpired) {
				t.Fatalf("expired first withdrawal=%+v %v", row, err)
			}
			assertCount(t, g.RequestID, 0)
			// A caller-supplied old timestamp cannot bypass the storage boundary.
			tx := db.WithContext(ctx).Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			row := &GrantRevocation{RequestID: g.RequestID, RevokedByPrincipalID: actor.PrincipalID,
				RevokedByMembershipID: actor.MembershipID, RevokedAt: g.GrantedAt, Reason: "Backdated expired withdrawal"}
			if err := mapError(tx.Create(row).Error); !errors.Is(err, ErrGrantRevocationExpired) {
				t.Fatalf("storage permitted expired insertion: %v", err)
			}
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			assertCount(t, g.RequestID, 0)
			actor.TokenExpiresAt = time.Now().Add(-time.Second)
			if row, err := service.RevokeGrant(ctx, inputFor(actor, g.RequestID)); row != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("expired Grant leaked before qualification: %+v %v", row, err)
			}
		})
		t.Run("withdrawal before expiry retries after expiry without a new fact", func(t *testing.T) {
			actor, _ := qualified(t, 0)
			g, expires := finiteGrant(t)
			input := inputFor(actor, g.RequestID)
			original, err := service.RevokeGrant(ctx, input)
			if err != nil || original == nil || !original.RevokedAt.Before(expires) {
				t.Fatalf("before expiry=%+v %v", original, err)
			}
			if err := waitExpiry(db, expires); err != nil {
				t.Fatal(err)
			}
			input.Reason = "  " + input.Reason + "\n"
			if row, err := service.RevokeGrant(ctx, input); err != nil || row == nil || !row.RevokedAt.Equal(original.RevokedAt) {
				t.Fatalf("expired retry changed history: %+v %v", row, err)
			}
			input.Reason = "Different reason after expiry"
			if _, err := service.RevokeGrant(ctx, input); !errors.Is(err, ErrGrantRevocationConflict) {
				t.Fatalf("expiry masked different-parameter conflict: %v", err)
			}
			input.Reason = original.Reason
			input.Actor.TokenExpiresAt = time.Now().Add(-time.Second)
			if row, err := service.RevokeGrant(ctx, input); row != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("expired retry bypassed current qualification: %+v %v", row, err)
			}
			assertCount(t, g.RequestID, 1)
		})
		t.Run("Grant expiry during insert is an expiry conflict not a storage failure", func(t *testing.T) {
			actor, _ := qualified(t, 0)
			g, expires := finiteGrant(t)
			const callback = "fixture:withdrawal_insert_expiry"
			reached := false
			if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if row, ok := tx.Statement.Dest.(*GrantRevocation); ok && row.RequestID == g.RequestID {
					reached = true
					if err := waitExpiry(tx.Session(&gorm.Session{NewDB: true}), expires); err != nil {
						tx.AddError(err)
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Create().Remove(callback)
			if row, err := service.RevokeGrant(ctx, inputFor(actor, g.RequestID)); row != nil || !errors.Is(err, ErrGrantRevocationExpired) || !reached {
				t.Fatalf("insertion boundary: reached=%v row=%+v err=%v", reached, row, err)
			}
			assertCount(t, g.RequestID, 0)
		})
		t.Run("Grant expiry after successful audit rolls back both facts", func(t *testing.T) {
			actor, _ := qualified(t, 0)
			g, expires := finiteGrant(t)
			const callback = "fixture:withdrawal_audit_grant_expiry"
			auditWritten := false
			if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
				if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "system.engine_access_grant.revoked" {
					auditWritten = tx.Error == nil
					if err := waitExpiry(tx.Session(&gorm.Session{NewDB: true}), expires); err != nil {
						tx.AddError(err)
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Create().Remove(callback)
			if row, err := service.RevokeGrant(ctx, inputFor(actor, g.RequestID)); row != nil || !errors.Is(err, ErrGrantRevocationExpired) || !auditWritten {
				t.Fatalf("audit expiry: written=%v row=%+v err=%v", auditWritten, row, err)
			}
			assertCount(t, g.RequestID, 0)
		})
		t.Run("target lock wait rechecks Grant natural expiry", func(t *testing.T) {
			actor, _ := qualified(t, 0)
			g, expires := finiteGrant(t)
			path, err := shared.EncodeSharingTarget(base.Path)
			if err != nil {
				t.Fatal(err)
			}
			tx := db.WithContext(ctx).Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			if err := NewRepository(tx).lockFulfillmentTarget(ctx, runtime.TenantID, path); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() { _, err := service.RevokeGrant(ctx, inputFor(actor, g.RequestID)); result <- err }()
			blocked := false
			for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
				if err := db.Raw("SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))").Scan(&blocked).Error; err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !blocked {
				t.Fatal("withdrawal did not wait on the target lock")
			}
			if err := waitExpiry(tx, expires); err != nil {
				t.Fatal(err)
			}
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, ErrGrantRevocationExpired) {
				t.Fatalf("lock wait escaped Grant expiry: %v", err)
			}
			assertCount(t, g.RequestID, 0)
		})
		t.Run("personal withdrawal leaves group and other personal Grants unchanged", func(t *testing.T) {
			actor, _ := qualified(t, 0)
			personal := issue(t, base)
			other := issue(t, base)
			organizations := iam.NewOrganizationService(iam.NewRepository(db), time.Now)
			group, err := organizations.CreateProjectGroup(ctx, iam.CreateProjectGroupInput{TenantID: runtime.TenantID,
				ActorPrincipalID: adminID, Code: "withdrawal_fixture_group", Name: "Withdrawal fixture group"})
			if err != nil {
				t.Fatal(err)
			}
			binding := base
			binding.RecipientType, binding.RecipientID = "project_group", group.ID
			groupGrant := issue(t, binding)
			input := inputFor(actor, personal.RequestID)
			type response struct {
				row *GrantRevocation
				err error
			}
			results := make(chan response, 4)
			for i := 0; i < cap(results); i++ {
				go func() { r, e := service.RevokeGrant(ctx, input); results <- response{r, e} }()
			}
			var original time.Time
			for i := 0; i < cap(results); i++ {
				result := <-results
				if result.err != nil || result.row == nil || result.row.RevokedAt.IsZero() {
					t.Fatalf("withdrawal=%+v %v", result.row, result.err)
				}
				if !original.IsZero() && !original.Equal(result.row.RevokedAt) {
					t.Fatal("retry replaced original withdrawal")
				}
				original = result.row.RevokedAt
			}
			assertCount(t, personal.RequestID, 1)
			assertCount(t, other.RequestID, 0)
			assertCount(t, groupGrant.RequestID, 0)
			input.Reason = "different reason"
			if r, err := service.RevokeGrant(ctx, input); r != nil || !errors.Is(err, ErrGrantRevocationConflict) {
				t.Fatalf("different retry=%+v %v", r, err)
			}
			input.Actor, _ = qualified(t, 0)
			input.Reason = "Withdraw this Grant"
			if _, err := service.RevokeGrant(ctx, input); !errors.Is(err, ErrGrantRevocationConflict) {
				t.Fatalf("different revoker=%v", err)
			}
			for _, g := range []*fulfillmentGrant{personal, other, groupGrant} {
				b := base
				if g == groupGrant {
					b = binding
				}
				recovered, err := service.IssueFulfillmentGrant(ctx, runtime, g.RequestID, b)
				if err != nil || recovered == nil || !recovered.GrantedAt.Equal(g.GrantedAt) {
					t.Fatalf("withdrawal rewrote issuance=%+v %v", recovered, err)
				}
				lookup, err := service.ResolveFulfillmentGrant(ctx, runtime, g.RequestID, b)
				if err != nil || lookup == nil || !lookup.Found || lookup.Grant == nil || !lookup.Grant.GrantedAt.Equal(g.GrantedAt) {
					t.Fatalf("withdrawal changed public issuance history=%+v %v", lookup, err)
				}
			}
			assertCount(t, personal.RequestID, 1)
			for _, sql := range []string{
				"UPDATE system.engine_access_grant_revocations SET reason='changed' WHERE request_id=?",
				"DELETE FROM system.engine_access_grant_revocations WHERE request_id=?",
			} {
				if err := db.Exec(sql, personal.RequestID).Error; err == nil {
					t.Fatal("withdrawal history mutable")
				}
			}
			if err := db.Exec("TRUNCATE system.engine_access_grant_revocations").Error; err == nil {
				t.Fatal("withdrawal history truncatable")
			}
		})
		t.Run("disabled engine can withdraw but cannot handle new access", func(t *testing.T) {
			actor, _ := qualified(t, 0)
			g := issue(t, base)
			if err := db.Table("system.engines").Where("id=?", base.Path.EngineID).Update("lifecycle_state", "disabled").Error; err != nil {
				t.Fatal(err)
			}
			defer db.Table("system.engines").Where("id=?", base.Path.EngineID).Update("lifecycle_state", "active")
			if _, err := service.RevokeGrant(ctx, inputFor(actor, g.RequestID)); err != nil {
				t.Fatal(err)
			}
			assertCount(t, g.RequestID, 1)
			original := Actor{TenantID: runtime.TenantID, PrincipalID: base.Operator.PrincipalID, MembershipID: base.Operator.MembershipID,
				AuthorizationVersion: base.Operator.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}
			if _, err := service.GetHandlingRequirement(ctx, original, base.Path); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("disabled new handling=%v", err)
			}
		})
		t.Run("tenant engine and issuance isolation", func(t *testing.T) {
			actor, _ := qualified(t, 0)
			for _, id := range []uuid.UUID{uuid.New(), func() uuid.UUID {
				id := uuid.New()
				if _, err := acceptor.AcceptFulfillment(ctx, runtime, id, base); err != nil {
					t.Fatal(err)
				}
				return id
			}()} {
				if _, err := service.RevokeGrant(ctx, inputFor(actor, id)); !errors.Is(err, commonapi.ErrNotFound) {
					t.Fatalf("unissued/missing=%v", err)
				}
				assertCount(t, id, 0)
			}
			g := issue(t, base)
			wrong := inputFor(actor, g.RequestID)
			wrong.EngineID = 9007199254740993
			if _, err := service.RevokeGrant(ctx, wrong); !errors.Is(err, commonapi.ErrNotFound) {
				t.Fatalf("cross engine=%v", err)
			}
			wrong = inputFor(actor, g.RequestID)
			wrong.Actor.TenantID++
			if _, err := service.RevokeGrant(ctx, wrong); !errors.Is(err, commonapi.ErrNotFound) {
				t.Fatalf("cross tenant identity=%v", err)
			}
			assertCount(t, g.RequestID, 0)
		})
		for _, scenario := range []string{"account suspended", "membership suspended", "stale version", "expired token", "delegation revoked", "permission expired", "no permission"} {
			t.Run(scenario, func(t *testing.T) {
				lifetime := time.Duration(0)
				if scenario == "permission expired" {
					lifetime = time.Second
				}
				actor, delegation := qualified(t, lifetime)
				g := issue(t, base)
				want := commonapi.ErrForbidden
				var err error
				switch scenario {
				case "account suspended":
					err = db.Table("system.principals").Where("id=?", actor.PrincipalID).Update("status", "suspended").Error
				case "membership suspended":
					err = db.Table("system.tenant_memberships").Where("id=?", actor.MembershipID).Update("status", "suspended").Error
				case "stale version":
					actor.AuthorizationVersion++
				case "expired token":
					actor.TokenExpiresAt = time.Now().Add(-time.Second)
					want = commonapi.ErrUnauthorized
				case "delegation revoked":
					err = service.repository.transaction(ctx, func(tx *Repository) error {
						now, e := tx.wallClock(ctx)
						if e != nil {
							return e
						}
						return tx.revoke(ctx, delegation, delegation.Version, adminID, "Fixture withdrawal", now)
					})
				case "permission expired":
					err = db.Exec("SELECT pg_sleep(1.05)").Error
				case "no permission":
					user, expiry := newOperator(t, time.Hour)
					seedDelegation(t, user.MembershipID, expiry.Add(-time.Second))
					actor.PrincipalID, actor.MembershipID, actor.AuthorizationVersion = user.PrincipalID, user.MembershipID, user.AuthorizationVersion
				}
				if err != nil {
					t.Fatal(err)
				}
				if r, err := service.RevokeGrant(ctx, inputFor(actor, g.RequestID)); r != nil || !errors.Is(err, want) {
					t.Fatalf("lost qualification=%+v %v", r, err)
				}
				assertCount(t, g.RequestID, 0)
			})
		}
		t.Run("audit failure rolls back withdrawal and retry succeeds", func(t *testing.T) {
			actor, _ := qualified(t, 0)
			g := issue(t, base)
			failure := errors.New("fixture withdrawal audit failure")
			const callback = "fixture:withdrawal_audit_failure"
			if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "system.engine_access_grant.revoked" {
					tx.AddError(failure)
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Create().Remove(callback)
			input := inputFor(actor, g.RequestID)
			if r, err := service.RevokeGrant(ctx, input); r != nil || !errors.Is(err, failure) {
				t.Fatalf("audit failure=%+v %v", r, err)
			}
			assertCount(t, g.RequestID, 0)
			if err := db.Callback().Create().Remove(callback); err != nil {
				t.Fatal(err)
			}
			if _, err := service.RevokeGrant(ctx, input); err != nil {
				t.Fatal(err)
			}
			assertCount(t, g.RequestID, 1)
		})
		t.Run("retry still requires current qualification", func(t *testing.T) {
			actor, delegation := qualified(t, 0)
			g := issue(t, base)
			input := inputFor(actor, g.RequestID)
			if _, err := service.RevokeGrant(ctx, input); err != nil {
				t.Fatal(err)
			}
			if err := service.repository.transaction(ctx, func(tx *Repository) error {
				now, err := tx.wallClock(ctx)
				if err != nil {
					return err
				}
				return tx.revoke(ctx, delegation, delegation.Version, adminID, "Fixture delegation ended", now)
			}); err != nil {
				t.Fatal(err)
			}
			if r, err := service.RevokeGrant(ctx, input); r != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("unqualified retry exposed history=%+v %v", r, err)
			}
			assertCount(t, g.RequestID, 1)
		})
		t.Run("normalized reason retries preserve original history", func(t *testing.T) {
			actor, _ := qualified(t, 0)
			g := issue(t, base)
			input := inputFor(actor, g.RequestID)
			input.Reason = " \tWithdraw this Grant\n "
			original, err := service.RevokeGrant(ctx, input)
			if err != nil || original == nil || original.Reason != "Withdraw this Grant" {
				t.Fatalf("normalized withdrawal=%+v %v", original, err)
			}
			input.Reason = "Withdraw this Grant"
			retried, err := service.RevokeGrant(ctx, input)
			if err != nil || retried == nil || !retried.RevokedAt.Equal(original.RevokedAt) {
				t.Fatalf("normalized retry replaced history=%+v %v", retried, err)
			}
			assertCount(t, g.RequestID, 1)
		})
		t.Run("audit wait cannot commit withdrawal with expired token", func(t *testing.T) {
			actor, _ := qualified(t, 0)
			g := issue(t, base)
			const callback = "fixture:withdrawal_audit_delay"
			auditWritten := false
			if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
				if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "system.engine_access_grant.revoked" {
					auditWritten = tx.Error == nil
					if err := tx.Session(&gorm.Session{NewDB: true}).Exec("SELECT pg_sleep(0.4)").Error; err != nil {
						tx.AddError(err)
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Create().Remove(callback)
			actor.TokenExpiresAt = time.Now().Add(300 * time.Millisecond)
			input := inputFor(actor, g.RequestID)
			if row, err := service.RevokeGrant(ctx, input); row != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("audit wait escaped token expiry=%+v %v", row, err)
			}
			if !auditWritten {
				t.Fatal("fixture did not reach the successful audit write before waiting")
			}
			assertCount(t, g.RequestID, 0)
			if err := db.Callback().Create().Remove(callback); err != nil {
				t.Fatal(err)
			}
			input.Actor.TokenExpiresAt = time.Now().Add(time.Minute)
			if _, err := service.RevokeGrant(ctx, input); err != nil {
				t.Fatal(err)
			}
			assertCount(t, g.RequestID, 1)
		})
		t.Run("historical handler or recipient loss cannot strand withdrawal", func(t *testing.T) {
			actor, _ := qualified(t, 0)
			handler, expires := newOperator(t, time.Hour)
			seedDelegation(t, handler.MembershipID, expires.Add(-time.Second))
			if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: runtime.TenantID,
				MembershipID: handler.MembershipID, RoleIDs: []int64{handlingRoleID}, ScopeType: "tenant",
				ActorPrincipalID: adminID, Reason: "Isolated historical handler"}); err != nil {
				t.Fatal(err)
			}
			current, err := iam.NewRepository(db).GetPrincipal(ctx, handler.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			recipient, _ := newOperator(t, time.Hour)
			binding := base
			binding.Operator = shared.SharingFulfillmentOperator{PrincipalID: handler.PrincipalID,
				MembershipID: handler.MembershipID, AuthorizationVersion: current.AuthorizationVersion}
			binding.RecipientID = recipient.PrincipalID
			g := issue(t, binding)
			if err := db.Table("system.principals").Where("id=?", recipient.PrincipalID).Update("status", "suspended").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Table("system.principals").Where("id=?", handler.PrincipalID).Update("status", "suspended").Error; err != nil {
				t.Fatal(err)
			}
			if _, err := service.RevokeGrant(ctx, inputFor(actor, g.RequestID)); err != nil {
				t.Fatal(err)
			}
			assertCount(t, g.RequestID, 1)
			if recovered, err := service.writeAcceptedGrant(ctx, runtime, g.RequestID, binding); err != nil || recovered == nil || !recovered.GrantedAt.Equal(g.GrantedAt) {
				t.Fatalf("withdrawal destroyed historical replay=%+v %v", recovered, err)
			}
		})
		t.Run("database binds revoker to original tenant and overwrites backdated timestamp", func(t *testing.T) {
			actor, _ := qualified(t, 0)
			g := issue(t, base)
			row := &GrantRevocation{RequestID: g.RequestID, RevokedByPrincipalID: actor.PrincipalID,
				RevokedByMembershipID: base.Operator.MembershipID, RevokedAt: time.Now(), Reason: "Invalid membership binding"}
			if err := db.Create(row).Error; err == nil {
				t.Fatal("mismatched revoker membership accepted")
			}
			// Test storage guard in a rollback-only transaction, never create an
			// unaudited committed production fact.
			tx := db.WithContext(ctx).Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			row.RevokedByMembershipID, row.RevokedAt = actor.MembershipID, time.Unix(1, 0)
			if err := tx.Raw(`INSERT INTO system.engine_access_grant_revocations
				(request_id,revoked_by_principal_id,revoked_by_membership_id,revoked_at,reason)
				VALUES (?,?,?,?,?) RETURNING *`, row.RequestID, row.RevokedByPrincipalID, row.RevokedByMembershipID, row.RevokedAt, row.Reason).Scan(row).Error; err != nil {
				t.Fatal(err)
			}
			if row.RevokedAt.Before(g.GrantedAt) {
				t.Fatal("client backdated withdrawal")
			}
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			assertCount(t, g.RequestID, 0)
		})
		t.Run("target lock wait rechecks current token", func(t *testing.T) {
			actor, _ := qualified(t, 0)
			g := issue(t, base)
			path, err := shared.EncodeSharingTarget(base.Path)
			if err != nil {
				t.Fatal(err)
			}
			tx := db.WithContext(ctx).Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			if err := NewRepository(tx).lockFulfillmentTarget(ctx, runtime.TenantID, path); err != nil {
				t.Fatal(err)
			}
			actor.TokenExpiresAt = time.Now().Add(300 * time.Millisecond)
			result := make(chan error, 1)
			go func() { _, err := service.RevokeGrant(ctx, inputFor(actor, g.RequestID)); result <- err }()
			if err := tx.Exec("SELECT pg_sleep(0.4)").Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("lock wait escaped expiry: %v", err)
			}
			assertCount(t, g.RequestID, 0)
		})
	})
}
