package engineaccess

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/production"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Historical duplicate fixtures alone temporarily disable relation dedup.
// Flush the new deferred FK events before the fixture's ALTER TABLE; this
// validates them normally rather than disabling constraints or production guards.
func restoreGrantRelationFixtureTrigger(tx *gorm.DB) error {
	if err := tx.Exec("SET CONSTRAINTS system.source_grant_initial_outcome, system.source_grant_upload_outcome IMMEDIATE").Error; err != nil {
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
		service := NewService(NewRepository(db), nil).withTestProductionReader(productionReceiptReaderFunc(func(context.Context, uint, string) (*client.TransferProductionReceipt, error) {
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
		denied, err := service.IssueInitialProductionGrant(ctx, actor, transferProductionRef(receipt.ExecutionID))
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
		replay, err := service.IssueInitialProductionGrant(ctx, actor, transferProductionRef(receipt.ExecutionID))
		if err != nil || replay.Outcome != "denied" || !replay.RecordedAt.Equal(denied.RecordedAt) {
			t.Fatal("refusal revived", replay, err)
		}
		fresh()
		executionID := receipt.ExecutionID
		remoteErr = errors.New("temporary dependency failure")
		if _, err := service.IssueInitialProductionGrant(ctx, actor, transferProductionRef(executionID)); err == nil {
			t.Fatal("dependency failure issued a result")
		}
		lookup, err := service.ResolveInitialProductionGrant(ctx, actor, transferProductionRef(executionID))
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
		if result, err := service.IssueInitialProductionGrant(ctx, actor, transferProductionRef(executionID)); result != nil || !errors.Is(err, failure) {
			t.Fatalf("audit failed result=%+v error=%v", result, err)
		}
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Fatal(err)
		}
		lookup, err = service.ResolveInitialProductionGrant(ctx, actor, transferProductionRef(executionID))
		if err != nil || lookup.Found {
			t.Fatal("rolled-back audit left a terminal outcome", lookup, err)
		}
		var rolledBack int64
		if err := db.Model(&sourceGrant{}).Where("request_id=?", productionGrantCommand(actor.TenantID, transferProductionRef(executionID))).Count(&rolledBack).Error; err != nil || rolledBack != 0 {
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
				r, e := service.IssueInitialProductionGrant(ctx, actor, transferProductionRef(executionID))
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

		// Two results of one execution have independent commands and terminal
		// histories. The fixture uses a registered neutral reader, so result IDs
		// are supplied by persisted owner facts rather than allocated by System.
		nativeRegistry := service.productionRegistry
		nativeIdentification := service.productionIdentification
		secondRef := production.Reference{Owner: "transfer", ExecutionID: executionID, ResultID: uuid.NewString()}
		secondReceipt := receipt.ProductionEvidence()
		secondReceipt.Reference = secondRef
		secondReceipt.Primary = plugin.TabularItemPath(base.Path.EngineID, "schema", "production", "second_"+uuid.NewString())
		secondReceipt.Objects = []production.CommittedObject{{Target: secondReceipt.Primary, Created: true}}
		registry, e := production.NewRegistry(production.Owner{Module: "transfer", ClientID: "addp-transfer", ReceiptPermission: "transfer.production_receipt.read", ExecutionPermission: "transfer.task.execute", ReadReceipt: func(_ context.Context, _ uint, ref production.Reference) (*production.Receipt, error) {
			if ref != secondReceipt.Reference {
				return nil, production.ErrEvidence
			}
			return &secondReceipt, nil
		}})
		if e != nil {
			t.Fatal(e)
		}
		logicalItemState := "new"
		service.WithProductionRegistry(registry).WithProductionIdentificationReader(productionIdentificationFunc(func(_ context.Context, tenant uint, ref production.Reference) (*client.MetaProductionIdentification, error) {
			binding := factstest.Binding(secondReceipt.Primary)
			return &client.MetaProductionIdentification{Reference: ref, TenantID: int64(tenant), EngineVersion: secondReceipt.TargetEngineVersion, ScanExecutionID: uuid.NewString(), Status: "success", Binding: &binding, LogicalItemState: logicalItemState}, nil
		})).WithAuthorizationFactsReader(testMetaFacts{paths: []plugin.EngineCatalogPath{base.Path, secondReceipt.Primary}})
		second, e := service.IssueInitialProductionGrant(ctx, actor, secondRef)
		if e != nil || second == nil || second.Outcome != "issued" || second.ResultID != uuid.MustParse(secondRef.ResultID) || second.Grant.RequestID == issued.Grant.RequestID {
			t.Fatal("independent result collapsed into first grant", second, e)
		}

		sourceFreeRef := secondRef
		sourceFreeRef.ResultID = uuid.NewString()
		secondReceipt.Reference = sourceFreeRef
		secondReceipt.Primary = plugin.TabularItemPath(base.Path.EngineID, "schema", "production", "source_free_"+uuid.NewString())
		secondReceipt.Objects = []production.CommittedObject{{Target: secondReceipt.Primary, Created: true}}
		secondReceipt.Sources = production.SourceEvidence{Kind: production.SourcesNone}
		service.WithAuthorizationFactsReader(testMetaFacts{paths: []plugin.EngineCatalogPath{secondReceipt.Primary}})
		sourceFree, e := service.IssueInitialProductionGrant(ctx, actor, sourceFreeRef)
		if e != nil || sourceFree == nil || sourceFree.Outcome != "issued" || sourceFree.Grant.ExpiryMode != shared.SharingExpiryUntilRevoked || sourceFree.Grant.ExpiresAt != nil {
			t.Fatal("proven source-free generation did not receive revocable read grant", sourceFree, e)
		}

		existingRef := sourceFreeRef
		existingRef.ResultID = uuid.NewString()
		secondReceipt.Reference = existingRef
		secondReceipt.Primary = plugin.TabularItemPath(base.Path.EngineID, "schema", "production", "logical_existing_"+uuid.NewString())
		secondReceipt.Objects = []production.CommittedObject{{Target: secondReceipt.Primary, Created: true}}
		service.WithAuthorizationFactsReader(testMetaFacts{paths: []plugin.EngineCatalogPath{secondReceipt.Primary}})
		logicalItemState = "existing"
		deniedExisting, e := service.IssueInitialProductionGrant(ctx, actor, existingRef)
		if e != nil || deniedExisting == nil || deniedExisting.Outcome != "denied" || deniedExisting.DenialCode != "production_ineligible" {
			t.Fatal("physical creation replaced missing logical newness", deniedExisting, e)
		}
		logicalItemState = "new"
		replayExisting, e := service.IssueInitialProductionGrant(ctx, actor, existingRef)
		if e != nil || replayExisting == nil || replayExisting.Outcome != "denied" || !replayExisting.RecordedAt.Equal(deniedExisting.RecordedAt) {
			t.Fatal("logical newness refresh reopened refusal", replayExisting, e)
		}
		noHuman := sourceFreeRef
		noHuman.ResultID = uuid.NewString()
		secondReceipt.Reference = noHuman
		secondReceipt.Origin = nil
		absent, e := service.IssueInitialProductionGrant(ctx, actor, noHuman)
		if e != nil || absent == nil || absent.Outcome != "denied" || absent.Grant != nil {
			t.Fatal("service-only task manufactured a human grant", absent, e)
		}
		thirdRef := secondRef
		thirdRef.ResultID = uuid.NewString()
		refusal, e := service.recordInitialProductionDenial(ctx, actor, thirdRef, "production_ineligible")
		if e != nil || refusal.Outcome != "denied" {
			t.Fatal("independent refusal collapsed", refusal, e)
		}
		original, e := service.ResolveInitialProductionGrant(ctx, actor, transferProductionRef(executionID))
		if e != nil || original.Result.Grant.RequestID != issued.Grant.RequestID {
			t.Fatal("other result changed original terminal history", original, e)
		}
		wrong := secondRef
		wrong.Owner = "unregistered"
		if _, e = service.ResolveInitialProductionGrant(ctx, actor, wrong); e == nil {
			t.Fatal("unregistered owner read history")
		}
		service.WithProductionRegistry(nativeRegistry).WithProductionIdentificationReader(nativeIdentification).WithAuthorizationFactsReader(testMetaFacts{paths: []plugin.EngineCatalogPath{base.Path, receipt.Target}})
		var grant sourceGrant
		if err := db.Where("request_id=?", issued.Grant.RequestID).Take(&grant).Error; err != nil {
			t.Fatal(err)
		}
		if grant.CatalogRequestID != nil || grant.OperatorPrincipalID != user.PrincipalID || grant.IssuerPrincipalID == nil || *grant.IssuerPrincipalID != actor.PrincipalID {
			t.Fatal("faked approval or mixed human/machine provenance", grant)
		}
		// Initial grants are ordinary source rules: they cover only the bound
		// DataItem boundary and remain independent of later source withdrawal.
		checkCoverage := func(path plugin.EngineCatalogPath, want bool) {
			t.Helper()
			rules, err := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{Action: "read", TenantID: actor.TenantID, Source: user,
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
		historyDenied, err := service.IssueInitialProductionGrant(ctx, actor, transferProductionRef(receipt.ExecutionID))
		if err != nil || historyDenied.Outcome != "denied" || historyDenied.DenialCode != "target_history_exists" {
			t.Fatal("historical target qualified as a new result", historyDenied, err)
		}
		if err := db.Model(&initialProductionOutcome{}).Where("tenant_id=? AND execution_id=?", actor.TenantID, executionID).Update("outcome", "denied").Error; err == nil {
			t.Fatal("terminal outcome was mutable")
		}
		remoteErr = errors.New("offline during historical recovery")
		service.WithAuthorizationFactsReader(nil)
		recovered, err := service.IssueInitialProductionGrant(ctx, actor, transferProductionRef(executionID))
		if err != nil || recovered.Grant.RequestID != issued.Grant.RequestID || !recovered.Grant.ExpiresAt.Equal(expiry) {
			t.Fatal("recovery required live dependencies or renewed expiry", recovered, err)
		}
		checkCoverage(receipt.Target, false)
		remoteErr = nil
		fresh()
		service.WithAuthorizationFactsReader(testMetaFacts{paths: []plugin.EngineCatalogPath{receipt.Target}})
		receipt.SourceReadSet = nil
		receipt.ManagerImport = &production.PrivateUploadProof{
			UploadID: uuid.NewString(), UploaderID: user.PrincipalID, TaskID: 23, ManifestSHA256: strings.Repeat("a", 64),
		}
		imported, err := service.IssueInitialProductionGrant(ctx, actor, transferProductionRef(receipt.ExecutionID))
		if err != nil || imported == nil || imported.Outcome != "issued" || imported.Grant == nil ||
			imported.Grant.ExpiryMode != shared.SharingExpiryUntilRevoked || imported.Grant.ExpiresAt != nil {
			t.Fatal("proven private upload did not receive an independently revocable output Grant", imported, err)
		}
		checkCoverage(receipt.Target, true)
		revoke(imported.Grant.RequestID)
		checkCoverage(receipt.Target, false)
		fresh()
		service.WithAuthorizationFactsReader(testMetaFacts{paths: []plugin.EngineCatalogPath{receipt.Target}})
		receipt.UserOrigin.AuthorizationVersion--
		stale, err := service.IssueInitialProductionGrant(ctx, actor, transferProductionRef(receipt.ExecutionID))
		if err != nil || stale == nil || stale.Outcome != "denied" || stale.DenialCode != "source_access_denied" {
			t.Fatal("stale private uploader retained initial Grant eligibility", stale, err)
		}
		bad := actor
		bad.ClientID = "addp-catalog"
		if _, err := service.ResolveInitialProductionGrant(ctx, bad, transferProductionRef(executionID)); err == nil {
			t.Fatal("wrong runtime read continuation history")
		}
		bad = actor
		bad.TenantID++
		if _, err := service.ResolveInitialProductionGrant(ctx, bad, transferProductionRef(executionID)); err == nil {
			t.Fatal("cross-tenant history returned")
		}
	})
}
