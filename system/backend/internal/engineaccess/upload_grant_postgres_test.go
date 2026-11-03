package engineaccess

import (
	"context"
	"errors"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

type uploadReceiptFunc func(context.Context, uint, string) (*client.UploadReceipt, error)

func (f uploadReceiptFunc) ReadUploadReceipt(c context.Context, t uint, id string) (*client.UploadReceipt, error) {
	return f(c, t, id)
}

type uploadIdentificationFunc func(context.Context, uint, string) (*client.MetaUploadIdentification, error)

func (f uploadIdentificationFunc) ReadUploadIdentification(c context.Context, t uint, id string) (*client.MetaUploadIdentification, error) {
	return f(c, t, id)
}
func exerciseUploadInitialGrants(t *testing.T, db *gorm.DB, tenantID, adminID int64, roles *iam.TenantRoleService, newUser func(*testing.T, time.Duration) (userProvenance, time.Time)) {
	t.Run("upload initial grant canonical lifecycle", func(t *testing.T) {
		ctx := t.Context()
		tenant := uint(tenantID)
		engine := models.Engine{Name: "Upload initial fixture", EngineType: "nfs", TenantID: &tenant, Version: 1, LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{"host": "initial-upload.invalid"}, IdentityKey: models.JSONString(`{"host":"initial-upload.invalid"}`)}
		if err := db.Table("system.engines").Create(&engine).Error; err != nil {
			t.Fatal(err)
		}
		var machine iam.Principal
		var membership iam.TenantMembership
		if err := db.Raw("SELECT p.* FROM system.principals p JOIN system.service_principals s ON s.id=p.id WHERE s.name='addp-manager'").Scan(&machine).Error; err != nil || machine.ID == 0 {
			t.Fatal(err)
		}
		if err := db.Where("tenant_id=? AND principal_id=?", tenantID, machine.ID).Take(&membership).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.oauth_clients").Where("client_id='addp-manager'").Updates(map[string]any{"status": "active", "client_secret_hash": "disposable_fixture_only"}).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Table("system.oauth_clients").Where("client_id='addp-manager'").Update("status", "disabled")
		if err := db.First(&machine, machine.ID).Error; err != nil {
			t.Fatal(err)
		}
		actor := UploadRuntimeActor{Actor: Actor{TenantID: tenantID, PrincipalID: machine.ID, MembershipID: membership.ID, AuthorizationVersion: machine.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}, ClientID: "addp-manager"}
		reader, _ := newUser(t, time.Hour)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.upload_initial_reader", Name: "Upload reader", PermissionKeys: []string{"manager.data_item.create", ManagerPreviewReadPermission}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: reader.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "Upload without authorization administration"}); err != nil {
			t.Fatal(err)
		}
		p, err := iam.NewRepository(db).GetPrincipal(ctx, reader.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		reader.AuthorizationVersion = p.AuthorizationVersion
		uploader := client.UploadUserOrigin{PrincipalID: reader.PrincipalID, MembershipID: reader.MembershipID, AuthorizationVersion: reader.AuthorizationVersion}
		receipt := client.UploadReceipt{SchemaVersion: client.UploadReceiptSchema, TenantID: tenantID, EngineID: engine.ID, EngineVersion: "1", Uploader: uploader, CommittedAt: time.Now().UTC()}
		var binding authorizationfacts.Binding
		var facts authorizationfacts.AuthorizationFacts
		remoteErr := error(nil)
		calls := 0
		fresh := func(metadata bool) {
			receipt.UploadID = uuid.NewString()
			target := plugin.FileItemPath(engine.ID, "uploads/new_"+uuid.NewString()+".ifc")
			receipt.Writes = []plugin.ContentWriteEvidence{{Target: target, Disposition: "created", Committed: true, SizeBytes: 4}}
			boundary := &authorizationfacts.AuthorizationBoundary{Layout: "single"}
			if metadata {
				boundary.MetadataTarget = &authorizationfacts.MetadataBoundary{Target: target, Layout: "single", DataType: "document", Format: "txt", CompositionDigest: strings.Repeat("a", 64)}
			} else {
				composition, e := authorizationfacts.NewFileCompositionBoundary("ifc", []authorizationfacts.FileCompositionRef{{Target: target, Role: "model", Required: true, Primary: true}})
				if e != nil {
					t.Fatal(e)
				}
				boundary.FileComposition = composition
			}
			fp, _ := authorizationfacts.PrimaryCandidateFingerprint(target)
			facts = authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: engine.ID, ItemType: "file", FullName: target.StringPath(), ItemFingerprint: fp, BasisState: authorizationfacts.AuthorizationBasisComplete, Boundary: boundary, ObservedAt: time.Now().UTC()}
			hash, e := facts.ComputeBoundaryHash()
			if e != nil {
				t.Fatal(e)
			}
			facts.BoundaryHash = &hash
			binding, e = facts.MetadataBinding()
			if e != nil {
				t.Fatal(e)
			}
		}
		svc := NewService(NewRepository(db), nil).WithUploadReaders(uploadReceiptFunc(func(c context.Context, tnt uint, id string) (*client.UploadReceipt, error) {
			calls++
			if err := db.WithContext(c).Transaction(func(tx *gorm.DB) error {
				return tx.Exec("SELECT id FROM system.principals WHERE id=? FOR UPDATE NOWAIT", machine.ID).Error
			}); err != nil {
				t.Fatal(err)
			}
			return &receipt, remoteErr
		}), uploadIdentificationFunc(func(context.Context, uint, string) (*client.MetaUploadIdentification, error) {
			return &client.MetaUploadIdentification{UploadID: receipt.UploadID, ScanExecutionID: uuid.NewString(), Status: "success", NewItems: []authorizationfacts.Binding{binding}}, remoteErr
		})).WithAuthorizationFactsReader(bindingFactsReaderFunc(func(context.Context, uint, string) (*authorizationfacts.AuthorizationFacts, error) {
			copy := facts
			return &copy, remoteErr
		}))
		for _, metadata := range []bool{false, true} {
			fresh(metadata)
			remoteErr = errors.New("temporary Meta outage")
			if got, e := svc.IssueInitialUploadGrant(ctx, actor, receipt.UploadID, binding.ItemFingerprint); e == nil || got != nil {
				t.Fatal("dependency failure granted")
			}
			var pending int64
			db.Table("system.engine_access_upload_grant_outcomes").Where("tenant_id=? AND upload_id=?", tenantID, receipt.UploadID).Count(&pending)
			if pending != 0 {
				t.Fatal("temporary failure became terminal")
			}
			remoteErr = nil
			got, e := svc.IssueInitialUploadGrant(ctx, actor, receipt.UploadID, binding.ItemFingerprint)
			want := "read"
			if metadata {
				want = "metadata_read"
			}
			if e != nil || got == nil || got.Outcome != "issued" || got.Action != want {
				t.Fatalf("issued=%+v err=%v", got, e)
			}
			rule, e := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{TenantID: tenantID, Source: reader, Action: "metadata_read", Targets: []plugin.EngineCatalogPath{receipt.Writes[0].Target}, Bindings: []authorizationfacts.Binding{binding}})
			if e != nil || !rule.Covered {
				t.Fatalf("canonical metadata rule=%+v %v", rule, e)
			}
			originalCalls := calls
			remoteErr = errors.New("offline after commit")
			replay, e := svc.IssueInitialUploadGrant(ctx, actor, receipt.UploadID, binding.ItemFingerprint)
			if e != nil || replay.RequestID == nil || *replay.RequestID != *got.RequestID || calls != originalCalls {
				t.Fatalf("history replay used owner IO: %+v %v", replay, e)
			}
			// Exercise the immutable revocation store with a current qualified administrator.
			operatorRole, e := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.upload_revoke_" + want, Name: "Revoke upload", PermissionKeys: []string{"system.engine_access_delegation.create", "system.engine_access_grant.revoke"}, ActorPrincipalID: adminID})
			if e != nil {
				t.Fatal(e)
			}
			op, _ := newUser(t, time.Hour)
			if _, e := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: op.MembershipID, RoleIDs: []int64{operatorRole.ID}, ActorPrincipalID: adminID, Reason: "Explicit upload revocation"}); e != nil {
				t.Fatal(e)
			}
			principal, e := iam.NewRepository(db).GetPrincipal(ctx, op.PrincipalID)
			if e != nil {
				t.Fatal(e)
			}
			if _, e := svc.RevokeGrant(ctx, RevokeGrantInput{Actor: Actor{TenantID: tenantID, PrincipalID: op.PrincipalID, MembershipID: op.MembershipID, AuthorizationVersion: principal.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}, EngineID: int64(engine.ID), RequestID: *got.RequestID, Reason: "Acceptance cleanup"}); e != nil {
				t.Fatal(e)
			}
			replay, e = svc.IssueInitialUploadGrant(ctx, actor, receipt.UploadID, binding.ItemFingerprint)
			if e != nil || replay.Outcome != "issued" {
				t.Fatal("revoked historical outcome lost", e)
			}
			rule, e = NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{TenantID: tenantID, Source: reader, Action: "metadata_read", Targets: []plugin.EngineCatalogPath{receipt.Writes[0].Target}, Bindings: []authorizationfacts.Binding{binding}})
			if e != nil || rule.Covered {
				t.Fatal("replay revived revoked upload", e)
			}
			remoteErr = nil
		}
		fresh(false)
		auditFailure := errors.New("upload issuance audit unavailable")
		const callback = "upload_initial_audit_failure"
		if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
			if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "system.engine_access_initial_grant.issued" {
				tx.AddError(auditFailure)
			}
		}); err != nil {
			t.Fatal(err)
		}
		failed, err := svc.IssueInitialUploadGrant(ctx, actor, receipt.UploadID, binding.ItemFingerprint)
		_ = db.Callback().Create().Remove(callback)
		if failed != nil || !errors.Is(err, auditFailure) {
			t.Fatalf("audit failure=%+v %v", failed, err)
		}
		var outcomes, grants int64
		if err := db.Table("system.engine_access_upload_grant_outcomes").Where("tenant_id=? AND upload_id=?", tenantID, receipt.UploadID).Count(&outcomes).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.engine_access_grants").Where("tenant_id=? AND upload_id=?", tenantID, receipt.UploadID).Count(&grants).Error; err != nil {
			t.Fatal(err)
		}
		if outcomes != 0 || grants != 0 {
			t.Fatal("failed audit left Grant or terminal outcome")
		}
		receipt.Writes[0].Disposition = "replaced"
		got, err := svc.IssueInitialUploadGrant(ctx, actor, receipt.UploadID, binding.ItemFingerprint)
		if err != nil || got.Outcome != "denied" || got.Reason != "not_new" {
			t.Fatalf("replacement=%+v %v", got, err)
		}
		receipt.Writes[0].Disposition = "created"
		again, err := svc.IssueInitialUploadGrant(ctx, actor, receipt.UploadID, binding.ItemFingerprint)
		if err != nil || again.Outcome != "denied" {
			t.Fatal("terminal refusal resurrected", err)
		}
		wrong := actor
		wrong.ClientID = "addp-transfer"
		if _, err := svc.IssueInitialUploadGrant(ctx, wrong, receipt.UploadID, binding.ItemFingerprint); err == nil {
			t.Fatal("other runtime signed upload")
		}
	})
}
