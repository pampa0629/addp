package engineaccess

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Runs inside the existing disposable-PostgreSQL arbitration fixture and its
// standard gate. Acceptance uses the production trusted-basis service; only
// the remote owner is a fixture. Issuance has no Catalog reader at all.
func exerciseFulfillmentGrants(t *testing.T, db *gorm.DB, acceptor *Service, actor FulfillmentRuntimeActor,
	base shared.SharingFulfillmentBinding, roles *iam.TenantRoleService, roleID, adminID int64,
	newOperator func(*testing.T, time.Duration) (userProvenance, time.Time), seedDelegation func(*testing.T, int64, time.Time) *Delegation,
) {
	t.Run("accepted grant issuance", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		issuer := NewService(NewRepository(db), nil)
		prepare := func(t *testing.T, binding shared.SharingFulfillmentBinding) uuid.UUID {
			t.Helper()
			id := uuid.New()
			if _, err := acceptor.AcceptFulfillment(ctx, actor, id, binding); err != nil {
				t.Fatal(err)
			}
			// Independent scenarios no longer leave an active duplicate relation
			// behind. Cleanup uses the production withdrawal command, not deletion.
			t.Cleanup(func() {
				g, err := issuer.repository.findSourceGrant(ctx, id)
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return
				}
				if err != nil {
					t.Error(err)
					return
				}
				if _, err := issuer.repository.findGrantRevocation(ctx, id); err == nil {
					return
				}
				p, err := iam.NewRepository(db).GetPrincipal(ctx, base.Operator.PrincipalID)
				if err != nil {
					t.Error(err)
					return
				}
				_, err = issuer.RevokeGrant(ctx, RevokeGrantInput{Actor: Actor{TenantID: actor.TenantID,
					PrincipalID: base.Operator.PrincipalID, MembershipID: base.Operator.MembershipID,
					AuthorizationVersion: p.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)},
					EngineID: g.EngineID, RequestID: id, Reason: "Close isolated issuance fixture"})
				if err != nil && !errors.Is(err, ErrGrantRevocationExpired) {
					t.Error(err)
				}
			})
			return id
		}
		assertCount := func(t *testing.T, id uuid.UUID, want int64) {
			t.Helper()
			for _, table := range []string{"system.engine_access_grants", "system.audit_logs"} {
				query := db.Table(table)
				if strings.HasSuffix(table, "audit_logs") {
					query = query.Where("entity_type='engine_access_grant' AND entity_id=?", id.String())
				} else {
					query = query.Where("request_id=?", id)
				}
				var count int64
				if err := query.Count(&count).Error; err != nil || count != want {
					t.Fatalf("%s count=%d want=%d err=%v", table, count, want, err)
				}
			}
		}
		qualified := func(t *testing.T, permissionLifetime time.Duration) (shared.SharingFulfillmentBinding, *Delegation) {
			t.Helper()
			operator, expires := newOperator(t, time.Hour)
			delegation := seedDelegation(t, operator.MembershipID, expires.Add(-time.Second))
			input := iam.CreateTenantRoleAssignmentsInput{TenantID: actor.TenantID, MembershipID: operator.MembershipID,
				RoleIDs: []int64{roleID}, ScopeType: "tenant", ActorPrincipalID: adminID, Reason: "Explicit grant fixture"}
			if permissionLifetime > 0 {
				expires := time.Now().Add(permissionLifetime)
				input.ValidUntil = &expires
			}
			if _, err := roles.CreateAssignments(ctx, input); err != nil {
				t.Fatal(err)
			}
			current, err := iam.NewRepository(db).GetPrincipal(ctx, operator.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			binding := base
			binding.Operator = shared.SharingFulfillmentOperator{PrincipalID: operator.PrincipalID,
				MembershipID: operator.MembershipID, AuthorizationVersion: current.AuthorizationVersion}
			return binding, delegation
		}
		t.Run("public service projects only committed issuance history", func(t *testing.T) {
			id := prepare(t, base)
			missing, err := issuer.ResolveFulfillmentGrant(ctx, actor, id, base)
			if err != nil || missing == nil || missing.Found || missing.Grant != nil {
				t.Fatalf("unissued history=%+v %v", missing, err)
			}
			assertCount(t, id, 0)
			original, err := issuer.IssueFulfillmentGrant(ctx, actor, id, base)
			if err != nil || original == nil || original.RequestID != id || original.GrantedAt.IsZero() {
				t.Fatalf("issuance=%+v %v", original, err)
			}
			retried, err := issuer.IssueFulfillmentGrant(ctx, actor, id, base)
			if err != nil || retried == nil || retried.RequestID != original.RequestID || !retried.GrantedAt.Equal(original.GrantedAt) {
				t.Fatalf("retry=%+v %v", retried, err)
			}
			lookup, err := issuer.ResolveFulfillmentGrant(ctx, actor, id, base)
			if err != nil || lookup == nil || !lookup.Found || lookup.Grant == nil || lookup.Grant.RequestID != original.RequestID || !lookup.Grant.GrantedAt.Equal(original.GrantedAt) {
				t.Fatalf("lookup=%+v %v", lookup, err)
			}
			assertCount(t, id, 1)
			duplicate := prepare(t, base)
			if _, err := issuer.IssueFulfillmentGrant(ctx, actor, duplicate, base); !errors.Is(err, ErrGrantRelationExists) {
				t.Fatalf("Catalog duplicate=%v", err)
			}
			assertCount(t, duplicate, 0)
			canonical, err := issuer.repository.findFulfillmentGrant(ctx, id)
			if err != nil || canonical.CatalogRequestID == nil || *canonical.CatalogRequestID != id ||
				canonical.TenantID != actor.TenantID || canonical.EngineID != int64(base.Path.EngineID) ||
				canonical.RecipientType != base.RecipientType || canonical.RecipientID != base.RecipientID ||
				canonical.Action != base.Action || canonical.RequirementVersion != base.RequirementVersion ||
				canonical.OperatorPrincipalID != base.Operator.PrincipalID || canonical.OperatorMembershipID != base.Operator.MembershipID ||
				canonical.OperatorAuthorizationVersion != base.Operator.AuthorizationVersion ||
				!shared.EqualSharingExpiry(canonical.ExpiryMode, canonical.ExpiresAt, base.ExpiryMode, base.ExpiresAt) {
				t.Fatalf("Catalog rule parameters did not reach the canonical Grant: %+v %v", canonical, err)
			}
			changed := base
			changed.RecipientID++
			if result, err := issuer.ResolveFulfillmentGrant(ctx, actor, id, changed); result != nil || !errors.Is(err, ErrFulfillmentBindingConflict) {
				t.Fatalf("binding conflict=%+v %v", result, err)
			}
		})
		t.Run("read-only lookup never settles or issues", func(t *testing.T) {
			missing := uuid.New()
			accepted := prepare(t, base)
			closed := uuid.New()
			if _, err := issuer.CloseFulfillment(ctx, actor, closed, base); err != nil {
				t.Fatal(err)
			}
			before, err := issuer.ResolveFulfillment(ctx, actor, accepted, base)
			if err != nil || before.Resolution == nil {
				t.Fatalf("accepted history=%+v %v", before, err)
			}
			for _, id := range []uuid.UUID{missing, accepted, closed} {
				if g, err := issuer.resolveAcceptedGrant(ctx, actor, id, base); g != nil || err != nil {
					t.Fatalf("lookup issued/failed=%+v %v", g, err)
				}
				assertCount(t, id, 0)
			}
			after, err := issuer.ResolveFulfillment(ctx, actor, accepted, base)
			if err != nil || after.Resolution == nil || after.Resolution.Outcome != before.Resolution.Outcome ||
				!after.Resolution.RecordedAt.Equal(before.Resolution.RecordedAt) || !after.Resolution.Deadline.Equal(*before.Resolution.Deadline) {
				t.Fatalf("lookup changed acceptance=%+v %v", after, err)
			}
			missingOutcome, err := issuer.ResolveFulfillment(ctx, actor, missing, base)
			if err != nil || missingOutcome.Found {
				t.Fatalf("lookup settled missing=%+v %v", missingOutcome, err)
			}
		})
		t.Run("lookup does not expose uncommitted grant or take arbitration locks", func(t *testing.T) {
			id := prepare(t, base)
			request, err := recoveryRequest(actor, id, base)
			if err != nil {
				t.Fatal(err)
			}
			path, _, err := request.encode()
			if err != nil {
				t.Fatal(err)
			}
			tx := db.WithContext(ctx).Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			if err := NewRepository(tx).lockFulfillmentTarget(ctx, actor.TenantID, path); err != nil {
				t.Fatal(err)
			}
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "engine_access_request|"+id.String()).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Create(&sourceGrant{RequestID: id, ApprovalMode: approvalModeCatalog}).Error; err != nil {
				t.Fatal(err)
			}
			if g, err := NewRepository(tx).readFulfillmentGrant(ctx, request); g != nil || !errors.Is(err, errFulfillmentBinding) {
				t.Fatalf("own uncommitted grant exposed=%+v %v", g, err)
			}
			readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
			defer readCancel()
			if g, err := issuer.resolveAcceptedGrant(readCtx, actor, id, base); g != nil || err != nil {
				t.Fatalf("lookup waited on arbitration or exposed uncommitted grant=%+v %v", g, err)
			}
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			assertCount(t, id, 0)
			original, err := issuer.writeAcceptedGrant(ctx, actor, id, base)
			if err != nil {
				t.Fatal(err)
			}
			recovered, err := issuer.resolveAcceptedGrant(ctx, actor, id, base)
			if err != nil || recovered == nil || !recovered.GrantedAt.Equal(original.GrantedAt) {
				t.Fatalf("committed grant not recovered=%+v %v", recovered, err)
			}
			assertCount(t, id, 1)
		})
		t.Run("lookup works with one connection and enforces read-only transactions", func(t *testing.T) {
			id := prepare(t, base)
			original, err := issuer.writeAcceptedGrant(ctx, actor, id, base)
			if err != nil {
				t.Fatal(err)
			}
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			previous := pool.Stats().MaxOpenConnections
			pool.SetMaxOpenConns(1)
			defer pool.SetMaxOpenConns(previous)
			readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
			defer readCancel()
			g, err := issuer.resolveAcceptedGrant(readCtx, actor, id, base)
			if err != nil || g == nil || !g.GrantedAt.Equal(original.GrantedAt) {
				t.Fatalf("single connection lookup=%+v %v", g, err)
			}
			err = NewRepository(db).readCommitted(ctx, func(tx *Repository) error {
				return tx.db.Create(&sourceGrant{RequestID: uuid.New(), ApprovalMode: approvalModeCatalog}).Error
			})
			if err == nil || !strings.Contains(err.Error(), "read-only transaction") {
				t.Fatalf("history transaction allows writes: %v", err)
			}
			assertCount(t, id, 1)
		})
		t.Run("lookup preserves database errors but hides them from unqualified caller", func(t *testing.T) {
			id := prepare(t, base)
			failure := errors.New("fixture grant history query unavailable")
			const callback = "fixture:grant_history_failure"
			if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "system.engine_access_grants" {
					tx.AddError(failure)
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Query().Remove(callback)
			if g, err := issuer.resolveAcceptedGrant(ctx, actor, id, base); g != nil || !errors.Is(err, failure) {
				t.Fatalf("database failure became missing=%+v %v", g, err)
			}
			badActor := actor
			badActor.AuthorizationVersion++
			if g, err := issuer.resolveAcceptedGrant(ctx, badActor, id, base); g != nil || !errors.Is(err, commonapi.ErrForbidden) || errors.Is(err, failure) {
				t.Fatalf("unqualified caller saw database state=%+v %v", g, err)
			}
		})
		t.Run("concurrent same parameters issue once without owner IO", func(t *testing.T) {
			id := prepare(t, base)
			type response struct {
				grant *sourceGrant
				err   error
			}
			results := make(chan response, 6)
			for i := 0; i < cap(results); i++ {
				go func() { g, err := issuer.writeAcceptedGrant(ctx, actor, id, base); results <- response{g, err} }()
			}
			var original time.Time
			for i := 0; i < cap(results); i++ {
				result := <-results
				if result.err != nil || result.grant == nil || result.grant.RequestID != id || result.grant.GrantedAt.IsZero() {
					t.Fatalf("issue=%+v %v", result.grant, result.err)
				}
				if !original.IsZero() && !original.Equal(result.grant.GrantedAt) {
					t.Fatal("retry replaced issuance time")
				}
				original = result.grant.GrantedAt
			}
			assertCount(t, id, 1)
			for _, mutate := range []func(*shared.SharingFulfillmentBinding){
				func(b *shared.SharingFulfillmentBinding) { b.Operator.AuthorizationVersion++ },
				func(b *shared.SharingFulfillmentBinding) { b.RequirementVersion++ },
				func(b *shared.SharingFulfillmentBinding) { b.DecisionID = uuid.New() },
				func(b *shared.SharingFulfillmentBinding) { b.RecipientID = 9007199254740993 },
				func(b *shared.SharingFulfillmentBinding) { b.Path.Segments[len(b.Path.Segments)-1].Name += "_other" },
			} {
				bad := base
				bad.Path.Segments = append(bad.Path.Segments[:0:0], bad.Path.Segments...)
				mutate(&bad)
				if g, err := issuer.writeAcceptedGrant(ctx, actor, id, bad); g != nil || !errors.Is(err, commonapi.ErrConflict) {
					t.Fatalf("changed binding issued/recovered=%+v %v", g, err)
				}
				if g, err := issuer.resolveAcceptedGrant(ctx, actor, id, bad); g != nil || !errors.Is(err, commonapi.ErrConflict) {
					t.Fatalf("lookup exposed changed binding=%+v %v", g, err)
				}
				badActor := actor
				badActor.AuthorizationVersion++
				if g, err := issuer.resolveAcceptedGrant(ctx, badActor, id, bad); g != nil || !errors.Is(err, commonapi.ErrForbidden) {
					t.Fatalf("unqualified lookup exposed binding conflict=%+v %v", g, err)
				}
			}
			badActor := actor
			badActor.AuthorizationVersion++
			if g, err := issuer.writeAcceptedGrant(ctx, badActor, id, base); g != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("stale runtime saw grant=%+v %v", g, err)
			}
			badActor = actor
			badActor.TokenExpiresAt = time.Now().Add(-time.Second)
			if g, err := issuer.writeAcceptedGrant(ctx, badActor, id, base); g != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("expired runtime saw grant=%+v %v", g, err)
			}
			for _, statement := range []string{
				"UPDATE system.engine_access_grants SET granted_at=clock_timestamp() WHERE request_id=?",
				"DELETE FROM system.engine_access_grants WHERE request_id=?",
			} {
				if err := db.Exec(statement, id).Error; err == nil {
					t.Fatal("issuance history is mutable")
				}
			}
			if err := db.Exec("TRUNCATE system.engine_access_grants").Error; err == nil {
				t.Fatal("issuance history can be truncated")
			}
		})
		t.Run("missing and closed receipts cannot issue", func(t *testing.T) {
			id := uuid.New()
			if g, err := issuer.IssueFulfillmentGrant(ctx, actor, id, base); g != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatal("missing receipt issued")
			}
			if _, err := acceptor.CloseFulfillment(ctx, actor, id, base); err != nil {
				t.Fatal(err)
			}
			if g, err := issuer.IssueFulfillmentGrant(ctx, actor, id, base); g != nil || !errors.Is(err, commonapi.ErrConflict) || !errors.Is(err, ErrFulfillmentAlreadyClosed) {
				t.Fatalf("closed issued=%+v %v", g, err)
			}
			if err := db.Create(&sourceGrant{RequestID: id, ApprovalMode: approvalModeCatalog}).Error; err == nil {
				t.Fatal("database accepted closed receipt")
			}
			assertCount(t, id, 0)
		})
		t.Run("historical retry survives window and human expiry", func(t *testing.T) {
			binding, _ := qualified(t, 0)
			expires := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
			binding.ExpiryMode, binding.ExpiresAt = shared.SharingExpiryAtTime, &expires
			id := prepare(t, binding)
			original, err := issuer.writeAcceptedGrant(ctx, actor, id, binding)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Table("system.principals").Where("id=?", binding.Operator.PrincipalID).Update("status", "suspended").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("SELECT pg_sleep(1.05)").Error; err != nil {
				t.Fatal(err)
			}
			retried, err := issuer.writeAcceptedGrant(ctx, actor, id, binding)
			if err != nil || retried == nil || !retried.GrantedAt.Equal(original.GrantedAt) {
				t.Fatalf("historical result=%+v %v", retried, err)
			}
			recovered, err := issuer.resolveAcceptedGrant(ctx, actor, id, binding)
			if err != nil || recovered == nil || !recovered.GrantedAt.Equal(original.GrantedAt) {
				t.Fatalf("lookup lost expired historical result=%+v %v", recovered, err)
			}
			assertCount(t, id, 1)
			// The reference preserves the original at_time; recovery cannot renew it.
			var row fulfillmentOutcome
			if err := db.Where("request_id=?", id).Take(&row).Error; err != nil || row.ExpiryMode != binding.ExpiryMode || row.GrantExpiresAt == nil || !row.GrantExpiresAt.Equal(expires) {
				t.Fatalf("expiry changed=%+v %v", row, err)
			}
		})
		for _, failure := range []string{"operator version", "operator status", "recipient status", "delegation revoked", "permission expired"} {
			t.Run(failure, func(t *testing.T) {
				lifetime := time.Duration(0)
				if failure == "permission expired" {
					lifetime = time.Second
				}
				binding, delegation := qualified(t, lifetime)
				if failure == "recipient status" {
					recipient, _ := newOperator(t, time.Hour)
					binding.RecipientID = recipient.PrincipalID
				}
				id := prepare(t, binding)
				var err error
				switch failure {
				case "operator version":
					err = db.Exec("UPDATE system.principals SET authorization_version=authorization_version+1 WHERE id=?", binding.Operator.PrincipalID).Error
				case "operator status":
					err = db.Table("system.principals").Where("id=?", binding.Operator.PrincipalID).Update("status", "suspended").Error
				case "recipient status":
					err = db.Table("system.principals").Where("id=?", binding.RecipientID).Update("status", "suspended").Error
				case "delegation revoked":
					err = issuer.repository.transaction(ctx, func(tx *Repository) error {
						now, err := tx.wallClock(ctx)
						if err != nil {
							return err
						}
						return tx.revoke(ctx, delegation, delegation.Version, adminID, "Fixture revocation", now)
					})
				case "permission expired":
					err = db.Exec("SELECT pg_sleep(1.05)").Error
				}
				if err != nil {
					t.Fatal(err)
				}
				if g, err := issuer.writeAcceptedGrant(ctx, actor, id, binding); g != nil || !errors.Is(err, commonapi.ErrForbidden) {
					t.Fatalf("lost qualification issued=%+v %v", g, err)
				}
				assertCount(t, id, 0)
			})
		}
		t.Run("audit failure rolls issuance back and original retry succeeds", func(t *testing.T) {
			id := prepare(t, base)
			failure := errors.New("fixture audit failure")
			const callback = "grant_fixture_audit_failure"
			if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "system.engine_access_grant.issued" {
					tx.AddError(failure)
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Callback().Create().Remove(callback) })
			if g, err := issuer.writeAcceptedGrant(ctx, actor, id, base); g != nil || !errors.Is(err, failure) {
				t.Fatalf("audit failed=%+v %v", g, err)
			}
			assertCount(t, id, 0)
			if err := db.Callback().Create().Remove(callback); err != nil {
				t.Fatal(err)
			}
			if _, err := issuer.writeAcceptedGrant(ctx, actor, id, base); err != nil {
				t.Fatal(err)
			}
			assertCount(t, id, 1)
		})
		t.Run("audit delay cannot carry a new grant past the deadline", func(t *testing.T) {
			binding := base
			expires := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
			binding.ExpiryMode, binding.ExpiresAt = shared.SharingExpiryAtTime, &expires
			id := prepare(t, binding)
			const callback = "grant_fixture_audit_delay"
			if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
				if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "system.engine_access_grant.issued" {
					if err := tx.Session(&gorm.Session{NewDB: true}).Exec("SELECT pg_sleep(1.05)").Error; err != nil {
						tx.AddError(err)
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Callback().Create().Remove(callback) })
			if g, err := issuer.writeAcceptedGrant(ctx, actor, id, binding); g != nil || !errors.Is(err, errFulfillmentExpired) {
				t.Fatalf("audit wait escaped deadline=%+v %v", g, err)
			}
			assertCount(t, id, 0)
		})
		t.Run("empty project group stays organizational recipient", func(t *testing.T) {
			organizations := iam.NewOrganizationService(iam.NewRepository(db), time.Now)
			group, err := organizations.CreateProjectGroup(ctx, iam.CreateProjectGroupInput{TenantID: actor.TenantID,
				ActorPrincipalID: adminID, Code: "grant_fixture_group", Name: "Grant fixture group"})
			if err != nil {
				t.Fatal(err)
			}
			binding := base
			binding.RecipientType, binding.RecipientID = "project_group", group.ID
			id := prepare(t, binding)
			pendingID := prepare(t, binding)
			if _, err := issuer.writeAcceptedGrant(ctx, actor, id, binding); err != nil {
				t.Fatal(err)
			}
			assertCount(t, id, 1)
			var row fulfillmentOutcome
			if err := db.Where("request_id=?", id).Take(&row).Error; err != nil || !strings.Contains(string(row.Binding), "project_group") {
				t.Fatalf("organization flattened=%+v %v", row, err)
			}
			if _, err := organizations.CloseProjectGroup(ctx, iam.CloseProjectGroupInput{TenantID: actor.TenantID,
				ProjectGroupID: group.ID, Version: group.Version, ActorPrincipalID: adminID, Reason: "Fixture closure"}); err != nil {
				t.Fatal(err)
			}
			if g, err := issuer.writeAcceptedGrant(ctx, actor, pendingID, binding); g != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("closed recipient newly issued=%+v %v", g, err)
			}
			assertCount(t, pendingID, 0)
			if _, err := issuer.writeAcceptedGrant(ctx, actor, id, binding); err != nil {
				t.Fatal(err)
			}
			assertCount(t, id, 1)
		})
		t.Run("accepted issuance survives later approval arrangement change", func(t *testing.T) {
			binding := base
			binding.Path.Segments = append(binding.Path.Segments[:0:0], binding.Path.Segments...)
			binding.Path.Segments[len(binding.Path.Segments)-1].Name = uuid.NewString()
			principalType, contextType := iam.PrincipalTypeUser, iam.ContextTypeTenant
			input := approvalRequirementChange{TenantID: actor.TenantID, Path: binding.Path, Mode: approvalModeCatalog,
				Reason: "Explicit grant fixture arrangement", Audit: iam.AuditMetadata{PrincipalID: &adminID,
					PrincipalType: &principalType, ContextType: &contextType, TenantID: &actor.TenantID}}
			change := func() error {
				return issuer.repository.transaction(ctx, func(tx *Repository) error {
					_, err := tx.changeApprovalRequirement(ctx, input, func(*Repository) error { return nil })
					return err
				})
			}
			if err := change(); err != nil {
				t.Fatal(err)
			}
			id := prepare(t, binding)
			input.Mode, input.ExpectedVersion, input.SuccessorPrincipalID = approvalModeIndependent, 1, adminID
			if err := change(); err != nil {
				t.Fatal(err)
			}
			if _, err := issuer.writeAcceptedGrant(ctx, actor, id, binding); err != nil {
				t.Fatal(err)
			}
			assertCount(t, id, 1)
		})
		t.Run("target lock wait cannot cross original deadline", func(t *testing.T) {
			binding := base
			expires := time.Now().UTC().Add(2 * time.Second).Truncate(time.Microsecond)
			binding.ExpiryMode, binding.ExpiresAt = shared.SharingExpiryAtTime, &expires
			id := prepare(t, binding)
			path, err := shared.EncodeSharingTarget(binding.Path)
			if err != nil {
				t.Fatal(err)
			}
			holder := db.WithContext(ctx).Begin()
			defer holder.Rollback()
			if err := NewRepository(holder).lockFulfillmentTarget(ctx, actor.TenantID, path); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := issuer.writeAcceptedGrant(ctx, actor, id, binding); done <- err }()
			blocked := false
			for until := time.Now().Add(time.Second); time.Now().Before(until); {
				if err := db.Raw("SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))").Scan(&blocked).Error; err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !blocked {
				t.Fatal("issuer did not wait at the exact target")
			}
			if err := db.Exec("SELECT pg_sleep(GREATEST(0, EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp())))+0.05)", expires).Error; err != nil {
				t.Fatal(err)
			}
			if err := holder.Commit().Error; err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, errFulfillmentExpired) {
				t.Fatalf("wait escaped window: %v", err)
			}
			// Even a backdated direct insert is rejected by the database trigger.
			if err := db.Create(&sourceGrant{RequestID: id, ApprovalMode: approvalModeCatalog, GrantedAt: expires.Add(-time.Minute)}).Error; err == nil {
				t.Fatal("backdated grant escaped window")
			}
			assertCount(t, id, 0)
		})
	})
}
