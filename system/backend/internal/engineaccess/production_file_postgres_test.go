package engineaccess

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/production"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Real IAM, Grant and migration stores with explicit producer/Meta doubles.
// No real file publication, NFS scan or cross-module T4 is executed here.
func exerciseFileProductionGrants(t *testing.T, db *gorm.DB, tenantID, adminID int64, roles *iam.TenantRoleService, newUser func(*testing.T, time.Duration) (userProvenance, time.Time)) {
	t.Run("file production grant lifecycle", func(t *testing.T) {
		ctx := t.Context()
		tenant := uint(tenantID)
		makeEngine := func(name string) models.Engine {
			e := models.Engine{Name: name, EngineType: "nfs", TenantID: &tenant, Version: 1, LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{"host": name + ".invalid"}, IdentityKey: models.JSONString(`{"host":"` + name + `.invalid"}`)}
			if err := db.Table("system.engines").Create(&e).Error; err != nil {
				t.Fatal(err)
			}
			return e
		}
		sourceEngine, targetEngine := makeEngine("production-source"), makeEngine("production-target")
		var machine iam.Principal
		var membership iam.TenantMembership
		if err := db.Raw("SELECT p.* FROM system.principals p JOIN system.service_principals s ON s.id=p.id WHERE s.name='addp-develop'").Scan(&machine).Error; err != nil || machine.ID == 0 {
			t.Fatal(err)
		}
		if err := db.Where("tenant_id=? AND principal_id=?", tenantID, machine.ID).Take(&membership).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.oauth_clients").Where("client_id='addp-develop'").Updates(map[string]any{"status": "active", "client_secret_hash": "disposable_fixture_only"}).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Table("system.oauth_clients").Where("client_id='addp-develop'").Update("status", "disabled")
		if err := db.First(&machine, machine.ID).Error; err != nil {
			t.Fatal(err)
		}
		actor := ProductionRuntimeActor{Actor: Actor{TenantID: tenantID, PrincipalID: machine.ID, MembershipID: membership.ID, AuthorizationVersion: machine.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}, ClientID: "addp-develop"}
		user, _ := newUser(t, time.Hour)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.file_production_execute", Name: "File producer", PermissionKeys: []string{"develop.task.execute", "system.execution_authorization.create"}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "Execution without Grant administration"}); err != nil {
			t.Fatal(err)
		}
		p, err := iam.NewRepository(db).GetPrincipal(ctx, user.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		user.AuthorizationVersion = p.AuthorizationVersion
		var receipt production.Receipt
		var remoteErr error
		bindings := map[string]authorizationfacts.Binding{}
		facts := map[string]authorizationfacts.AuthorizationFacts{}
		fresh := func(engine uint, sources production.SourceEvidence) {
			id, operation := uuid.NewString(), uuid.NewString()
			path := plugin.FileItemPath(engine, "outputs/"+operation+".gpkg")
			f := developGeoPackageFacts(t, path)
			b, e := f.ApprovedBinding()
			if e != nil {
				t.Fatal(e)
			}
			facts[b.ItemFingerprint], bindings[b.ItemFingerprint] = *f, b
			receipt = production.Receipt{Reference: production.Reference{Owner: "develop", ExecutionID: id, ResultID: operation}, TenantID: tenantID, TargetEngineVersion: 1, Attempt: 1, OperationID: operation, CommittedAt: time.Now().UTC(), Origin: &production.UserOrigin{PrincipalID: user.PrincipalID, MembershipID: user.MembershipID, AuthorizationVersion: user.AuthorizationVersion}, Primary: path, Objects: []production.CommittedObject{{Target: path, Created: true}}, Sources: sources}
		}
		registry, err := production.NewRegistry(production.Owner{Module: "develop", ClientID: "addp-develop", ReceiptPermission: "develop.production_receipt.read", ExecutionPermission: "develop.task.execute", ReadReceipt: func(_ context.Context, tenant uint, ref production.Reference) (*production.Receipt, error) {
			if remoteErr != nil {
				return nil, remoteErr
			}
			if int64(tenant) != tenantID || ref != receipt.Reference {
				return nil, production.ErrEvidence
			}
			copy := receipt
			return &copy, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		svc := NewService(NewRepository(db), nil).WithProductionRegistry(registry).
			WithAuthorizationFactsReader(bindingFactsReaderFunc(func(_ context.Context, _ uint, fp string) (*authorizationfacts.AuthorizationFacts, error) {
				f, ok := facts[fp]
				if !ok {
					return nil, errors.New("unknown fixture target")
				}
				return &f, remoteErr
			})).
			WithProductionIdentificationReader(productionIdentificationFunc(func(_ context.Context, _ uint, ref production.Reference) (*client.MetaProductionIdentification, error) {
				fp, _ := authorizationfacts.PrimaryCandidateFingerprint(receipt.Primary)
				b := bindings[fp]
				return &client.MetaProductionIdentification{Reference: ref, TenantID: tenantID, ScanExecutionID: uuid.NewString(), Status: "success", EngineVersion: 1, LogicalItemState: "new", Binding: &b}, remoteErr
			}))
		fresh(sourceEngine.ID, production.SourceEvidence{Kind: production.SourcesNone})
		first, err := svc.IssueInitialProductionGrant(ctx, actor, receipt.Reference)
		if err != nil || first.Outcome != "issued" || first.Grant.ExpiryMode != shared.SharingExpiryUntilRevoked {
			t.Fatal(first, err)
		}
		source := receipt.Primary
		fp, _ := authorizationfacts.PrimaryCandidateFingerprint(source)
		sourceBinding := bindings[fp]
		var stored sourceGrant
		if err := db.Where("request_id=?", first.Grant.RequestID).Take(&stored).Error; err != nil || len(stored.FileComposition) == 0 {
			t.Fatal("file composition missing from canonical Grant", err)
		}
		readSet, _ := plugin.NewQueryReadSet(source)
		sources := production.SourceEvidence{Kind: production.SourcesDataItems, ReadSet: readSet, EngineVersions: []production.EngineVersion{{EngineID: int64(sourceEngine.ID), Version: 1}}}
		fresh(targetEngine.ID, sources)
		secondRef, target := receipt.Reference, receipt.Primary
		second, err := svc.IssueInitialProductionGrant(ctx, actor, secondRef)
		if err != nil || second.Outcome != "issued" || second.Grant.ExpiresAt == nil {
			t.Fatal("file source did not constrain derived expiry", second, err)
		}
		targetFP, _ := authorizationfacts.PrimaryCandidateFingerprint(target)
		coverage := func(path plugin.EngineCatalogPath, binding authorizationfacts.Binding) bool {
			rules, e := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{TenantID: tenantID, Source: user, Action: "read", Targets: []plugin.EngineCatalogPath{path}, Bindings: []authorizationfacts.Binding{binding}})
			if e != nil {
				t.Fatal(e)
			}
			return rules.Covered
		}
		if !coverage(source, sourceBinding) || !coverage(target, bindings[targetFP]) {
			t.Fatal("canonical file Grant unreadable")
		}
		// A separately qualified administrator withdraws the source permission.
		op, _ := newUser(t, time.Hour)
		opRole, e := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.file_production_revoke", Name: "File revoke", PermissionKeys: []string{"system.engine_access_delegation.create", "system.engine_access_grant.revoke"}, ActorPrincipalID: adminID})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: op.MembershipID, RoleIDs: []int64{opRole.ID}, ActorPrincipalID: adminID, Reason: "Explicit source withdrawal"}); e != nil {
			t.Fatal(e)
		}
		opPrincipal, e := iam.NewRepository(db).GetPrincipal(ctx, op.PrincipalID)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = svc.RevokeGrant(ctx, RevokeGrantInput{Actor: Actor{TenantID: tenantID, PrincipalID: op.PrincipalID, MembershipID: op.MembershipID, AuthorizationVersion: opPrincipal.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}, EngineID: int64(sourceEngine.ID), RequestID: first.Grant.RequestID, Reason: "Withdraw source"}); e != nil {
			t.Fatal(e)
		}
		if coverage(source, sourceBinding) || !coverage(target, bindings[targetFP]) {
			t.Fatal("source withdrawal cascaded to materialized output")
		}
		remoteErr = errors.New("owner unavailable after issuance")
		replayed, e := svc.IssueInitialProductionGrant(ctx, actor, secondRef)
		if e != nil || !reflect.DeepEqual(replayed.Grant, second.Grant) {
			t.Fatal("replay changed original Grant/expiry", replayed, e)
		}
		remoteErr = nil
		fresh(targetEngine.ID, sources)
		denied, e := svc.IssueInitialProductionGrant(ctx, actor, receipt.Reference)
		if e != nil || denied.Outcome != "denied" || denied.DenialCode != "source_access_denied" {
			t.Fatal("revoked source permitted new output Grant", denied, e)
		}
		fresh(targetEngine.ID, sources)
		if e := db.Table("system.engines").Where("id=?", sourceEngine.ID).Update("version", 2).Error; e != nil {
			t.Fatal(e)
		}
		changed, e := svc.IssueInitialProductionGrant(ctx, actor, receipt.Reference)
		if e != nil || changed.Outcome != "denied" || changed.DenialCode != "engine_changed" {
			t.Fatal("current source version replaced frozen version", changed, e)
		}
	})
}
