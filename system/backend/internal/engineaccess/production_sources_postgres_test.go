package engineaccess

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Separate operation budget: adding an initial-production exercise must not
// consume the existing source-rule fixture's deliberately bounded deadline.
func exerciseInitialProductionSources(t *testing.T, db *gorm.DB, acceptor *Service, runtime FulfillmentRuntimeActor,
	base shared.SharingFulfillmentBinding, roles *iam.TenantRoleService, fulfillmentRoleID, adminID int64,
	newUser func(*testing.T, time.Duration) (userProvenance, time.Time), seedDelegation func(*testing.T, int64, time.Time) *Delegation,
) {
	t.Run("initial production observes actual sources and persistent target history without issuing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		defer cancel()
		repo := NewRepository(db)
		identity := iam.NewRepository(db)
		now, err := repo.wallClock(ctx)
		if err != nil {
			t.Fatal(err)
		}
		runtime.TokenExpiresAt = now.Add(time.Minute)
		refresh := func(t *testing.T, user userProvenance) userProvenance {
			t.Helper()
			p, err := identity.GetPrincipal(ctx, user.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			user.AuthorizationVersion = p.AuthorizationVersion
			return user
		}
		commandRole, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: runtime.TenantID, RoleKey: "custom.production_rule_commands", Name: "Production rule commands", PermissionKeys: []string{"system.engine_access_deny.create", "system.engine_access_deny.release", "system.engine_access_grant.revoke"}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		commandUser, expiry := newUser(t, time.Hour)
		seedDelegation(t, commandUser.MembershipID, expiry.Add(-time.Second))
		if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: runtime.TenantID, MembershipID: commandUser.MembershipID, RoleIDs: []int64{commandRole.ID, fulfillmentRoleID}, ActorPrincipalID: adminID, Reason: "Independent production fixture"}); err != nil {
			t.Fatal(err)
		}
		commandUser = refresh(t, commandUser)
		actor := Actor{TenantID: runtime.TenantID, PrincipalID: commandUser.PrincipalID, MembershipID: commandUser.MembershipID, AuthorizationVersion: commandUser.AuthorizationVersion, TokenExpiresAt: now.Add(time.Minute)}
		service := NewService(repo, nil).WithAuthorizationFactsReader(testMetaFacts{paths: []engineplugin.EngineCatalogPath{base.Path}})
		grant := func(t *testing.T, kind string, id int64, expiry *time.Time) uuid.UUID {
			t.Helper()
			binding := base
			binding.RecipientType, binding.RecipientID = kind, id
			if expiry != nil {
				binding.ExpiryMode, binding.ExpiresAt = shared.SharingExpiryAtTime, expiry
			}
			requestID := uuid.New()
			if _, err := acceptor.AcceptFulfillment(ctx, runtime, requestID, binding); err != nil {
				t.Fatal(err)
			}
			if _, err := service.IssueFulfillmentGrant(ctx, runtime, requestID, binding); err != nil {
				t.Fatal(err)
			}
			return requestID
		}
		deny := func(t *testing.T, kind string, id int64, expiry *time.Time) uuid.UUID {
			t.Helper()
			input := CreateDenyInput{Actor: actor, EngineID: int64(base.Path.EngineID), DenyID: uuid.New(), CatalogPath: base.Path, ItemFingerprint: factstest.Binding(base.Path).ItemFingerprint, RecipientType: kind, RecipientID: id, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Production source test"}
			if expiry != nil {
				input.ExpiryMode, input.ExpiresAt = shared.SharingExpiryAtTime, expiry
			}
			if _, err := service.CreateDeny(ctx, input); err != nil {
				t.Fatal(err)
			}
			return input.DenyID
		}
		release := func(t *testing.T, id uuid.UUID) {
			t.Helper()
			if _, err := service.ReleaseDeny(ctx, ReleaseDenyInput{Actor: actor, EngineID: int64(base.Path.EngineID), DenyID: id, Reason: "Fixture release"}); err != nil {
				t.Fatal(err)
			}
		}
		revoke := func(t *testing.T, id uuid.UUID) {
			t.Helper()
			if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(base.Path.EngineID), RequestID: id, Reason: "Fixture withdrawal"}); err != nil {
				t.Fatal(err)
			}
		}
		user, _ := newUser(t, time.Hour)
		target := engineplugin.TabularItemPath(base.Path.EngineID, "schema", "production", "new_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
		receipt := productionBindingReceipt(runtime.TenantID, base.Path.EngineID)
		receipt.Target = target
		receipt.SourceReadSet, _ = engineplugin.NewQueryReadSet(base.Path)
		setOrigin := func() {
			user = refresh(t, user)
			receipt.UserOrigin = &client.TransferProductionUserOrigin{PrincipalID: user.PrincipalID, TenantMembershipID: user.MembershipID, AuthorizationVersion: user.AuthorizationVersion}
		}
		setOrigin()
		productionService := NewService(repo, nil).
			WithTransferProductionReceiptReader(productionReceiptReaderFunc(func(context.Context, uint, string) (*client.TransferProductionReceipt, error) { return &receipt, nil })).
			WithAuthorizationFactsReader(testMetaFacts{paths: []engineplugin.EngineCatalogPath{base.Path, target}})
		prepare := func() *preparedInitialProduction {
			t.Helper()
			prepared, err := productionService.prepareInitialTransferProduction(ctx, runtime.TenantID, receipt.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			return prepared
		}
		observe := func(prepared *preparedInitialProduction, covered, executionIAM, history bool) *initialProductionObservation {
			t.Helper()
			tables := []string{"engine_access_grants", "engine_access_approval_requirements", "engine_access_fulfillment_outcomes", "engine_access_denies", "engine_access_grant_revocations", "audit_logs"}
			counts := make([]int64, len(tables))
			for i, table := range tables {
				if err := db.Table("system." + table).Count(&counts[i]).Error; err != nil {
					t.Fatal(err)
				}
			}
			got, err := repo.observeInitialProduction(ctx, prepared)
			if err != nil || got.sourceRules.Covered != covered || got.executionIAM != executionIAM || got.targetHasHistory != history {
				t.Fatalf("observation=%+v error=%v expected=%v/%v/%v", got, err, covered, executionIAM, history)
			}
			for i, table := range tables {
				var after int64
				if err := db.Table("system." + table).Count(&after).Error; err != nil || after != counts[i] {
					t.Fatalf("read-only production observation wrote %s: %d -> %d err=%v", table, counts[i], after, err)
				}
			}
			return got
		}
		observe(prepare(), false, false, false)
		deadline := time.Now().UTC().Add(20 * time.Minute).Truncate(time.Microsecond)
		sourceGrant := grant(t, "user", user.PrincipalID, &deadline)
		got := observe(prepare(), true, false, false)
		if got.sourceRules.ExpiresAt == nil || !got.sourceRules.ExpiresAt.Equal(deadline) {
			t.Fatal("producer-supplied expiry replaced current source deadline", got)
		}
		executionRole, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: runtime.TenantID, RoleKey: "custom.production_execution", Name: "Production execution fixture", PermissionKeys: []string{"system.execution_authorization.create", "transfer.task.execute"}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		old := prepare()
		if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: runtime.TenantID, MembershipID: user.MembershipID, RoleIDs: []int64{executionRole.ID}, ActorPrincipalID: adminID, Reason: "Execution only, not authorization management"}); err != nil {
			t.Fatal(err)
		}
		observe(old, false, true, false) // Historical version does not survive current IAM changes.
		receipt.ExecutionID = uuid.NewString()
		setOrigin() // A new fixture execution captures the now-current source.
		prepared := prepare()
		observe(prepared, true, true, false)
		blocked := deny(t, "user", user.PrincipalID, nil)
		observe(prepared, false, true, false)
		release(t, blocked)
		observe(prepared, true, true, false)
		boundary := prepared.sources.Bindings[0]
		prepared.sources.Bindings[0].BoundaryHash = strings.Repeat("f", 64)
		if got, err := repo.observeInitialProduction(ctx, prepared); got != nil || !errors.Is(err, errInitialProductionEvidence) {
			t.Fatal("tampered boundary reached source rule observation", got, err)
		}
		prepared.sources.Bindings[0] = boundary
		// Source membership/version still comes from the actual execution,
		// while a target restriction belongs to the stable target identity.
		denied, err := productionService.CreateDeny(ctx, CreateDenyInput{Actor: actor, EngineID: int64(target.EngineID), DenyID: uuid.New(), CatalogPath: target,
			ItemFingerprint: factstest.Binding(target).ItemFingerprint, RecipientType: "user", RecipientID: user.PrincipalID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Restrict produced logical target"})
		if err != nil {
			t.Fatal(err)
		}
		observe(prepared, true, true, true)
		release(t, denied.DenyID)
		observe(prepared, true, true, true) // Released history cannot make a target pristine.
		revoke(t, sourceGrant)
		observe(prepared, false, true, true)
		if _, err := repo.observeInitialProduction(ctx, &preparedInitialProduction{production: prepared.production, sources: prepared.sources, engineVersions: map[int64]int64{}}); !errors.Is(err, errInitialProductionEvidence) {
			t.Fatal("missing engine versions accepted", err)
		}
	})
}
