package engineaccess

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/migration"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestFulfillmentArbitrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Error(err)
		}
	})
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
			t.Error(err)
		}
		var n int64
		if err := db.Raw("SELECT count(*) FROM pg_namespace WHERE nspname IN ('system', 'common')").Scan(&n).Error; err != nil || n != 0 {
			t.Errorf("residual=%d err=%v", n, err)
		}
	})
	if err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	// The package gate owns the whole-suite deadline. Independent serial
	// exercises below have their own bounded contexts; the account factory must
	// not inherit a setup budget consumed by earlier exercises.
	ctx := context.Background()
	if deadline, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	setupCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := migration.NewRunner(dsn).Run(setupCtx); err != nil {
		t.Fatal(err)
	}
	if err := migration.NewRunner(dsn).Run(setupCtx); err != nil {
		t.Fatal(err)
	}
	identity := iam.NewRepository(db)
	user := &iam.Principal{PrincipalType: iam.PrincipalTypeUser, Status: iam.PrincipalStatusActive, AuthorizationVersion: 1}
	if err := identity.Transaction(ctx, func(tx *iam.Repository) error {
		if err := tx.CreatePrincipal(ctx, user); err != nil {
			return err
		}
		return tx.CreateUser(ctx, &iam.User{ID: user.ID, DisplayName: "Fulfillment fixture"})
	}); err != nil {
		t.Fatal(err)
	}
	tenant, err := iam.NewPlatformTenantService(identity, time.Now).Create(ctx, iam.CreateTenantInput{
		Code: "fulfillment_fixture", Name: "Fulfillment", InitialAdministratorPrincipalID: user.ID, ActorPrincipalID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	member, err := identity.LockTenantMembership(ctx, tenant.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	currentUser, err := identity.GetPrincipal(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	var callerID int64
	if err := db.Raw("SELECT id FROM system.service_principals WHERE name = 'addp-catalog'").Scan(&callerID).Error; err != nil || callerID == 0 {
		t.Fatalf("caller=%d err=%v", callerID, err)
	}
	tenantID := uint(tenant.ID)
	engine := models.Engine{Name: "No business connection", EngineType: "postgresql", TenantID: &tenantID,
		LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{}, IdentityKey: models.JSONString(`{"host":"fulfillment.invalid","database":"fixture"}`)}
	if err := db.Table("system.engines").Create(&engine).Error; err != nil {
		t.Fatal(err)
	}
	seedDelegation := func(t *testing.T, membershipID int64, expires time.Time) *Delegation {
		t.Helper()
		var now time.Time
		if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			t.Fatal(err)
		}
		delegation := &Delegation{TenantID: tenant.ID, EngineID: int64(engine.ID), TenantMembershipID: membershipID,
			Status: "active", Version: 1, GrantedByPrincipalID: user.ID, GrantedAt: now, ExpiresAt: expires,
			GrantReason: "Disposable coordination fixture only"}
		if err := db.Create(delegation).Error; err != nil {
			t.Fatal(err)
		}
		return delegation
	}
	baseDelegation := seedDelegation(t, member.ID, time.Now().Add(time.Hour))
	newOperator := func(t *testing.T, lifetime time.Duration) (userProvenance, time.Time) {
		t.Helper()
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		principal := &iam.Principal{PrincipalType: iam.PrincipalTypeUser, Status: iam.PrincipalStatusActive, AuthorizationVersion: 1}
		if err := identity.Transaction(ctx, func(tx *iam.Repository) error {
			if err := tx.CreatePrincipal(ctx, principal); err != nil {
				return err
			}
			return tx.CreateUser(ctx, &iam.User{ID: principal.ID, DisplayName: "Limited fulfillment operator"})
		}); err != nil {
			t.Fatal(err)
		}
		var now time.Time
		if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			t.Fatal(err)
		}
		expires := now.Add(lifetime)
		// Fixture setup uses the same database clock as the expiry assertion;
		// host/container clock drift must not change a one-second test boundary.
		result, err := iam.NewTenantMembershipService(identity, func() time.Time { return now }).EstablishMembership(ctx,
			iam.EstablishTenantMembershipInput{TenantID: tenant.ID, PrincipalID: principal.ID,
				SourceType: iam.TenantMembershipSourceManual, ExpiresAt: &expires})
		if err != nil {
			t.Fatal(err)
		}
		current, err := identity.GetPrincipal(ctx, principal.ID)
		if err != nil {
			t.Fatal(err)
		}
		return userProvenance{PrincipalID: principal.ID, MembershipID: result.Membership.ID, AuthorizationVersion: current.AuthorizationVersion}, expires
	}
	newRequest := func() fulfillmentRequest {
		r := testFulfillmentRequest()
		r.TenantID, r.CallerPrincipalID, r.Path = tenant.ID, callerID, engineplugin.TabularItemPath(engine.ID, "schema", "public", "fixture")
		r.RecipientID = user.ID
		r.Operator = userProvenance{PrincipalID: user.ID, MembershipID: member.ID, AuthorizationVersion: currentUser.AuthorizationVersion}
		r.RequirementVersion = 1
		return r
	}
	repo := NewRepository(db)
	exerciseFulfillmentRuntimeRecovery(t, db, newRequest())
	confirmation, _ := newOperator(t, time.Hour)
	confirmationRole, err := iam.NewTenantRoleService(identity, time.Now).CreateRole(ctx, iam.CreateTenantRoleInput{
		TenantID: tenant.ID, RoleKey: "custom.fulfillment_confirmation", Name: "Explicit business confirmation fixture",
		ScopeTypes: []string{"tenant"}, PermissionKeys: []string{"catalog.entry.read", "catalog.sharing_decision.create"},
		ActorPrincipalID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := iam.NewTenantRoleService(identity, time.Now).CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{
		TenantID: tenant.ID, MembershipID: confirmation.MembershipID, RoleIDs: []int64{confirmationRole.ID},
		ScopeType: "tenant", ActorPrincipalID: user.ID, Reason: "Explicit fixture confirmation permission"}); err != nil {
		t.Fatal(err)
	}
	confirmedPrincipal, err := identity.GetPrincipal(ctx, confirmation.PrincipalID)
	if err != nil {
		t.Fatal(err)
	}
	confirmation.AuthorizationVersion = confirmedPrincipal.AuthorizationVersion
	// Only coordination predicates are tested here. This is NOT a production
	// business approval or approval-requirement service.
	verified := func(*Repository) error { return nil }
	actorType, actorContext := iam.PrincipalTypeUser, iam.ContextTypeTenant
	change := func(path engineplugin.EngineCatalogPath, mode string, version int64, verify func(*Repository) error) (*approvalRequirement, error) {
		input := approvalRequirementChange{TenantID: tenant.ID, Path: path, Mode: mode, ExpectedVersion: version, Reason: "Explicit fixture handoff",
			Audit: iam.AuditMetadata{PrincipalID: &user.ID, PrincipalType: &actorType, ContextType: &actorContext, TenantID: &tenant.ID}}
		if mode == approvalModeIndependent {
			input.SuccessorPrincipalID = user.ID
		}
		var row *approvalRequirement
		err := repo.transaction(ctx, func(tx *Repository) error {
			var err error
			row, err = tx.changeApprovalRequirement(ctx, input, verify)
			return err
		})
		return row, err
	}
	if _, err := change(newRequest().Path, approvalModeCatalog, 0, verified); err != nil {
		t.Fatal(err)
	}
	exerciseFulfillmentAcceptance(t, db, newRequest(), confirmation, newOperator, seedDelegation)
	t.Run("real IAM role assignment does not deadlock qualification", func(t *testing.T) {
		operator, _ := newOperator(t, time.Hour)
		roles := iam.NewTenantRoleService(identity, time.Now)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenant.ID,
			RoleKey: "custom.qualification_reader", Name: "Qualification fixture", ScopeTypes: []string{"tenant"},
			PermissionKeys: []string{"catalog.entry.read"}, ActorPrincipalID: user.ID})
		if err != nil {
			t.Fatal(err)
		}
		assertIAMQualificationRace(t, db, tenant.ID, operator, func(raceCtx context.Context) error {
			_, err := roles.CreateAssignments(raceCtx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenant.ID,
				MembershipID: operator.MembershipID, RoleIDs: []int64{role.ID}, ScopeType: "tenant",
				ActorPrincipalID: user.ID, Reason: "Concurrency fixture"})
			return err
		})
	})
	t.Run("real IAM writers share qualification lock order", func(t *testing.T) {
		exerciseIAMQualificationWriters(t, db, identity, tenant.ID, user.ID, newOperator)
	})
	t.Run("batch IAM mutation rechecks affected account set", func(t *testing.T) {
		exerciseIAMMutationSetChanges(t, db, identity, tenant.ID, user.ID, newOperator)
	})
	settle := func(r fulfillmentRequest, accept bool) (*fulfillmentOutcome, error) {
		var result *fulfillmentOutcome
		err := repo.transaction(ctx, func(tx *Repository) error {
			var err error
			result, err = tx.settleFulfillment(ctx, r, accept, confirmation, verified)
			return err
		})
		return result, err
	}
	t.Run("recipient qualification and historical recovery", func(t *testing.T) {
		exerciseFulfillmentRecipients(t, db, identity, newRequest, newOperator, seedDelegation, confirmation)
	})
	t.Run("business confirmer current qualification", func(t *testing.T) {
		exerciseFulfillmentConfirmers(t, db, identity, newRequest, newOperator, seedDelegation)
	})
	t.Run("long-term read still has original five-minute automatic window", func(t *testing.T) {
		r := newRequest()
		r.ExpiryMode, r.ExpiresAt = authorization.SharingExpiryUntilRevoked, nil
		first, err := settle(r, true)
		if err != nil || first.GrantExpiresAt != nil || first.ExpiryMode != authorization.SharingExpiryUntilRevoked ||
			first.Deadline == nil || !first.Deadline.Equal(first.RecordedAt.Add(5*time.Minute)) {
			t.Fatalf("long-term receipt=%+v err=%v", first, err)
		}
		recovered, err := settle(r, true)
		if err != nil || !recovered.RecordedAt.Equal(first.RecordedAt) || !recovered.Deadline.Equal(*first.Deadline) {
			t.Fatalf("long-term retry refreshed window: %+v err=%v", recovered, err)
		}
		r.ExpiryMode, r.ExpiresAt = authorization.SharingExpiryAtTime, testFulfillmentExpiry(time.Now().Add(time.Hour))
		if _, err := settle(r, true); !errors.Is(err, errFulfillmentBinding) {
			t.Fatalf("mode changed in same request: %v", err)
		}
		for _, mutate := range []func(*fulfillmentOutcome){
			func(v *fulfillmentOutcome) { v.ExpiryMode = "" },
			func(v *fulfillmentOutcome) { v.GrantExpiresAt = testFulfillmentExpiry(time.Now().Add(time.Hour)) },
			func(v *fulfillmentOutcome) { deadline := v.RecordedAt.Add(time.Hour); v.Deadline = &deadline },
		} {
			invalid := *first
			invalid.RequestID = uuid.New()
			mutate(&invalid)
			if err := db.Transaction(func(tx *gorm.DB) error { return tx.Create(&invalid).Error }); err == nil {
				t.Fatal("invalid long-term storage bypassed date/window constraints")
			}
		}
	})
	// The repository owns a separate read-only transaction; PostgreSQL rejects
	// accidental outcome/audit writes. The lock-wait case below separately
	// proves that recovery does not take the arbitration's advisory locks.
	readOnly := func(r fulfillmentRequest) (*fulfillmentOutcome, error) {
		return repo.readFulfillment(ctx, r)
	}
	t.Run("recovery miss is not closure and binding is exact", func(t *testing.T) {
		r := newRequest()
		if row, err := readOnly(r); row != nil || !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("missing recovery row=%+v error=%v", row, err)
		}
		first, err := settle(r, true)
		if err != nil || first.Outcome != "accepted" {
			t.Fatalf("a recovery miss blocked later acceptance: %+v %v", first, err)
		}
		for _, mutate := range []func(*fulfillmentRequest){
			func(v *fulfillmentRequest) { v.Operator.PrincipalID++ },
			func(v *fulfillmentRequest) { v.Operator.MembershipID++ },
			func(v *fulfillmentRequest) { v.Operator.AuthorizationVersion++ },
			func(v *fulfillmentRequest) { v.TenantID++ },
			func(v *fulfillmentRequest) { v.CallerPrincipalID++ },
			func(v *fulfillmentRequest) { v.DecisionID = uuid.New() },
			func(v *fulfillmentRequest) { v.RequirementVersion++ },
			func(v *fulfillmentRequest) { v.RecipientType = "department" },
			func(v *fulfillmentRequest) { v.RecipientID++ },
			func(v *fulfillmentRequest) { v.ExpiresAt = testFulfillmentExpiry(v.ExpiresAt.Add(time.Second)) },
			func(v *fulfillmentRequest) { v.ExpiryMode, v.ExpiresAt = authorization.SharingExpiryUntilRevoked, nil },
			func(v *fulfillmentRequest) {
				v.Path = engineplugin.TabularItemPath(engine.ID, "schema", "public", "other")
			},
		} {
			changed := r
			mutate(&changed)
			if row, err := readOnly(changed); row != nil || !errors.Is(err, errFulfillmentBinding) {
				t.Fatalf("changed recovery binding row=%+v error=%v", row, err)
			}
		}
		invalid := r
		invalid.Action = "write"
		if row, err := readOnly(invalid); row != nil || !errors.Is(err, errFulfillmentBinding) {
			t.Fatalf("invalid recovery input row=%+v error=%v", row, err)
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if row, err := repo.readFulfillment(canceled, r); row != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("failed recovery became a missing or closed result: %+v %v", row, err)
		}
		row, err := readOnly(r)
		if err != nil || row.RequestID != first.RequestID || !row.RecordedAt.Equal(first.RecordedAt) || !row.Deadline.Equal(*first.Deadline) {
			t.Fatalf("read-only recovery changed original receipt: %+v %v", row, err)
		}
		var audits int64
		if err := db.Table("system.audit_logs").Where("entity_type = ? AND entity_id = ?", "engine_access_fulfillment", r.RequestID.String()).Count(&audits).Error; err != nil || audits != 1 {
			t.Fatalf("recovery wrote new fulfillment audits=%d error=%v", audits, err)
		}
	})
	t.Run("new acceptance checks human identity but recovery and close do not require it", func(t *testing.T) {
		// The business predicate fixture cannot bypass current System identity.
		r := newRequest()
		r.Operator.AuthorizationVersion++
		if row, err := settle(r, true); row != nil || !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("stale human accepted: %+v %v", row, err)
		}
		if row, err := readOnly(r); row != nil || !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("failed identity produced an outcome: %+v %v", row, err)
		}
		closed, err := settle(r, false)
		if err != nil || closed.Outcome != "closed" {
			t.Fatalf("invalid human stranded closure: %+v %v", closed, err)
		}
		if row, err := readOnly(r); err != nil || row.Outcome != "closed" {
			t.Fatalf("invalid human stranded historical recovery: %+v %v", row, err)
		}
		if _, err := settle(r, true); !errors.Is(err, errFulfillmentClosed) {
			t.Fatalf("closed request reopened: %v", err)
		}
		r = newRequest()
		r.Operator.PrincipalID = callerID
		if _, err := settle(r, true); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("machine caller masqueraded as human: %v", err)
		}
	})
	t.Run("membership expiry after business verification rejects acceptance but cannot strand recovery", func(t *testing.T) {
		limitedOperator := func(lifetime time.Duration) userProvenance {
			t.Helper()
			operator, expires := newOperator(t, lifetime)
			seedDelegation(t, operator.MembershipID, expires)
			return operator
		}
		r := newRequest()
		r.Operator = limitedOperator(time.Second)
		verifiedBusiness := false
		err := repo.transaction(ctx, func(tx *Repository) error {
			_, err := tx.settleFulfillment(ctx, r, true, confirmation, func(tx *Repository) error {
				verifiedBusiness = true
				return tx.db.Exec("SELECT pg_sleep(1.05)").Error
			})
			return err
		})
		if !verifiedBusiness || !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("membership deadline escaped after verification: reached=%t error=%v", verifiedBusiness, err)
		}
		if row, err := readOnly(r); row != nil || !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("expired operator committed acceptance: %+v %v", row, err)
		}
		if row, err := settle(r, false); err != nil || row.Outcome != "closed" {
			t.Fatalf("expired operator stranded close: %+v %v", row, err)
		}
		r = newRequest()
		r.Operator = limitedOperator(time.Second)
		accepted, err := settle(r, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("SELECT pg_sleep(1.05)").Error; err != nil {
			t.Fatal(err)
		}
		for _, accept := range []bool{false, true} {
			row, err := settle(r, accept)
			if err != nil || row.Outcome != "accepted" || !row.RecordedAt.Equal(accepted.RecordedAt) || !row.Deadline.Equal(*accepted.Deadline) {
				t.Fatalf("expired operator changed historical arbitration: %+v %v", row, err)
			}
		}
		if row, err := readOnly(r); err != nil || row.Outcome != "accepted" {
			t.Fatalf("expired operator stranded read-only recovery: %+v %v", row, err)
		}
		r.RequestID = uuid.New()
		if _, err := settle(r, true); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("history restored expired operator qualification: %v", err)
		}
	})
	t.Run("current delegation is required and expiry or revocation cannot strand history", func(t *testing.T) {
		assertRejected := func(r fulfillmentRequest) {
			t.Helper()
			if row, err := settle(r, true); row != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("unqualified management scope accepted: %+v %v", row, err)
			}
			if row, err := readOnly(r); row != nil || !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("rejected scope persisted an outcome: %+v %v", row, err)
			}
			var audits int64
			if err := db.Table("system.audit_logs").Where("entity_type = ? AND entity_id = ?",
				"engine_access_fulfillment", r.RequestID.String()).Count(&audits).Error; err != nil || audits != 0 {
				t.Fatalf("rejected scope persisted audits=%d error=%v", audits, err)
			}
		}
		r := newRequest()
		r.Operator, _ = newOperator(t, time.Hour)
		// Another membership's valid delegation, including the fixture tenant
		// administrator's, never supplies this operator's target scope.
		assertRejected(r)
		if row, err := settle(r, false); err != nil || row.Outcome != "closed" {
			t.Fatalf("missing delegation stranded closure: %+v %v", row, err)
		}
		r = newRequest()
		operator, expires := newOperator(t, time.Hour)
		r.Operator = operator
		delegation := seedDelegation(t, operator.MembershipID, expires)
		accepted, err := settle(r, true)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.transaction(ctx, func(tx *Repository) error {
			now, err := tx.wallClock(ctx)
			if err != nil {
				return err
			}
			return tx.revoke(ctx, delegation, delegation.Version, user.ID, "Fixture scope revoked", now)
		}); err != nil {
			t.Fatal(err)
		}
		for _, accept := range []bool{false, true} {
			row, err := settle(r, accept)
			if err != nil || row.Outcome != "accepted" || !row.RecordedAt.Equal(accepted.RecordedAt) || !row.Deadline.Equal(*accepted.Deadline) {
				t.Fatalf("revocation changed historical arbitration: %+v %v", row, err)
			}
		}
		if row, err := readOnly(r); err != nil || row.Outcome != "accepted" {
			t.Fatalf("revocation stranded read-only recovery: %+v %v", row, err)
		}
		r.RequestID = uuid.New()
		assertRejected(r)
		if row, err := settle(r, false); err != nil || row.Outcome != "closed" {
			t.Fatalf("revoked scope stranded closure: %+v %v", row, err)
		}
		r = newRequest()
		r.Operator, _ = newOperator(t, time.Hour)
		var now time.Time
		if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			t.Fatal(err)
		}
		seedDelegation(t, r.Operator.MembershipID, now.Add(time.Second))
		verifiedBusiness := false
		err = repo.transaction(ctx, func(tx *Repository) error {
			_, err := tx.settleFulfillment(ctx, r, true, confirmation, func(tx *Repository) error {
				verifiedBusiness = true
				return tx.db.Exec("SELECT pg_sleep(1.05)").Error
			})
			return err
		})
		if !verifiedBusiness || !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("delegation expired during verification: reached=%t error=%v", verifiedBusiness, err)
		}
		assertRejected(r)
		if row, err := settle(r, false); err != nil || row.Outcome != "closed" {
			t.Fatalf("expired scope stranded closure: %+v %v", row, err)
		}
		// A second registered engine cannot inherit the original engine's scope.
		otherEngine := engine
		otherEngine.ID, otherEngine.Name = 0, "Other scope"
		otherEngine.IdentityKey = models.JSONString(`{"host":"fulfillment.invalid","database":"other"}`)
		if err := db.Table("system.engines").Create(&otherEngine).Error; err != nil {
			t.Fatal(err)
		}
		r = newRequest()
		r.Path = engineplugin.TabularItemPath(otherEngine.ID, "schema", "public", "fixture")
		assertRejected(r)
		// Engine lifecycle is independently required even when the delegation
		// remains active; roll back this probe to preserve the shared fixture.
		err = repo.transaction(ctx, func(tx *Repository) error {
			if err := tx.db.Exec("UPDATE system.engines SET lifecycle_state = 'deleting' WHERE id = ?", engine.ID).Error; err != nil {
				return err
			}
			_, err := tx.settleFulfillment(ctx, newRequest(), true, confirmation, verified)
			return err
		})
		if !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("inactive engine accepted: %v", err)
		}
	})
	t.Run("revocation committed before a waiting acceptance wins", func(t *testing.T) {
		r := newRequest()
		operator, expires := newOperator(t, time.Hour)
		r.Operator = operator
		delegation := seedDelegation(t, operator.MembershipID, expires)
		writer := db.Begin()
		if writer.Error != nil {
			t.Fatal(writer.Error)
		}
		defer writer.Rollback()
		writerRepo := NewRepository(writer)
		now, err := writerRepo.wallClock(ctx)
		if err != nil {
			t.Fatal(err)
		}
		// Scope storage probe only, not a production API/Permission test. The
		// uncommitted revocation makes acceptance wait on the delegation row.
		if err := writerRepo.revoke(ctx, delegation, delegation.Version, user.ID, "Concurrent fixture revocation", now); err != nil {
			t.Fatal(err)
		}
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		started, done := make(chan int, 1), make(chan error, 1)
		go func() {
			done <- repo.transaction(bounded, func(tx *Repository) error {
				var pid int
				if err := tx.db.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
					return err
				}
				started <- pid
				_, err := tx.settleFulfillment(bounded, r, true, confirmation, verified)
				return err
			})
		}()
		var pid int
		select {
		case pid = <-started:
		case err := <-done:
			t.Fatalf("acceptance did not start: %v", err)
		case <-bounded.Done():
			t.Fatal(bounded.Err())
		}
		blocked := false
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if err := db.Raw("SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid = ? AND NOT granted AND locktype IN ('transactionid', 'tuple'))", pid).Scan(&blocked).Error; err != nil {
				t.Fatal(err)
			}
			if blocked {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !blocked {
			t.Fatal("acceptance did not wait for the uncommitted delegation revocation")
		}
		if err := writer.Commit().Error; err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("committed revocation lost to waiting acceptance: %v", err)
		}
		if row, err := readOnly(r); row != nil || !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("revocation-first race persisted acceptance: %+v %v", row, err)
		}
		if row, err := settle(r, false); err != nil || row.Outcome != "closed" {
			t.Fatalf("revocation-first race stranded closure: %+v %v", row, err)
		}
	})
	t.Run("shared qualification locks allow independent targets and block authority writes", func(t *testing.T) {
		if _, _, _, err := identity.LockUserAuthorizationSource(ctx, user.ID, member.ID, tenant.ID); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("qualification lock outside transaction: %v", err)
		}
		if _, err := repo.lockManagementScope(ctx, tenant.ID, int64(engine.ID), member.ID); !errors.Is(err, errFulfillmentBinding) {
			t.Fatalf("management scope lock outside transaction: %v", err)
		}
		r := newRequest()
		r.Path = engineplugin.TabularItemPath(engine.ID, "schema", "public", uuid.NewString())
		if _, err := change(r.Path, approvalModeCatalog, 0, verified); err != nil {
			t.Fatal(err)
		}
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		if _, err := NewRepository(tx).settleFulfillment(ctx, r, true, confirmation, verified); err != nil {
			t.Fatal(err)
		}
		// The first transaction remains uncommitted and holds all source locks.
		// A different target for the same actor must still be independently usable.
		other := newRequest()
		other.Path = engineplugin.TabularItemPath(engine.ID, "schema", "public", uuid.NewString())
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		err := repo.transaction(bounded, func(second *Repository) error {
			input := approvalRequirementChange{TenantID: tenant.ID, Path: other.Path, Mode: approvalModeCatalog,
				Reason: "Independent target", Audit: iam.AuditMetadata{PrincipalID: &user.ID, PrincipalType: &actorType,
					ContextType: &actorContext, TenantID: &tenant.ID}}
			if _, err := second.changeApprovalRequirement(bounded, input, verified); err != nil {
				return err
			}
			_, err := second.settleFulfillment(bounded, other, true, confirmation, verified)
			return err
		})
		if err != nil {
			t.Fatalf("shared source serialized unrelated target or audit: %v", err)
		}
		// Every source row remains protected, including non-key state/version
		// updates. Roll back probe transactions so the shared fixture stays valid.
		for _, statement := range []string{
			"UPDATE system.principals SET authorization_version = authorization_version + 1 WHERE id = ?",
			"UPDATE system.tenant_memberships SET expires_at = expires_at WHERE id = ?",
			"UPDATE system.tenants SET name = name WHERE id = ?",
			"UPDATE system.engines SET lifecycle_state = 'deleting' WHERE id = ?",
			"UPDATE system.engine_access_delegations SET status = 'revoked', version = version + 1, revoked_by_principal_id = ?, " +
				"revoked_at = clock_timestamp(), revoked_reason = 'Fixture revocation' WHERE id = ?",
		} {
			id := user.ID
			if strings.Contains(statement, "tenant_memberships") {
				id = member.ID
			} else if strings.Contains(statement, "system.tenants") {
				id = tenant.ID
			} else if strings.Contains(statement, "system.engines") {
				id = int64(engine.ID)
			} else if strings.Contains(statement, "engine_access_delegations") {
				id = baseDelegation.ID
			}
			args := []any{id}
			if strings.Contains(statement, "engine_access_delegations") {
				args = []any{user.ID, id}
			}
			var lockError interface{ SQLState() string }
			err := db.Transaction(func(writer *gorm.DB) error {
				if err := writer.Exec("SET LOCAL lock_timeout = '100ms'").Error; err != nil {
					return err
				}
				if err := writer.Exec(statement, args...).Error; err != nil {
					return err
				}
				return errors.New("qualification write unexpectedly passed held identity locks")
			})
			if !errors.As(err, &lockError) || lockError.SQLState() != "55P03" {
				t.Fatalf("IAM source not protected until outcome commit: %s: %v", statement, err)
			}
		}
		if err := tx.Commit().Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Transaction(func(writer *gorm.DB) error {
			if err := writer.Exec("SET LOCAL lock_timeout = '100ms'").Error; err != nil {
				return err
			}
			return writer.Exec("SELECT id FROM system.principals WHERE id = ? FOR UPDATE", user.ID).Error
		}); err != nil {
			t.Fatalf("identity lock survived commit: %v", err)
		}
	})
	t.Run("recovery observes only committed arbitration without waiting for target", func(t *testing.T) {
		r := newRequest()
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		first, err := NewRepository(tx).settleFulfillment(ctx, r, true, confirmation, verified)
		if err != nil {
			t.Fatal(err)
		}
		if row, err := NewRepository(tx).readFulfillment(ctx, r); row != nil || !errors.Is(err, errFulfillmentBinding) {
			t.Fatalf("arbitration's own uncommitted result exposed: %+v %v", row, err)
		}
		// The accepting transaction still holds request and target locks. A
		// bounded readonly query on a separate connection must not wait for them.
		readContext, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if row, err := repo.readFulfillment(readContext, r); row != nil || !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("uncommitted result leaked or recovery waited: %+v %v", row, err)
		}
		if err := tx.Commit().Error; err != nil {
			t.Fatal(err)
		}
		row, err := readOnly(r)
		if err != nil || row.Outcome != "accepted" || !row.RecordedAt.Equal(first.RecordedAt) {
			t.Fatalf("committed result not recovered: %+v %v", row, err)
		}
	})
	t.Run("recovery retains expired receipt after explicit handoff", func(t *testing.T) {
		r := newRequest()
		r.Path = engineplugin.TabularItemPath(engine.ID, "schema", "public", "recovery_handoff")
		if _, err := change(r.Path, approvalModeCatalog, 0, verified); err != nil {
			t.Fatal(err)
		}
		r.ExpiresAt = testFulfillmentExpiry(time.Now().Add(time.Second))
		first, err := settle(r, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := change(r.Path, approvalModeIndependent, 1, verified); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("SELECT pg_sleep(1.05)").Error; err != nil {
			t.Fatal(err)
		}
		row, err := readOnly(r)
		if err != nil || row.Outcome != "accepted" || !row.RecordedAt.Equal(first.RecordedAt) || !row.Deadline.Equal(*first.Deadline) {
			t.Fatalf("handoff or expiry rewrote original receipt: %+v %v", row, err)
		}
		closed := newRequest()
		closed.ExpiresAt = testFulfillmentExpiry(time.Now().Add(-time.Minute))
		if _, err := settle(closed, false); err != nil {
			t.Fatal(err)
		}
		if row, err := readOnly(closed); err != nil || row.Outcome != "closed" || row.Deadline != nil {
			t.Fatalf("expired closed result could not be recovered: %+v %v", row, err)
		}
	})
	t.Run("requirement mutation needs owner transaction actor and qualification verification", func(t *testing.T) {
		input := approvalRequirementChange{TenantID: tenant.ID, Path: engineplugin.TabularItemPath(engine.ID, "schema", "public", "qualification_checks"),
			Mode: approvalModeIndependent, SuccessorPrincipalID: user.ID, Reason: "Explicit initialization",
			Audit: iam.AuditMetadata{PrincipalID: &user.ID, PrincipalType: &actorType, ContextType: &actorContext, TenantID: &tenant.ID}}
		if _, err := repo.changeApprovalRequirement(ctx, input, verified); !errors.Is(err, errApprovalRequirementInput) {
			t.Fatalf("nontransactional requirement mutation: %v", err)
		}
		for _, mutate := range []func(*approvalRequirementChange){
			func(v *approvalRequirementChange) { v.SuccessorPrincipalID = 0 },
			func(v *approvalRequirementChange) { v.Mode = approvalModeCatalog },
			func(v *approvalRequirementChange) { v.Audit.PrincipalID = nil },
			func(v *approvalRequirementChange) { v.Audit.PrincipalType = nil },
			func(v *approvalRequirementChange) { v.Audit.ContextType = nil },
			func(v *approvalRequirementChange) { v.Audit.TenantID = nil },
			func(v *approvalRequirementChange) { v.Reason = " " },
		} {
			invalid := input
			mutate(&invalid)
			err := repo.transaction(ctx, func(tx *Repository) error {
				_, err := tx.changeApprovalRequirement(ctx, invalid, verified)
				return err
			})
			if !errors.Is(err, errApprovalRequirementInput) {
				t.Fatalf("invalid requirement mutation accepted: %v", err)
			}
		}
		failure := errors.New("qualification revoked")
		err := repo.transaction(ctx, func(tx *Repository) error {
			_, err := tx.changeApprovalRequirement(ctx, input, func(*Repository) error { return failure })
			return err
		})
		if !errors.Is(err, failure) {
			t.Fatalf("qualification failure=%v", err)
		}
		path, _ := encodeFulfillmentPath(input.Path)
		if _, err := repo.approvalRequirement(ctx, tenant.ID, int64(engine.ID), path); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("qualification failure initialized authority: %v", err)
		}
	})
	t.Run("missing or independent requirement cannot accept Catalog requests", func(t *testing.T) {
		r := newRequest()
		r.Path = engineplugin.TabularItemPath(engine.ID, "schema", "public", "no_requirement")
		if _, err := settle(r, true); !errors.Is(err, errApprovalRequirementUnavailable) {
			t.Fatalf("missing requirement: %v", err)
		}
		if _, err := change(r.Path, approvalModeIndependent, 0, verified); err != nil {
			t.Fatal(err)
		}
		if _, err := settle(r, true); !errors.Is(err, errApprovalRequirementUnavailable) {
			t.Fatalf("independent requirement: %v", err)
		}
		if _, err := settle(r, false); err != nil {
			t.Fatalf("unknown acceptance remains closable: %v", err)
		}
	})
	t.Run("requirement changes are explicit and audit rolls back with them", func(t *testing.T) {
		r := newRequest()
		r.Path = engineplugin.TabularItemPath(engine.ID, "schema", "public", "explicit_versions")
		if _, err := change(r.Path, approvalModeCatalog, 0, nil); !errors.Is(err, errApprovalRequirementInput) {
			t.Fatalf("missing qualification verifier: %v", err)
		}
		row, err := change(r.Path, approvalModeCatalog, 0, verified)
		if err != nil || row.Version != 1 {
			t.Fatalf("initialize=%+v err=%v", row, err)
		}
		if _, err := change(r.Path, approvalModeIndependent, 0, verified); !errors.Is(err, errApprovalRequirementVersion) {
			t.Fatalf("initialization cannot replace current requirement: %v", err)
		}
		failure := errors.New("interrupt after authority and audit write")
		err = repo.transaction(ctx, func(tx *Repository) error {
			input := approvalRequirementChange{TenantID: tenant.ID, Path: r.Path, ExpectedVersion: 1, Mode: approvalModeIndependent,
				SuccessorPrincipalID: user.ID, Reason: "Explicit exit", Audit: iam.AuditMetadata{PrincipalID: &user.ID,
					PrincipalType: &actorType, ContextType: &actorContext, TenantID: &tenant.ID}}
			if _, err := tx.changeApprovalRequirement(ctx, input, verified); err != nil {
				return err
			}
			return failure
		})
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
		path, _ := encodeFulfillmentPath(r.Path)
		current, err := repo.approvalRequirement(ctx, tenant.ID, int64(engine.ID), path)
		var audits int64
		if err != nil || current.Version != 1 || current.Mode != approvalModeCatalog {
			t.Fatalf("rollback current=%+v err=%v", current, err)
		}
		if err := db.Table("system.audit_logs").Where("entity_type = ? AND entity_id = ?", "engine_access_approval_requirement", row.ID.String()).Count(&audits).Error; err != nil || audits != 1 {
			t.Fatalf("rollback audits=%d err=%v", audits, err)
		}
		if _, err := change(r.Path, approvalModeIndependent, 1, verified); err != nil {
			t.Fatal(err)
		}
		if _, err := change(r.Path, approvalModeCatalog, 1, verified); !errors.Is(err, errApprovalRequirementVersion) {
			t.Fatalf("old retry cannot re-enable after exit: %v", err)
		}
		if row, err := change(r.Path, approvalModeCatalog, 2, verified); err != nil || row.Version != 3 {
			t.Fatalf("re-enable=%+v err=%v", row, err)
		}
		if _, err := settle(r, true); !errors.Is(err, errApprovalRequirementVersion) {
			t.Fatalf("old version cannot consume new activation: %v", err)
		}
		r.RequirementVersion = 3
		if _, err := settle(r, true); err != nil {
			t.Fatalf("explicit current version cannot accept: %v", err)
		}
	})
	t.Run("exact long targets have separate identities and immutable version boundaries", func(t *testing.T) {
		r := newRequest()
		r.Path = engineplugin.TabularItemPath(engine.ID, "schema", "业务.域", " 表/名 "+strings.Repeat("资源", 1400))
		row, err := change(r.Path, approvalModeCatalog, 0, verified)
		if err != nil {
			t.Fatal(err)
		}
		other := r
		other.Path = engineplugin.TabularItemPath(engine.ID, "schema", "业务.域", "表/名"+strings.Repeat("资源", 1400))
		if _, err := settle(other, true); !errors.Is(err, errApprovalRequirementUnavailable) {
			t.Fatalf("trimmed name inherited requirement: %v", err)
		}
		otherRow, err := change(other.Path, approvalModeIndependent, 0, verified)
		if err != nil || row.ID == otherRow.ID {
			t.Fatalf("distinct identity=%+v err=%v", otherRow, err)
		}
		if _, err := settle(r, true); err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			"UPDATE system.engine_access_approval_requirements SET mode = 'independent' WHERE id = ?",
			"UPDATE system.engine_access_approval_requirements SET catalog_path = '{}'::jsonb, version = version + 1 WHERE id = ?",
			"DELETE FROM system.engine_access_approval_requirements WHERE id = ?",
		} {
			if err := db.Exec(statement, row.ID).Error; err == nil {
				t.Fatalf("accepted authority mutation: %s", statement)
			}
		}
		if err := db.Exec("TRUNCATE system.engine_access_approval_requirements").Error; err == nil {
			t.Fatal("accepted authority truncate")
		}
	})
	t.Run("exit and acceptance share the actual target boundary in both orders", func(t *testing.T) {
		for _, acceptFirst := range []bool{false, true} {
			r := newRequest()
			r.Path = engineplugin.TabularItemPath(engine.ID, "schema", "public", uuid.NewString())
			if _, err := change(r.Path, approvalModeCatalog, 0, verified); err != nil {
				t.Fatal(err)
			}
			held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 2)
			hold := func(*Repository) error { close(held); <-release; return nil }
			go func() {
				if acceptFirst {
					done <- repo.transaction(ctx, func(tx *Repository) error {
						_, err := tx.settleFulfillment(ctx, r, true, confirmation, hold)
						return err
					})
				} else {
					_, err := change(r.Path, approvalModeIndependent, 1, hold)
					done <- err
				}
			}()
			select {
			case <-held:
			case <-time.After(5 * time.Second):
				close(release)
				t.Fatal("first operation did not acquire target")
			}
			waitingPID := make(chan int, 1)
			go func() {
				done <- repo.transaction(ctx, func(tx *Repository) error {
					var pid int
					if err := tx.db.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
						return err
					}
					waitingPID <- pid
					if !acceptFirst {
						_, err := tx.settleFulfillment(ctx, r, true, confirmation, verified)
						return err
					}
					input := approvalRequirementChange{TenantID: tenant.ID, Path: r.Path, ExpectedVersion: 1, Mode: approvalModeIndependent,
						SuccessorPrincipalID: user.ID, Reason: "Explicit concurrent exit", Audit: iam.AuditMetadata{PrincipalID: &user.ID,
							PrincipalType: &actorType, ContextType: &actorContext, TenantID: &tenant.ID}}
					_, err := tx.changeApprovalRequirement(ctx, input, verified)
					return err
				})
			}()
			var pid int
			select {
			case pid = <-waitingPID:
			case <-time.After(5 * time.Second):
				close(release)
				t.Fatal("waiting transaction did not begin")
			}
			deadline := time.Now().Add(5 * time.Second)
			blocked := false
			for time.Now().Before(deadline) {
				if err := db.Raw("SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid = ? AND NOT granted AND locktype = 'advisory')", pid).Scan(&blocked).Error; err != nil {
					close(release)
					t.Fatal(err)
				}
				if blocked {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			close(release)
			if !blocked {
				t.Fatal("second operation did not wait on the shared target lock")
			}
			var unavailable int
			for i := 0; i < 2; i++ {
				if err := <-done; errors.Is(err, errApprovalRequirementUnavailable) {
					unavailable++
				} else if err != nil {
					t.Fatal(err)
				}
			}
			if (!acceptFirst && unavailable != 1) || (acceptFirst && unavailable != 0) {
				t.Fatalf("acceptFirst=%t rejected=%d", acceptFirst, unavailable)
			}
			var count int64
			if err := db.Model(&fulfillmentOutcome{}).Where("request_id = ?", r.RequestID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if (!acceptFirst && count != 0) || (acceptFirst && count != 1) {
				t.Fatalf("acceptFirst=%t accepted results=%d", acceptFirst, count)
			}
			if acceptFirst {
				first, err := settle(r, true)
				if err != nil {
					t.Fatal(err)
				}
				again, err := settle(r, false)
				if err != nil || !first.Deadline.Equal(*again.Deadline) {
					t.Fatalf("accepted receipt changed after exit: %+v %v", again, err)
				}
			}
			r.RequestID = uuid.New()
			if _, err := settle(r, true); !errors.Is(err, errApprovalRequirementUnavailable) {
				t.Fatalf("new acceptance after exit: %v", err)
			}
		}
	})
	t.Run("no owner transaction cannot create a result", func(t *testing.T) {
		r := newRequest()
		if _, err := repo.settleFulfillment(ctx, r, true, confirmation, verified); !errors.Is(err, errFulfillmentBinding) {
			t.Fatalf("nontransactional acceptance: %v", err)
		}
		var rows int64
		if err := db.Model(&fulfillmentOutcome{}).Where("request_id = ?", r.RequestID).Count(&rows).Error; err != nil || rows != 0 {
			t.Fatalf("rows=%d err=%v", rows, err)
		}
	})
	t.Run("close defeats delayed accept", func(t *testing.T) {
		r := newRequest()
		if row, err := settle(r, false); err != nil || row.Outcome != "closed" {
			t.Fatalf("close=%v err=%v", row, err)
		}
		if _, err := settle(r, true); !errors.Is(err, errFulfillmentClosed) {
			t.Fatalf("late accept=%v", err)
		}
		if _, err := settle(r, false); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("lost response and close preserve original window", func(t *testing.T) {
		r := newRequest()
		r.ExpiresAt = testFulfillmentExpiry(time.Now().Add(time.Minute))
		first, err := settle(r, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, accept := range []bool{true, false, true} {
			row, err := settle(r, accept)
			if err != nil || row.Outcome != "accepted" || !row.RecordedAt.Equal(first.RecordedAt) || !row.Deadline.Equal(*first.Deadline) || !row.Deadline.Equal(*r.ExpiresAt) {
				t.Fatalf("retry=%v err=%v", row, err)
			}
		}
		changed := r
		changed.RecipientID++
		if _, err := settle(changed, false); !errors.Is(err, errFulfillmentBinding) {
			t.Fatalf("changed recipient=%v", err)
		}
		changed = r
		changed.TenantID++
		if _, err := settle(changed, false); !errors.Is(err, errFulfillmentBinding) {
			t.Fatalf("changed tenant=%v", err)
		}
		changed = r
		changed.Path = engineplugin.TabularItemPath(engine.ID, "schema", "public", "other")
		if _, err := settle(changed, true); !errors.Is(err, errFulfillmentBinding) {
			t.Fatalf("changed path=%v", err)
		}
	})
	t.Run("normal acceptance is capped at five minutes", func(t *testing.T) {
		r := newRequest()
		r.ExpiresAt = testFulfillmentExpiry(time.Now().Add(time.Hour))
		row, err := settle(r, true)
		if err != nil {
			t.Fatal(err)
		}
		if row.Deadline == nil || row.Deadline.Sub(row.RecordedAt) != 5*time.Minute {
			t.Fatalf("unexpected window: %+v", row)
		}
	})
	t.Run("missing verification cannot accept", func(t *testing.T) {
		r := newRequest()
		err := repo.transaction(ctx, func(tx *Repository) error {
			_, err := tx.settleFulfillment(ctx, r, true, confirmation, nil)
			return err
		})
		if !errors.Is(err, errFulfillmentBinding) {
			t.Fatalf("unverified acceptance: %v", err)
		}
		if _, err := settle(r, false); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("concurrent requests have exactly one durable result", func(t *testing.T) {
		for i := 0; i < 12; i++ {
			r := newRequest()
			start := make(chan struct{})
			var wg sync.WaitGroup
			results := make(chan error, 2)
			for _, accept := range []bool{true, false} {
				wg.Add(1)
				go func(accept bool) { defer wg.Done(); <-start; _, err := settle(r, accept); results <- err }(accept)
			}
			close(start)
			wg.Wait()
			close(results)
			for err := range results {
				if err != nil && !errors.Is(err, errFulfillmentClosed) {
					t.Fatal(err)
				}
			}
			var rows, audits int64
			if err := db.Model(&fulfillmentOutcome{}).Where("request_id = ?", r.RequestID).Count(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Table("system.audit_logs").Where("entity_type = ? AND entity_id = ?", "engine_access_fulfillment", r.RequestID.String()).Count(&audits).Error; err != nil {
				t.Fatal(err)
			}
			if rows != 1 || audits != 1 {
				t.Fatalf("results=%d audits=%d", rows, audits)
			}
		}
	})
	t.Run("failed validation and rollback never commit acceptance", func(t *testing.T) {
		for _, failAfterInsert := range []bool{false, true} {
			r := newRequest()
			failure := errors.New("interrupted transaction")
			err := repo.transaction(ctx, func(tx *Repository) error {
				verify := func(*Repository) error {
					if !failAfterInsert {
						return failure
					}
					return nil
				}
				if _, err := tx.settleFulfillment(ctx, r, true, confirmation, verify); err != nil {
					return err
				}
				return failure
			})
			if !errors.Is(err, failure) {
				t.Fatal(err)
			}
			var rows int64
			if err := db.Model(&fulfillmentOutcome{}).Where("request_id = ?", r.RequestID).Count(&rows).Error; err != nil || rows != 0 {
				t.Fatalf("rows=%d err=%v", rows, err)
			}
			if _, err := settle(r, false); err != nil {
				t.Fatalf("rollback did not release lock: %v", err)
			}
		}
	})
	t.Run("expiry after verification is checked using wall clock", func(t *testing.T) {
		r := newRequest()
		var now time.Time
		if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			t.Fatal(err)
		}
		r.ExpiresAt = testFulfillmentExpiry(now.Add(100 * time.Millisecond))
		err := repo.transaction(ctx, func(tx *Repository) error {
			_, err := tx.settleFulfillment(ctx, r, true, confirmation, func(tx *Repository) error { return tx.db.Exec("SELECT pg_sleep(0.15)").Error })
			return err
		})
		if !errors.Is(err, errFulfillmentExpired) {
			t.Fatalf("expiry=%v", err)
		}
		if _, err := settle(r, false); err != nil {
			t.Fatalf("expired unaccepted request cannot be closed: %v", err)
		}
	})
	t.Run("terminal outcome rejects direct mutation", func(t *testing.T) {
		r := newRequest()
		if _, err := settle(r, true); err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			"UPDATE system.engine_access_fulfillment_outcomes SET recorded_at = clock_timestamp() WHERE request_id = ?",
			"DELETE FROM system.engine_access_fulfillment_outcomes WHERE request_id = ?",
		} {
			if err := db.Exec(statement, r.RequestID).Error; err == nil {
				t.Fatal("terminal outcome mutated")
			}
		}
		if err := db.Exec("TRUNCATE system.engine_access_fulfillment_outcomes").Error; err == nil {
			t.Fatal("terminal history truncated")
		}
	})
}
