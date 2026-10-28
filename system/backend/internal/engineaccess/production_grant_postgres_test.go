package engineaccess

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Historical duplicate fixtures alone temporarily disable relation dedup.
// Flush the new deferred FK events before the fixture's ALTER TABLE; this
// validates them normally rather than disabling constraints or production guards.
func restoreGrantRelationFixtureTrigger(tx *gorm.DB) error {
	if err := tx.Exec("SET CONSTRAINTS system.source_grant_initial_outcome IMMEDIATE").Error; err != nil {
		return err
	}
	return tx.Exec("ALTER TABLE system.engine_access_grants ENABLE TRIGGER trg_source_grant_relation").Error
}

func exerciseInitialProductionGrants(t *testing.T, db *gorm.DB, acceptor *Service, catalog FulfillmentRuntimeActor, base shared.SharingFulfillmentBinding,
	roles *iam.TenantRoleService, adminID int64, newUser func(*testing.T, time.Duration) (userProvenance, time.Time)) {
	t.Run("initial production grant terminal lifecycle", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		defer cancel()
		var p iam.Principal
		var member iam.TenantMembership
		if err := db.Raw("SELECT p.* FROM system.principals p JOIN system.service_principals s ON s.id=p.id WHERE s.name='addp-transfer'").Scan(&p).Error; err != nil || p.ID == 0 {
			t.Fatal(err)
		}
		if err := db.Where("tenant_id=? AND principal_id=?", catalog.TenantID, p.ID).Take(&member).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.oauth_clients").Where("client_id='addp-transfer'").Updates(map[string]any{"status": "active", "client_secret_hash": "disposable_fixture_only"}).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Table("system.oauth_clients").Where("client_id='addp-transfer'").Update("status", "disabled")
		if err := db.First(&p, p.ID).Error; err != nil {
			t.Fatal(err)
		}
		actor := ProductionRuntimeActor{Actor: Actor{TenantID: catalog.TenantID, PrincipalID: p.ID, MembershipID: member.ID, AuthorizationVersion: p.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}, ClientID: "addp-transfer"}
		catalog.TokenExpiresAt = time.Now().Add(time.Minute)
		user, _ := newUser(t, time.Hour)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: actor.TenantID, RoleKey: "custom.initial_production_execute", Name: "Production execution only", PermissionKeys: []string{"system.execution_authorization.create", "transfer.task.execute"}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: actor.TenantID, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "No authorization administration"}); err != nil {
			t.Fatal(err)
		}
		principal, err := iam.NewRepository(db).GetPrincipal(ctx, user.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		user.AuthorizationVersion = principal.AuthorizationVersion
		receipt := productionBindingReceipt(actor.TenantID, base.Path.EngineID)
		receipt.SourceReadSet, _ = plugin.NewQueryReadSet(base.Path)
		receipt.UserOrigin = &client.TransferProductionUserOrigin{PrincipalID: user.PrincipalID, TenantMembershipID: user.MembershipID, AuthorizationVersion: user.AuthorizationVersion}
		fresh := func() {
			receipt.ExecutionID = uuid.NewString()
			receipt.Target = plugin.TabularItemPath(base.Path.EngineID, "schema", "production", "new_"+uuid.NewString())
		}
		fresh()
		var remoteErr error
		service := NewService(NewRepository(db), nil).WithTransferProductionReceiptReader(productionReceiptReaderFunc(func(context.Context, uint, string) (*client.TransferProductionReceipt, error) {
			return &receipt, remoteErr
		})).
			WithAuthorizationFactsReader(bindingFactsReaderFunc(func(_ context.Context, _ uint, fp string) (*authorizationfacts.AuthorizationFacts, error) {
				for _, path := range []plugin.EngineCatalogPath{base.Path, receipt.Target} {
					facts := factstest.Single(path)
					if facts.ItemFingerprint == fp {
						return &facts, nil
					}
				}
				return nil, errors.New("unexpected target")
			}))
		denied, err := service.IssueInitialTransferGrant(ctx, actor, receipt.ExecutionID)
		if err != nil || denied.Outcome != "denied" || denied.DenialCode != "source_access_denied" {
			t.Fatalf("denied=%+v error=%v", denied, err)
		}
		// Establish a source Grant after the explicit refusal. Only a fresh
		// production may now qualify; the original refusal must stay terminal.
		binding := base
		binding.RecipientType = "user"
		binding.RecipientID = user.PrincipalID
		expiry := time.Now().UTC().Add(20 * time.Minute).Truncate(time.Microsecond)
		binding.ExpiryMode = shared.SharingExpiryAtTime
		binding.ExpiresAt = &expiry
		sourceID := uuid.New()
		if _, err := acceptor.AcceptFulfillment(ctx, catalog, sourceID, binding); err != nil {
			t.Fatal(err)
		}
		if _, err := service.IssueFulfillmentGrant(ctx, catalog, sourceID, binding); err != nil {
			t.Fatal(err)
		}
		replay, err := service.IssueInitialTransferGrant(ctx, actor, receipt.ExecutionID)
		if err != nil || replay.Outcome != "denied" || !replay.RecordedAt.Equal(denied.RecordedAt) {
			t.Fatal("refusal revived", replay, err)
		}
		fresh()
		executionID := receipt.ExecutionID
		remoteErr = errors.New("temporary dependency failure")
		if _, err := service.IssueInitialTransferGrant(ctx, actor, executionID); err == nil {
			t.Fatal("dependency failure issued a result")
		}
		lookup, err := service.ResolveInitialTransferGrant(ctx, actor, executionID)
		if err != nil || lookup.Found {
			t.Fatal("temporary failure became terminal", lookup, err)
		}
		remoteErr = nil
		// A failed audit must roll back both the Grant and the terminal outcome;
		// retrying authorization must not rerun the producer's successful task.
		failure := errors.New("initial production audit fixture failure")
		const callback = "initial_production_audit_failure"
		if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
			if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "system.engine_access_initial_grant.issued" {
				tx.AddError(failure)
			}
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Callback().Create().Remove(callback) })
		if result, err := service.IssueInitialTransferGrant(ctx, actor, executionID); result != nil || !errors.Is(err, failure) {
			t.Fatalf("audit failed result=%+v error=%v", result, err)
		}
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Fatal(err)
		}
		lookup, err = service.ResolveInitialTransferGrant(ctx, actor, executionID)
		if err != nil || lookup.Found {
			t.Fatal("rolled-back audit left a terminal outcome", lookup, err)
		}
		var rolledBack int64
		if err := db.Model(&sourceGrant{}).Where("request_id=?", productionGrantCommand(actor.TenantID, uuid.MustParse(executionID))).Count(&rolledBack).Error; err != nil || rolledBack != 0 {
			t.Fatalf("rolled-back audit left grants=%d error=%v", rolledBack, err)
		}
		// Concurrent continuations must produce one Grant and one terminal row.
		var wg sync.WaitGroup
		results := make(chan *shared.InitialProductionResult, 2)
		failures := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, e := service.IssueInitialTransferGrant(ctx, actor, executionID)
				results <- r
				failures <- e
			}()
		}
		wg.Wait()
		close(results)
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		var issued *shared.InitialProductionResult
		for r := range results {
			if r == nil || r.Outcome != "issued" || r.Grant == nil || r.Grant.ExpiresAt == nil || !r.Grant.ExpiresAt.Equal(expiry) {
				t.Fatal("bad issued result", r)
			}
			if issued != nil && !issued.Grant.ExpiresAt.Equal(*r.Grant.ExpiresAt) {
				t.Fatal("replay expiry changed")
			}
			issued = r
		}
		var count int64
		if err := db.Model(&sourceGrant{}).Where("tenant_id=? AND producer_owner='transfer' AND production_execution_id=?", actor.TenantID, executionID).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("grants=%d err=%v", count, err)
		}
		var grant sourceGrant
		if err := db.Where("request_id=?", issued.Grant.RequestID).Take(&grant).Error; err != nil {
			t.Fatal(err)
		}
		if grant.DirectPermissionVersion != 0 || grant.CatalogRequestID != nil || grant.OperatorPrincipalID != user.PrincipalID || grant.IssuerPrincipalID == nil || *grant.IssuerPrincipalID != actor.PrincipalID {
			t.Fatal("faked approval or mixed human/machine provenance", grant)
		}
		// Initial grants are ordinary source rules: they cover only the bound
		// DataItem boundary and remain independent of later source withdrawal.
		checkCoverage := func(path plugin.EngineCatalogPath, want bool) {
			t.Helper()
			rules, err := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{TenantID: actor.TenantID, Source: user,
				Targets: []plugin.EngineCatalogPath{path}, Bindings: []authorizationfacts.Binding{factstest.Binding(path)}})
			if err != nil || rules.Covered != want {
				t.Fatalf("covered=%+v want=%v error=%v", rules, want, err)
			}
		}
		checkCoverage(receipt.Target, true)
		humanActor := Actor{TenantID: actor.TenantID, PrincipalID: base.Operator.PrincipalID, MembershipID: base.Operator.MembershipID,
			AuthorizationVersion: base.Operator.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}
		revoke := func(id uuid.UUID) {
			t.Helper()
			if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: humanActor, EngineID: int64(base.Path.EngineID), RequestID: id, Reason: "isolated initial grant fixture withdrawal"}); err != nil {
				t.Fatal(err)
			}
		}
		revoke(sourceID)
		checkCoverage(base.Path, false)
		checkCoverage(receipt.Target, true)
		revoke(issued.Grant.RequestID)
		checkCoverage(receipt.Target, false)
		receipt.ExecutionID = uuid.NewString()
		historyDenied, err := service.IssueInitialTransferGrant(ctx, actor, receipt.ExecutionID)
		if err != nil || historyDenied.Outcome != "denied" || historyDenied.DenialCode != "target_history_exists" {
			t.Fatal("historical target qualified as a new result", historyDenied, err)
		}
		if err := db.Model(&initialProductionOutcome{}).Where("tenant_id=? AND execution_id=?", actor.TenantID, executionID).Update("outcome", "denied").Error; err == nil {
			t.Fatal("terminal outcome was mutable")
		}
		remoteErr = errors.New("offline during historical recovery")
		service.WithAuthorizationFactsReader(nil)
		recovered, err := service.IssueInitialTransferGrant(ctx, actor, executionID)
		if err != nil || recovered.Grant.RequestID != issued.Grant.RequestID || !recovered.Grant.ExpiresAt.Equal(expiry) {
			t.Fatal("recovery required live dependencies or renewed expiry", recovered, err)
		}
		checkCoverage(receipt.Target, false)
		bad := actor
		bad.ClientID = "addp-catalog"
		if _, err := service.ResolveInitialTransferGrant(ctx, bad, executionID); err == nil {
			t.Fatal("wrong runtime read continuation history")
		}
		bad = actor
		bad.TenantID++
		if _, err := service.ResolveInitialTransferGrant(ctx, bad, executionID); err == nil {
			t.Fatal("cross-tenant history returned")
		}
	})
}
