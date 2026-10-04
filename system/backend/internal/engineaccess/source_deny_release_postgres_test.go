package engineaccess

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Formal IAM fixture, standard disposable PG gate; never contacts a source.
func exerciseDenyReleases(t *testing.T, db *gorm.DB, tenantID int64, path engineplugin.EngineCatalogPath,
	roles *iam.TenantRoleService, adminID int64, newOperator func(*testing.T, time.Duration) (userProvenance, time.Time),
	seedDelegation func(*testing.T, int64, time.Time) *Delegation) {
	t.Run("source Deny release", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		service := NewService(NewRepository(db), nil)
		makeRole := func(key, permission string) *iam.TenantRole {
			row, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: key, Name: key,
				ScopeTypes: []string{"tenant"}, PermissionKeys: []string{permission}, ActorPrincipalID: adminID})
			if err != nil {
				t.Fatal(err)
			}
			return row
		}
		creatorRole := makeRole("custom.deny_release_creator", "system.engine_access_deny.create")
		releaseRole := makeRole("custom.deny_releaser", "system.engine_access_deny.release")
		qualified := func(t *testing.T, role *iam.TenantRole, delegated bool, lifetime time.Duration) (Actor, *Delegation) {
			t.Helper()
			user, expires := newOperator(t, time.Hour)
			var delegation *Delegation
			if delegated {
				delegation = seedDelegation(t, user.MembershipID, expires.Add(-time.Second))
			}
			if role != nil {
				input := iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ScopeType: "tenant", ActorPrincipalID: adminID, Reason: "Deny release fixture"}
				if lifetime > 0 {
					var now time.Time
					if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
						t.Fatal(err)
					}
					until := now.Add(lifetime)
					input.ValidUntil = &until
				}
				if _, err := roles.CreateAssignments(ctx, input); err != nil {
					t.Fatal(err)
				}
			}
			current, err := iam.NewRepository(db).GetPrincipal(ctx, user.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			return Actor{TenantID: tenantID, PrincipalID: user.PrincipalID, MembershipID: user.MembershipID,
				AuthorizationVersion: current.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}, delegation
		}
		creator, _ := qualified(t, creatorRole, true, 0)
		releaser, _ := qualified(t, releaseRole, true, 0)
		create := func(t *testing.T, finite bool) *SourceDeny {
			t.Helper()
			recipient, _ := newOperator(t, time.Hour)
			input := CreateDenyInput{Actor: creator, EngineID: int64(path.EngineID), DenyID: uuid.New(), CatalogPath: path,
				RecipientType: "user", RecipientID: recipient.PrincipalID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Restrict precise reading"}
			if finite {
				var expires time.Time
				if err := db.Raw("SELECT clock_timestamp()+interval '1.5 seconds'").Scan(&expires).Error; err != nil {
					t.Fatal(err)
				}
				input.ExpiryMode, input.ExpiresAt = shared.SharingExpiryAtTime, &expires
			}
			row, err := service.CreateDeny(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			return row
		}
		inputFor := func(row *SourceDeny) ReleaseDenyInput {
			return ReleaseDenyInput{Actor: releaser, EngineID: int64(path.EngineID), DenyID: row.DenyID, Reason: "Remove this restriction"}
		}
		assertCount := func(t *testing.T, id uuid.UUID, want int64) {
			t.Helper()
			for _, table := range []string{"system.engine_access_deny_releases", "system.audit_logs"} {
				query := db.Table(table).Where("deny_id=?", id)
				if table == "system.audit_logs" {
					query = db.Table(table).Where("event_name='system.engine_access_deny.released' AND entity_id=?", id.String())
				}
				var n int64
				if err := query.Count(&n).Error; err != nil || n != want {
					t.Fatalf("%s count=%d want=%d err=%v", table, n, want, err)
				}
			}
		}
		waitUntil := func(conn *gorm.DB, expires time.Time) error {
			return conn.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Exec("SELECT pg_sleep(GREATEST(EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp()))+0.03,0))", expires).Error
		}
		grantSnapshots := map[string]string{}
		for _, table := range []string{"system.engine_access_grants", "system.engine_access_grant_revocations", "system.engine_access_fulfillment_outcomes"} {
			var snapshot string
			if err := db.Raw("SELECT COALESCE(jsonb_agg(to_jsonb(g) ORDER BY request_id)::text,'[]') FROM " + table + " g").Scan(&snapshot).Error; err != nil {
				t.Fatal(err)
			}
			grantSnapshots[table] = snapshot
		}
		t.Run("release qualification independent and history hidden", func(t *testing.T) {
			row := create(t, false)
			for _, actor := range []Actor{creator, func() Actor { a, _ := qualified(t, releaseRole, false, 0); return a }(), func() Actor { a, _ := qualified(t, nil, true, 0); return a }()} {
				input := inputFor(row)
				input.Actor = actor
				if _, err := service.ReleaseDeny(ctx, input); !errors.Is(err, commonapi.ErrForbidden) {
					t.Fatalf("unqualified release=%v", err)
				}
				input.DenyID = uuid.New()
				if _, err := service.ReleaseDeny(ctx, input); !errors.Is(err, commonapi.ErrForbidden) {
					t.Fatalf("missing history disclosed before qualification=%v", err)
				}
			}
			input := inputFor(row)
			input.DenyID = uuid.New()
			if _, err := service.ReleaseDeny(ctx, input); !errors.Is(err, commonapi.ErrNotFound) {
				t.Fatalf("missing=%v", err)
			}
			// A valid rule in another Engine is still hidden from this qualified scope.
			var other models.Engine
			if err := db.Table("system.engines").First(&other, path.EngineID).Error; err != nil {
				t.Fatal(err)
			}
			other.ID, other.Name, other.IdentityKey = 0, "Deny release other scope", models.JSONString(`{"host":"fulfillment.invalid","database":"deny_release_other"}`)
			if err := db.Table("system.engines").Create(&other).Error; err != nil {
				t.Fatal(err)
			}
			foreign := *row
			foreign.DenyID, foreign.EngineID = uuid.New(), int64(other.ID)
			encoded, err := shared.EncodeSharingTarget(engineplugin.TabularItemPath(other.ID, "schema", "public", "fixture"))
			if err != nil {
				t.Fatal(err)
			}
			foreign.CatalogPath = encoded
			if err := db.Create(&foreign).Error; err != nil {
				t.Fatal(err)
			}
			input.DenyID = foreign.DenyID
			if _, err := service.ReleaseDeny(ctx, input); !errors.Is(err, commonapi.ErrNotFound) {
				t.Fatalf("cross-engine=%v", err)
			}
			assertCount(t, foreign.DenyID, 0)
			assertCount(t, row.DenyID, 0)
		})
		t.Run("disabled engine and inactive recipient still release without Catalog", func(t *testing.T) {
			row := create(t, false)
			if _, err := iam.NewTenantMembershipService(iam.NewRepository(db), time.Now).SuspendMembership(ctx, iam.ChangeTenantMembershipInput{TenantID: tenantID, PrincipalID: row.RecipientID, Reason: "Deny release inactive subject"}); err != nil {
				t.Fatal(err)
			}
			if err := db.Table("system.engines").Where("id=?", path.EngineID).Update("lifecycle_state", "disabled").Error; err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := db.Table("system.engines").Where("id=?", path.EngineID).Update("lifecycle_state", "active").Error; err != nil {
					t.Error(err)
				}
			}()
			input := inputFor(row)
			original, err := service.ReleaseDeny(ctx, input)
			if err != nil || original == nil || original.ReleasedByPrincipalID == row.EstablishedByPrincipalID {
				t.Fatalf("other creator release=%+v %v", original, err)
			}
			input.Reason = "  " + input.Reason + "\n"
			replay, err := service.ReleaseDeny(ctx, input)
			if err != nil || replay == nil || !replay.ReleasedAt.Equal(original.ReleasedAt) {
				t.Fatalf("retry=%+v %v", replay, err)
			}
			input.Reason = "different"
			if _, err := service.ReleaseDeny(ctx, input); !errors.Is(err, ErrDenyReleaseConflict) {
				t.Fatalf("changed reason=%v", err)
			}
			input.Reason = original.Reason
			input.Actor, _ = qualified(t, releaseRole, true, 0)
			if _, err := service.ReleaseDeny(ctx, input); !errors.Is(err, ErrDenyReleaseConflict) {
				t.Fatalf("changed actor=%v", err)
			}
			input.Actor = releaser
			input.Actor.AuthorizationVersion--
			if _, err := service.ReleaseDeny(ctx, input); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("stale history access=%v", err)
			}
			for _, statement := range []string{"UPDATE system.engine_access_deny_releases SET reason='overwrite' WHERE deny_id=?", "DELETE FROM system.engine_access_deny_releases WHERE deny_id=?", "TRUNCATE system.engine_access_deny_releases"} {
				tx := db.Begin()
				if tx.Error != nil {
					t.Fatal(tx.Error)
				}
				var err error
				if statement == "TRUNCATE system.engine_access_deny_releases" {
					err = tx.Exec(statement).Error
				} else {
					err = tx.Exec(statement, row.DenyID).Error
				}
				tx.Rollback()
				if err == nil {
					t.Fatalf("mutable release: %s", statement)
				}
			}
			parent, err := NewRepository(db).findSourceDeny(ctx, row.DenyID)
			if err != nil || !sameSourceDeny(parent, row) || !parent.EstablishedAt.Equal(row.EstablishedAt) {
				t.Fatalf("parent rewritten: %+v %v", parent, err)
			}
			assertCount(t, row.DenyID, 1)
		})
		t.Run("first expiry writes no fact and rejects backdating", func(t *testing.T) {
			row := create(t, true)
			if err := waitUntil(db, *row.ExpiresAt); err != nil {
				t.Fatal(err)
			}
			input := inputFor(row)
			if got, err := service.ReleaseDeny(ctx, input); got != nil || !errors.Is(err, ErrDenyReleaseExpired) {
				t.Fatalf("expired=%+v %v", got, err)
			}
			tx := db.Begin()
			defer tx.Rollback()
			backdate := DenyRelease{DenyID: row.DenyID, ReleasedByPrincipalID: releaser.PrincipalID, ReleasedByMembershipID: releaser.MembershipID, ReleasedAt: row.EstablishedAt, Reason: input.Reason}
			if err := mapError(tx.Create(&backdate).Error); !errors.Is(err, ErrDenyReleaseExpired) {
				t.Fatalf("backdated=%v", err)
			}
			tx.Rollback()
			input.Actor.TokenExpiresAt = time.Now().Add(-time.Second)
			if _, err := service.ReleaseDeny(ctx, input); !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("expiry exposed before credentials=%v", err)
			}
			assertCount(t, row.DenyID, 0)
		})
		t.Run("committed release retry after original expiry", func(t *testing.T) {
			row := create(t, true)
			input := inputFor(row)
			original, err := service.ReleaseDeny(ctx, input)
			if err != nil || original == nil || !original.ReleasedAt.Before(*row.ExpiresAt) {
				t.Fatalf("finite release=%+v %v", original, err)
			}
			if err := waitUntil(db, *row.ExpiresAt); err != nil {
				t.Fatal(err)
			}
			if _, err := iam.NewTenantMembershipService(iam.NewRepository(db), time.Now).SuspendMembership(ctx, iam.ChangeTenantMembershipInput{TenantID: tenantID, PrincipalID: row.RecipientID, Reason: "Expired rule subject"}); err != nil {
				t.Fatal(err)
			}
			input.Reason = "  " + input.Reason + "\n"
			replay, err := service.ReleaseDeny(ctx, input)
			if err != nil || replay == nil || !replay.ReleasedAt.Equal(original.ReleasedAt) {
				t.Fatalf("expired replay=%+v %v", replay, err)
			}
			input.Reason = "changed after expiry"
			if _, err := service.ReleaseDeny(ctx, input); !errors.Is(err, ErrDenyReleaseConflict) {
				t.Fatalf("expiry hid conflict=%v", err)
			}
			input.Reason = original.Reason
			input.Actor.TokenExpiresAt = time.Now().Add(-time.Second)
			if _, err := service.ReleaseDeny(ctx, input); !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("retry bypassed current qualification=%v", err)
			}
			assertCount(t, row.DenyID, 1)
		})
		for _, phase := range []string{"insert", "audit"} {
			t.Run("expiry during "+phase+" rolls back", func(t *testing.T) {
				row := create(t, true)
				callback := "fixture:deny_release_expiry_" + phase
				reached := false
				block := func(tx *gorm.DB) {
					if phase == "insert" {
						if r, ok := tx.Statement.Dest.(*DenyRelease); !ok || r.DenyID != row.DenyID {
							return
						}
					} else {
						if log, ok := tx.Statement.Dest.(*iam.AuditLog); !ok || log.EventName != "system.engine_access_deny.released" {
							return
						}
					}
					reached = tx.Error == nil
					tx.AddError(waitUntil(tx, *row.ExpiresAt))
				}
				var err error
				if phase == "insert" {
					err = db.Callback().Create().Before("gorm:create").Register(callback, block)
				} else {
					err = db.Callback().Create().After("gorm:create").Register(callback, block)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer db.Callback().Create().Remove(callback)
				if got, err := service.ReleaseDeny(ctx, inputFor(row)); got != nil || !errors.Is(err, ErrDenyReleaseExpired) || !reached {
					t.Fatalf("%s reached=%v got=%+v err=%v", phase, reached, got, err)
				}
				assertCount(t, row.DenyID, 0)
			})
		}
		t.Run("target lock wait rechecks expiry", func(t *testing.T) {
			row := create(t, true)
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			if err := NewRepository(tx).lockFulfillmentTarget(ctx, tenantID, row.CatalogPath); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() { _, err := service.ReleaseDeny(ctx, inputFor(row)); result <- err }()
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
				t.Fatal("release bypassed precise target lock")
			}
			if err := waitUntil(db, *row.ExpiresAt); err != nil {
				t.Fatal(err)
			}
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, ErrDenyReleaseExpired) {
				t.Fatalf("wait expiry=%v", err)
			}
			assertCount(t, row.DenyID, 0)
		})
		t.Run("current Permission expires after audit", func(t *testing.T) {
			row := create(t, false)
			actor, _ := qualified(t, releaseRole, true, 1500*time.Millisecond)
			var deadline time.Time
			if err := db.Raw("SELECT valid_until FROM system.role_assignments WHERE principal_id=? AND tenant_id=? AND role_id=?", actor.PrincipalID, tenantID, releaseRole.ID).Scan(&deadline).Error; err != nil {
				t.Fatal(err)
			}
			const callback = "fixture:deny_release_qualification_expiry"
			reached := false
			if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
				if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "system.engine_access_deny.released" {
					reached = tx.Error == nil
					tx.AddError(waitUntil(tx, deadline))
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Create().Remove(callback)
			input := inputFor(row)
			input.Actor = actor
			if got, err := service.ReleaseDeny(ctx, input); got != nil || !errors.Is(err, commonapi.ErrForbidden) || !reached {
				t.Fatalf("qualification expired reached=%v got=%+v err=%v", reached, got, err)
			}
			assertCount(t, row.DenyID, 0)
		})
		t.Run("audit failure rolls back release", func(t *testing.T) {
			row := create(t, false)
			const callback = "fixture:deny_release_audit_failure"
			if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "system.engine_access_deny.released" {
					tx.AddError(errors.New("Deny release fixture audit failure"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Create().Remove(callback)
			if _, err := service.ReleaseDeny(ctx, inputFor(row)); err == nil {
				t.Fatal("audit failure accepted")
			}
			assertCount(t, row.DenyID, 0)
		})
		t.Run("concurrent identical release writes one history", func(t *testing.T) {
			row := create(t, false)
			input := inputFor(row)
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); _, err := service.ReleaseDeny(ctx, input); results <- err }()
			}
			wg.Wait()
			close(results)
			for err := range results {
				if err != nil {
					t.Fatal(err)
				}
			}
			assertCount(t, row.DenyID, 1)
		})
		t.Run("revoked management rejects both new release and retry", func(t *testing.T) {
			row, another := create(t, false), create(t, false)
			actor, delegation := qualified(t, releaseRole, true, 0)
			input := inputFor(row)
			input.Actor = actor
			if _, err := service.ReleaseDeny(ctx, input); err != nil {
				t.Fatal(err)
			}
			if err := db.Table("system.engine_access_delegations").Where("id=?", delegation.ID).Updates(map[string]any{"status": "revoked", "version": delegation.Version + 1, "revoked_at": time.Now(), "revoked_by_principal_id": adminID, "revoked_reason": "Release qualification test"}).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := service.ReleaseDeny(ctx, input); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("revoked retry=%v", err)
			}
			input.DenyID = another.DenyID
			if _, err := service.ReleaseDeny(ctx, input); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("revoked new release=%v", err)
			}
			assertCount(t, row.DenyID, 1)
			assertCount(t, another.DenyID, 0)
		})
		for table, before := range grantSnapshots {
			var after string
			if err := db.Raw("SELECT COALESCE(jsonb_agg(to_jsonb(g) ORDER BY request_id)::text,'[]') FROM " + table + " g").Scan(&after).Error; err != nil || after != before {
				t.Fatalf("Deny release altered %s: err=%v", table, err)
			}
		}
	})
}
