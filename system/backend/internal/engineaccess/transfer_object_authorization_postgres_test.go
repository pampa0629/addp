package engineaccess

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/production"
	"github.com/addp/common/resourcetree"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Uses formal User sessions and Grant commands in the registered file-composition gate.
// Meta facts and the connection decoder are fixtures; no business bytes are read here.
func exerciseTransferObjectAuthorization(t *testing.T, db *gorm.DB, source *Service, ctx context.Context, actor Actor, reader userProvenance, credential string, path plugin.EngineCatalogPath, facts authorizationfacts.AuthorizationFacts) {
	t.Run("Transfer exact object execution authorization", func(t *testing.T) {
		decoder := source.executionEngineSnapshot
		source.WithExecutionEngineSnapshot(func(e *models.Engine) (*models.Engine, error) {
			copy := *e
			copy.ConnectionInfo = models.ConnectionInfo{"host": "exact-object.invalid"}
			return &copy, nil
		})
		defer source.WithExecutionEngineSnapshot(decoder)
		repo := iam.NewRepository(db)
		service := iam.NewTransferObjectAuthorizationService(repo, source.PrepareExecutionSourceRead)
		principal, err := repo.GetPrincipal(ctx, reader.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		tenant := uint(actor.TenantID)
		target := models.Engine{Name: "Object import target", EngineType: "postgresql", TenantID: &tenant, Version: 1, LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{"host": "object-target.invalid"}, IdentityKey: models.JSONString(`{"host":"object-target.invalid"}`)}
		if err := db.Table("system.engines").Create(&target).Error; err != nil {
			t.Fatal(err)
		}
		var version int64
		if err := db.Raw("SELECT version FROM system.engines WHERE id=?", path.EngineID).Scan(&version).Error; err != nil {
			t.Fatal(err)
		}
		itemID := uint(90000199)
		loc := (&resourcetree.ResourceLocator{EngineID: path.EngineID, Type: resourcetree.TypeObject, Path: strings.Split(path.StringPath(), "/"), ItemID: &itemID}).ToURI()
		binding, err := facts.ApprovedBinding()
		if err != nil {
			t.Fatal(err)
		}
		who := int(reader.PrincipalID)
		owner := "9000001"
		job := &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(actor.TenantID), Module: "transfer", Source: "transfer", TaskType: "sync", SourceTaskID: &owner,
			Status: execution.ExecutionStatusPending, TriggerType: "manual", ExecutionBoundary: "bounded", MaxAttempts: 1, TriggeredBy: &who,
			ActorPrincipalID: &reader.PrincipalID, ActorTenantMembershipID: &reader.MembershipID, IssuedAuthorizationVersion: &principal.AuthorizationVersion,
			ExecutionConfig: commonModels.JSONMap{"runtime": map[string]any{"boundary": "bounded"}, "load": map[string]any{"mode": "snapshot"},
				"source":        map[string]any{"locator": loc, "data_type": "table", "representation": "encoded", "format": "parquet", "options": map[string]any{}},
				"target":        map[string]any{"parent_locator": (&resourcetree.ResourceLocator{EngineID: target.ID, Type: resourcetree.TypeSchema, Path: []string{"public"}}).ToURI(), "name": "orders", "data_type": "table", "representation": "native"},
				"object_source": execution.TransferObjectSource{EngineVersion: strconv.FormatInt(version, 10), Binding: binding}}}
		if err := execution.CreatePending(ctx, db, job); err != nil {
			t.Fatal(err)
		}
		issue := iam.IssueTransferObjectAuthorizationInput{SourceAccessToken: credential, ExecutionID: uuid.MustParse(job.ExecutionID)}
		denyIssue := func() {
			t.Helper()
			if got, err := service.Issue(ctx, issue); got != nil || err == nil {
				t.Fatal("issued without an exact read Grant", err)
			}
		}
		denyIssue()
		command := CreateIndependentGrantInput{Actor: actor, EngineID: int64(path.EngineID), RequestID: uuid.New(), CatalogPath: path, ItemFingerprint: facts.ItemFingerprint, ExpectedBoundaryHash: *facts.BoundaryHash,
			RecipientType: "user", RecipientID: reader.PrincipalID, Action: shared.SourceAccessMetadataRead, ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Object import metadata-only counterexample"}
		metadata, err := source.CreateIndependentGrant(ctx, command)
		if err != nil {
			t.Fatal(err)
		}
		denyIssue()
		t.Cleanup(func() {
			if t.Failed() {
				_, _ = source.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(path.EngineID), RequestID: metadata.RequestID, Reason: "Failed import fixture cleanup"})
			}
		})
		command.Action = shared.SourceAccessRead
		command.RequestID = uuid.New()
		sourceExpiry := time.Now().UTC().Add(20 * time.Minute).Truncate(time.Microsecond)
		command.ExpiryMode, command.ExpiresAt = shared.SharingExpiryAtTime, &sourceExpiry
		grant, err := source.CreateIndependentGrant(ctx, command)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if t.Failed() {
				_, _ = source.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(path.EngineID), RequestID: grant.RequestID, Reason: "Failed import fixture cleanup"})
			}
		})
		issued, err := service.Issue(ctx, issue)
		if err != nil {
			t.Fatal("exact object issuance", err)
		}
		if err := db.Model(job).Updates(map[string]any{"execution_authorization_id": issued.ID, "authorization_expires_at": issued.ExpiresAt}).Error; err != nil {
			t.Fatal(err)
		}
		_, lease, err := execution.ClaimPending(ctx, db, execution.PendingIdentity{ExecutionID: job.ExecutionID, TenantID: job.TenantID, Module: "transfer", TaskType: "sync", Source: "transfer"}, execution.ClaimOptions{Module: "transfer", TaskType: "sync", WorkerID: "exact-object-fixture", LeaseDuration: time.Minute, RequireAuthorization: true}, nil)
		if err != nil || lease == nil {
			t.Fatal("claim exact object", err)
		}
		var runtimeID int64
		if err := db.Raw("SELECT id FROM system.service_principals WHERE name='addp-transfer'").Scan(&runtimeID).Error; err != nil || runtimeID == 0 {
			t.Fatal("runtime unavailable", err)
		}
		hash, hashErr := bcrypt.GenerateFromPassword([]byte("object-import-disposable-runtime"), bcrypt.MinCost)
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		if err := db.Exec("UPDATE system.oauth_clients SET client_secret_hash=?,status='active' WHERE client_id='addp-transfer'", string(hash)).Error; err != nil {
			t.Fatal(err)
		}
		typ, contextType := iam.PrincipalTypeServicePrincipal, iam.ContextTypeTenant
		input := iam.AuthorizeTransferObjectInput{AuthorizationID: issued.ID, ExecutionID: issued.ExecutionID, Attempt: lease.Attempt, LeaseToken: uuid.MustParse(lease.Token), SourceReadScope: issued.SourceReadScope,
			ServicePrincipalID: runtimeID, ServiceClientID: "addp-transfer", TenantID: actor.TenantID, Audit: iam.AuditMetadata{PrincipalID: &runtimeID, PrincipalType: &typ, ContextType: &contextType, TenantID: &actor.TenantID}}
		observation, err := service.Authorize(ctx, input)
		if err != nil || observation == nil || observation.Engine == nil || observation.Engine.ID != path.EngineID || observation.Engine.Version != version {
			t.Fatal("current source lease", err)
		}
		exerciseRustFSInitialProduction(t, db, ctx, actor.TenantID, reader, path, target.ID, runtimeID, sourceExpiry)
		deny := func(wrong iam.AuthorizeTransferObjectInput) {
			t.Helper()
			if got, err := service.Authorize(ctx, wrong); got != nil || err == nil {
				t.Fatal("accepted invalid object access", err)
			}
		}
		wrong := input
		wrong.LeaseToken = uuid.New()
		deny(wrong)
		wrong = input
		wrong.Attempt++
		deny(wrong)
		wrong = input
		wrong.ServiceClientID = "addp-manager"
		deny(wrong)
		wrong = input
		wrong.TenantID++
		deny(wrong)
		wrong = input
		wrong.SourceReadScope = *input.SourceReadScope.Clone()
		wrong.SourceReadScope.ConfigDigest = strings.Repeat("a", 64)
		deny(wrong)
		for _, other := range []plugin.EngineCatalogPath{plugin.ObjectItemPath(path.EngineID, "models", "sibling.parquet"), plugin.ObjectItemPath(path.EngineID, "other", "scene.parquet")} {
			wrong = input
			wrong.SourceReadScope = *input.SourceReadScope.Clone()
			wrong.SourceReadScope.ObjectSource.Binding, err = authorizationfacts.NativeCandidateBinding(other)
			if err != nil {
				t.Fatal(err)
			}
			deny(wrong)
		}
		generic, _ := iam.NewExecutionAuthorizationService(repo)
		for _, effect := range []string{"read", "write"} {
			if got, err := generic.AuthorizeEngineAccess(ctx, iam.AuthorizeExecutionEngineAccessInput{AuthorizationID: issued.ID, ExecutionID: issued.ExecutionID, EngineID: int64(path.EngineID), RequiredEffects: []string{effect}, ServicePrincipalID: runtimeID, ServiceClientID: "addp-transfer", TenantID: actor.TenantID, Audit: input.Audit}); got != nil || err == nil {
				t.Fatal("generic access downgraded object scope")
			}
		}
		if err := db.Exec("UPDATE system.execution_authorizations SET source_read_scope=jsonb_set(source_read_scope,'{config_digest}',to_jsonb(?::text)) WHERE id=?", strings.Repeat("a", 64), issued.ID).Error; err == nil {
			t.Fatal("database allowed immutable scope mutation")
		}
		if _, err := source.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(path.EngineID), RequestID: grant.RequestID, Reason: "Withdraw import source"}); err != nil {
			t.Fatal(err)
		}
		deny(input)
		if _, err := source.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(path.EngineID), RequestID: metadata.RequestID, Reason: "Finish metadata counterexample"}); err != nil {
			t.Fatal(err)
		}
		t.Log("no Grant and metadata-only denied; exact source observed; sibling, bucket, config, tenant, client, attempt, lease, generic engine access, mutation and revocation denied")
	})
}

func exerciseRustFSInitialProduction(t *testing.T, db *gorm.DB, ctx context.Context, tenantID int64, reader userProvenance, path plugin.EngineCatalogPath, targetID uint, runtimeID int64, sourceExpiry time.Time) {
	t.Helper()
	var principal iam.Principal
	var member iam.TenantMembership
	if err := db.First(&principal, runtimeID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("tenant_id=? AND principal_id=?", tenantID, runtimeID).Take(&member).Error; err != nil {
		t.Fatal(err)
	}
	actor := ProductionRuntimeActor{Actor: Actor{TenantID: member.TenantID, PrincipalID: runtimeID, MembershipID: member.ID, AuthorizationVersion: principal.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}, ClientID: "addp-transfer"}
	receipt := productionBindingReceipt(member.TenantID, targetID)
	receipt.UserOrigin = &client.TransferProductionUserOrigin{PrincipalID: reader.PrincipalID, TenantMembershipID: reader.MembershipID, AuthorizationVersion: reader.AuthorizationVersion}
	// Role assignment preceded the real session; use the actual current version.
	var version int64
	if err := db.Raw("SELECT authorization_version FROM system.principals WHERE id=?", reader.PrincipalID).Scan(&receipt.UserOrigin.AuthorizationVersion).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT version FROM system.engines WHERE id=?", path.EngineID).Scan(&version).Error; err != nil {
		t.Fatal(err)
	}
	sibling := plugin.ObjectItemPath(path.EngineID, "models", "unapproved.parquet")
	productionService := NewService(NewRepository(db), nil).
		withTestProductionReader(productionReceiptReaderFunc(func(context.Context, uint, string) (*client.TransferProductionReceipt, error) { return &receipt, nil })).
		WithAuthorizationFactsReader(bindingFactsReaderFunc(func(_ context.Context, _ uint, fp string) (*authorizationfacts.AuthorizationFacts, error) {
			for _, p := range []plugin.EngineCatalogPath{path, sibling, receipt.Target} {
				var f authorizationfacts.AuthorizationFacts
				if p.EngineID == targetID {
					f = factstest.Single(p)
				} else {
					f = exactNativeObjectFacts(t, p)
				}
				if fp == f.ItemFingerprint {
					return &f, nil
				}
			}
			return nil, ErrAuthorizationFactsUnavailable
		}))
	fresh := func() {
		receipt.ExecutionID = uuid.NewString()
		receipt.Target = plugin.TabularItemPath(targetID, "schema", "public", "rustfs_production_"+strings.ReplaceAll(receipt.ExecutionID, "-", ""))
	}
	fresh()
	ref := transferProductionRef(receipt.ExecutionID)
	denied, err := productionService.IssueInitialProductionGrant(ctx, actor, ref)
	if err != nil || denied == nil || denied.Outcome != "denied" || denied.DenialCode != "production_ineligible" {
		t.Fatal("missing actual read proof did not remain ineligible", denied, err)
	}
	receipt.SourceReadSet, _ = plugin.NewQueryReadSet(path)
	receipt.SourceEngineVersions = []production.EngineVersion{{EngineID: int64(path.EngineID), Version: version}}
	replay, err := productionService.IssueInitialProductionGrant(ctx, actor, ref)
	if err != nil || replay == nil || replay.Outcome != "denied" || !replay.RecordedAt.Equal(denied.RecordedAt) {
		t.Fatal("historical refusal was reopened", replay, err)
	}
	fresh()
	issued, err := productionService.IssueInitialProductionGrant(ctx, actor, transferProductionRef(receipt.ExecutionID))
	if err != nil || issued == nil || issued.Outcome != "issued" || issued.Grant == nil {
		t.Fatal("exact RustFS source did not issue PG initial read grant", issued, err)
	}
	if issued.Grant.ExpiryMode != shared.SharingExpiryAtTime || issued.Grant.ExpiresAt == nil || !issued.Grant.ExpiresAt.Equal(sourceExpiry) {
		t.Fatal("object source deadline was not preserved", issued.Grant)
	}
	var grants int64
	if err := db.Model(&sourceGrant{}).Where("request_id=? AND engine_id=? AND recipient_type='user' AND recipient_id=? AND action='read'", issued.Grant.RequestID, targetID, reader.PrincipalID).Count(&grants).Error; err != nil || grants != 1 {
		t.Fatal("initial Grant did not bind the exact PG recipient and read action", grants, err)
	}
	fresh()
	receipt.SourceReadSet, _ = plugin.NewQueryReadSet(sibling)
	wrong, err := productionService.IssueInitialProductionGrant(ctx, actor, transferProductionRef(receipt.ExecutionID))
	if err != nil || wrong == nil || wrong.Outcome != "denied" || wrong.DenialCode != "source_access_denied" {
		t.Fatal("sibling object inherited source Grant", wrong, err)
	}
	fresh()
	receipt.SourceReadSet, _ = plugin.NewQueryReadSet(path)
	receipt.SourceEngineVersions[0].Version++
	wrong, err = productionService.IssueInitialProductionGrant(ctx, actor, transferProductionRef(receipt.ExecutionID))
	if err != nil || wrong == nil || wrong.Outcome != "denied" || wrong.DenialCode != "engine_changed" {
		t.Fatal("different source version issued an initial Grant", wrong, err)
	}
}
