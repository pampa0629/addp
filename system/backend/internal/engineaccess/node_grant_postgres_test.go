package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type nodeIdentityReaderFunc func(context.Context, uint, authorizationfacts.NodeIdentityQuery) (*authorizationfacts.NodeIdentity, error)

func (f nodeIdentityReaderFunc) ResolveNodeIdentity(c context.Context, t uint, q authorizationfacts.NodeIdentityQuery) (*authorizationfacts.NodeIdentity, error) {
	return f(c, t, q)
}

type nodeIdentityPageReaderFunc func(context.Context, uint, authorizationfacts.NodeIdentityPageQuery) (*authorizationfacts.NodeIdentityPage, error)

func (f nodeIdentityPageReaderFunc) QueryNodeIdentities(c context.Context, t uint, q authorizationfacts.NodeIdentityPageQuery) (*authorizationfacts.NodeIdentityPage, error) {
	return f(c, t, q)
}

func exercisePersistentNodeGrants(t *testing.T, db *gorm.DB, service *Service, actor Actor, base plugin.EngineCatalogPath, newUser func(*testing.T, time.Duration) (userProvenance, time.Time)) {
	t.Run("persistent PG node grants use current item facts and independent sources", func(t *testing.T) {
		ctx := context.Background()
		repo := NewRepository(db)
		user, _ := newUser(t, time.Hour)
		calls := 0
		service.WithNodeIdentityReader(nodeIdentityReaderFunc(func(c context.Context, tenant uint, q authorizationfacts.NodeIdentityQuery) (*authorizationfacts.NodeIdentity, error) {
			calls++
			if int64(tenant) != actor.TenantID || q.Target.EngineID != base.EngineID {
				t.Fatal("cross-owner identity scope changed")
			}
			// Cross-owner facts never run under operator locks.
			if err := db.WithContext(c).Transaction(func(tx *gorm.DB) error {
				return tx.Exec("SELECT id FROM system.principals WHERE id=? FOR UPDATE NOWAIT", actor.PrincipalID).Error
			}); err != nil {
				return nil, err
			}
			return &authorizationfacts.NodeIdentity{TenantID: tenant, Target: q.Target, Exists: true}, nil
		}))
		scope := plugin.TabularNamespacePath(base.EngineID, "schema", "node_scope.a/ b")
		recursive := false
		command := CreateIndependentGrantInput{Actor: actor, EngineID: int64(base.EngineID), RequestID: uuid.New(), CatalogPath: scope, NodeRecursive: &recursive, RecipientType: "user", RecipientID: user.PrincipalID, Action: shared.SourceAccessMetadataRead, ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Persistent exact schema scope"}
		grant, err := service.CreateIndependentGrant(ctx, command)
		if err != nil {
			t.Fatal(err)
		}
		if grant.NodeRecursive == nil || *grant.NodeRecursive || grant.ItemFingerprint != "" || grant.BoundaryHash != "" {
			t.Fatalf("scope lost: %+v", grant)
		}
		if replay, err := service.CreateIndependentGrant(ctx, command); err != nil || replay.RequestID != grant.RequestID || calls != 1 {
			t.Fatalf("scope command replay queried source or changed identity: %+v %d %v", replay, calls, err)
		}
		changed := command
		expanded := true
		changed.NodeRecursive = &expanded
		if _, err := service.CreateIndependentGrant(ctx, changed); !errors.Is(err, ErrIndependentGrantConflict) {
			t.Fatalf("same command expanded scope: %v", err)
		}
		changed = command
		changed.RequestID = uuid.New()
		if _, err := service.CreateIndependentGrant(ctx, changed); !errors.Is(err, ErrGrantRelationExists) {
			t.Fatalf("duplicate scope relation: %v", err)
		}
		current := plugin.TabularItemPath(base.EngineID, "schema", scope.Segments[1].Name, "current")
		future := plugin.TabularItemPath(base.EngineID, "schema", scope.Segments[1].Name, "identified_later")
		sibling := plugin.TabularItemPath(base.EngineID, "schema", scope.Segments[1].Name+"_sibling", "current")
		read := func(action string, path plugin.EngineCatalogPath, covered bool) {
			t.Helper()
			result, err := repo.readCurrentSourceRules(ctx, sourceReadRequest{Action: action, TenantID: actor.TenantID, Source: user, Targets: []plugin.EngineCatalogPath{path}, Bindings: []authorizationfacts.Binding{factstest.Binding(path)}})
			if err != nil || result == nil || result.Covered != covered {
				t.Fatalf("action=%s scope=%v covered=%v result=%+v err=%v", action, path, covered, result, err)
			}
		}
		read("metadata_read", current, true)
		read("metadata_read", future, true)
		read("read", current, false)
		read("metadata_read", sibling, false)
		foreign := current
		foreign.EngineID++
		read("metadata_read", foreign, false)
		var total int64
		if err := db.Model(&sourceGrant{}).Where("request_id=?", grant.RequestID).Count(&total).Error; err != nil || total != 1 {
			t.Fatal("scope expanded into leaf grants", total, err)
		}
		// A separate read scope overlaps the metadata scope; withdrawal is independent.
		readCommand := command
		readCommand.RequestID = uuid.New()
		readCommand.Action = "read"
		readGrant, err := service.CreateIndependentGrant(ctx, readCommand)
		if err != nil {
			t.Fatal(err)
		}
		read("read", future, true)
		if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(base.EngineID), RequestID: readGrant.RequestID, Reason: "Withdraw only read scope"}); err != nil {
			t.Fatal(err)
		}
		read("read", future, false)
		read("metadata_read", future, true)
		// The original target remains a node in current relations and history.
		rows, count, err := service.ListSourceGrantRelations(ctx, actor, int64(base.EngineID), 1, 100, SourceGrantFilter{RecipientType: "user", RecipientID: user.PrincipalID, CatalogPath: &future})
		if err != nil || count != 1 || len(rows) != 1 || rows[0].NodeRecursive == nil || rows[0].Inspection == nil || !rows[0].Inspection.RuleCovered || len(rows[0].Inspection.Sources) != 1 {
			t.Fatalf("node source inspection: %+v %d %v", rows, count, err)
		}
		var original plugin.EngineCatalogPath
		if err := json.Unmarshal(rows[0].CatalogPath, &original); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(original, scope) {
			t.Fatal("node scope was represented as fake leaf history")
		}
		if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(base.EngineID), RequestID: grant.RequestID, Reason: "Withdraw metadata scope"}); err != nil {
			t.Fatal(err)
		}
		read("metadata_read", future, false)
		replay, err := service.CreateIndependentGrant(ctx, command)
		if err != nil || replay.Revocation == nil {
			t.Fatalf("revoked command revived: %+v %v", replay, err)
		}
		// Engine root grants explicitly distinguish direct and recursive coverage.
		rootCommand := command
		rootCommand.RequestID = uuid.New()
		rootCommand.CatalogPath = plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), base.EngineID)
		direct, err := service.CreateIndependentGrant(ctx, rootCommand)
		if err != nil {
			t.Fatal(err)
		}
		read("metadata_read", future, false)
		rootCommand.RequestID = uuid.New()
		rootCommand.NodeRecursive = &expanded
		root, err := service.CreateIndependentGrant(ctx, rootCommand)
		if err != nil {
			t.Fatal(err)
		}
		read("metadata_read", future, true)
		read("metadata_read", sibling, true)
		for _, id := range []uuid.UUID{root.RequestID, direct.RequestID} {
			if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(base.EngineID), RequestID: id, Reason: "End node fixture"}); err != nil {
				t.Fatal(err)
			}
		}
		read("metadata_read", future, false)
		// Withdrawing a precise source cannot exclude an item from a live node.
		service.WithAuthorizationFactsReader(testMetaFacts{paths: []plugin.EngineCatalogPath{future}}).WithIndependentTargetVerifier(independentVerifierFunc(func(c context.Context, tenant int64, p plugin.EngineCatalogPath) (int64, error) {
			if tenant != actor.TenantID || !reflect.DeepEqual(p, future) {
				return 0, commonapi.ErrBadRequest
			}
			engine, err := repo.engine(c, tenant, int64(base.EngineID), false)
			if err != nil {
				return 0, err
			}
			return engine.Version, nil
		}))
		precise := command
		precise.RequestID, precise.CatalogPath, precise.NodeRecursive = uuid.New(), future, nil
		binding := factstest.Binding(future)
		precise.ItemFingerprint, precise.ExpectedBoundaryHash = binding.ItemFingerprint, binding.BoundaryHash
		exact, err := service.CreateIndependentGrant(ctx, precise)
		if err != nil {
			t.Fatal(err)
		}
		overlap := command
		overlap.RequestID = uuid.New()
		scoped, err := service.CreateIndependentGrant(ctx, overlap)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(base.EngineID), RequestID: exact.RequestID, Reason: "Withdraw precise source only"}); err != nil {
			t.Fatal(err)
		}
		read("metadata_read", future, true)
		if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(base.EngineID), RequestID: scoped.RequestID, Reason: "Withdraw final node source"}); err != nil {
			t.Fatal(err)
		}
		read("metadata_read", future, false)
		expiry := time.Now().UTC().Truncate(time.Microsecond).Add(3 * time.Second)
		finite := command
		finite.RequestID, finite.ExpiryMode, finite.ExpiresAt = uuid.New(), shared.SharingExpiryAtTime, &expiry
		expired, err := service.CreateIndependentGrant(ctx, finite)
		if err != nil {
			t.Fatal(err)
		}
		observed, err := repo.readCurrentSourceRules(ctx, sourceReadRequest{Action: "metadata_read", TenantID: actor.TenantID, Source: user, Targets: []plugin.EngineCatalogPath{future}, Bindings: []authorizationfacts.Binding{binding}})
		if err != nil || observed == nil || !observed.Covered || observed.ExpiresAt == nil || !observed.ExpiresAt.Equal(expiry) {
			t.Fatalf("node expiry observation: %+v %v", observed, err)
		}
		time.Sleep(time.Until(expiry) + 20*time.Millisecond)
		read("metadata_read", future, false)
		var revoked int64
		if err := db.Table("system.engine_access_grant_revocations").Where("request_id=?", expired.RequestID).Count(&revoked).Error; err != nil || revoked != 0 {
			t.Fatal("manual revocation replaced natural node expiry", err)
		}
		prior, err := service.CreateIndependentGrant(ctx, finite)
		if err != nil || prior.Revocation != nil || prior.ExpiresAt == nil || !prior.ExpiresAt.Equal(expiry) {
			t.Fatalf("expired replay renewed or revoked scope: %+v %v", prior, err)
		}
		read("metadata_read", future, false)
		// Missing or mismatched Meta facts never create a Grant.
		service.WithNodeIdentityReader(nodeIdentityReaderFunc(func(_ context.Context, tenant uint, q authorizationfacts.NodeIdentityQuery) (*authorizationfacts.NodeIdentity, error) {
			return &authorizationfacts.NodeIdentity{TenantID: tenant, Target: q.Target, Exists: false}, nil
		}))
		missing := command
		missing.RequestID = uuid.New()
		if _, err := service.CreateIndependentGrant(ctx, missing); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatalf("missing Meta node accepted: %v", err)
		}
		// Navigation is protected before and after Meta IO, without source or
		// item enumeration. Losing qualification while IO is in flight hides it.
		rootQuery := authorizationfacts.NodeIdentityPageQuery{Target: rootCommand.CatalogPath, Page: 1, PageSize: 100}
		pageCalls := 0
		qualifyDuringIO := true
		service.WithNodeIdentityPageReader(nodeIdentityPageReaderFunc(func(c context.Context, tenant uint, q authorizationfacts.NodeIdentityPageQuery) (*authorizationfacts.NodeIdentityPage, error) {
			pageCalls++
			if err := db.WithContext(c).Transaction(func(tx *gorm.DB) error {
				return tx.Exec("SELECT id FROM system.principals WHERE id=? FOR UPDATE NOWAIT", actor.PrincipalID).Error
			}); err != nil {
				return nil, err
			}
			if !qualifyDuringIO {
				if err := db.WithContext(c).Exec("UPDATE system.principals SET status='disabled' WHERE id=?", actor.PrincipalID).Error; err != nil {
					return nil, err
				}
			}
			return &authorizationfacts.NodeIdentityPage{Parent: authorizationfacts.NodeIdentity{TenantID: tenant, Target: q.Target, Exists: true}, Nodes: []authorizationfacts.NodeIdentity{{TenantID: tenant, Target: scope, Exists: true}}, Total: 1, Page: q.Page, PageSize: q.PageSize}, nil
		}))
		unqualified := Actor{TenantID: actor.TenantID, PrincipalID: user.PrincipalID, MembershipID: user.MembershipID, AuthorizationVersion: user.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}
		if _, err := service.ListAuthorizationNodes(ctx, unqualified, int64(base.EngineID), rootQuery); err == nil || pageCalls != 0 {
			t.Fatalf("unqualified browser reached Meta: %d %v", pageCalls, err)
		}
		if page, err := service.ListAuthorizationNodes(ctx, actor, int64(base.EngineID), rootQuery); err != nil || page.Total != 1 || pageCalls != 1 {
			t.Fatalf("qualified node discovery: %+v %d %v", page, pageCalls, err)
		}
		qualifyDuringIO = false
		defer db.Exec("UPDATE system.principals SET status='active' WHERE id=?", actor.PrincipalID)
		if page, err := service.ListAuthorizationNodes(ctx, actor, int64(base.EngineID), rootQuery); err == nil || page != nil || pageCalls != 2 {
			t.Fatalf("qualification loss leaked nodes: %+v %d %v", page, pageCalls, err)
		}
		service.WithNodeIdentityPageReader(nil)
		service.WithNodeIdentityReader(nil)
	})
	t.Run("NFS node scope uses current complete boundaries", func(t *testing.T) {
		ctx := t.Context()
		// This existing disposable fixture changes profile only inside this
		// subtest; no real engine or source is contacted.
		if err := db.Exec("UPDATE system.engines SET engine_type='nfs' WHERE tenant_id=? AND id=?", actor.TenantID, base.EngineID).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Exec("UPDATE system.engines SET engine_type='postgresql' WHERE tenant_id=? AND id=?", actor.TenantID, base.EngineID).Error; err != nil {
				t.Error(err)
			}
		})
		service.WithNodeIdentityReader(nodeIdentityReaderFunc(func(_ context.Context, tenant uint, q authorizationfacts.NodeIdentityQuery) (*authorizationfacts.NodeIdentity, error) {
			return &authorizationfacts.NodeIdentity{TenantID: tenant, Target: q.Target, Exists: true}, nil
		}))
		t.Cleanup(func() { service.WithNodeIdentityReader(nil) })
		user, _ := newUser(t, time.Hour)
		scope := plugin.FileDirectoryPath(base.EngineID, "models/ exact.dir ")
		direct := false
		command := CreateIndependentGrantInput{Actor: actor, EngineID: int64(base.EngineID), RequestID: uuid.New(), CatalogPath: scope, NodeRecursive: &direct, RecipientType: "user", RecipientID: user.PrincipalID, Action: shared.SourceAccessRead, ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "NFS current and future items"}
		grant, err := service.CreateIndependentGrant(ctx, command)
		if err != nil {
			t.Fatal(err)
		}
		bindingFor := func(name string, whole bool, dependency string) authorizationfacts.Binding {
			t.Helper()
			primary := plugin.FileItemPath(base.EngineID, scope.StringPath()+"/"+name)
			var boundary *authorizationfacts.AuthorizationBoundary
			if whole {
				primary = plugin.FileDirectoryPath(base.EngineID, scope.StringPath()+"/"+name)
				boundary = &authorizationfacts.AuthorizationBoundary{Layout: "whole", Scope: &authorizationfacts.ScopeBoundary{Format: "filegdb", RuleVersion: 1, Root: primary}}
			} else {
				refs := []authorizationfacts.FileCompositionRef{{Target: primary, Primary: true, Required: true, Role: "model"}}
				format := "ifc"
				if dependency != "" {
					format = "3ds"
					refs = append(refs, authorizationfacts.FileCompositionRef{Target: plugin.FileItemPath(base.EngineID, dependency), Required: true, Role: "texture"})
				}
				composition, err := authorizationfacts.NewFileCompositionBoundary(format, refs)
				if err != nil {
					t.Fatal(err)
				}
				boundary = &authorizationfacts.AuthorizationBoundary{Layout: "single", FileComposition: composition}
			}
			fp, err := authorizationfacts.PrimaryCandidateFingerprint(primary)
			if err != nil {
				t.Fatal(err)
			}
			facts := authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: base.EngineID, ItemType: "file", FullName: primary.StringPath(), ItemFingerprint: fp, BasisState: authorizationfacts.AuthorizationBasisComplete, Boundary: boundary, ObservedAt: time.Now().UTC()}
			hash, err := facts.ComputeBoundaryHash()
			if err != nil {
				t.Fatal(err)
			}
			facts.BoundaryHash = &hash
			binding, err := facts.ApprovedBinding()
			if err != nil {
				t.Fatal(err)
			}
			return binding
		}
		check := func(binding authorizationfacts.Binding, want bool) {
			t.Helper()
			primary, err := binding.PrimaryTarget()
			if err != nil {
				t.Fatal(err)
			}
			result, err := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{Action: "metadata_read", TenantID: actor.TenantID, Source: user, Targets: []plugin.EngineCatalogPath{primary}, Bindings: []authorizationfacts.Binding{binding}})
			if err != nil || result == nil || result.Covered != want {
				t.Fatalf("covered=%v result=%+v err=%v", want, result, err)
			}
		}
		current := bindingFor("current.ifc", false, "")
		future := bindingFor("later.ifc", false, "")
		whole := bindingFor("whole.gdb", true, "")
		nested := bindingFor("child/nested.ifc", false, "")
		cross := bindingFor("scene.3ds", false, scope.StringPath()+"/child/texture.png")
		check(current, true)
		check(future, true)
		check(whole, true)
		check(nested, false)
		check(cross, false)
		if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(base.EngineID), RequestID: grant.RequestID, Reason: "Withdraw NFS direct scope"}); err != nil {
			t.Fatal(err)
		}
		check(current, false)
		check(whole, false)
		recursive := true
		command.NodeRecursive = &recursive
		command.RequestID = uuid.New()
		var now time.Time
		if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			t.Fatal(err)
		}
		expiry := now.Add(2 * time.Second)
		command.ExpiryMode = shared.SharingExpiryAtTime
		command.ExpiresAt = &expiry
		finite, err := service.CreateIndependentGrant(ctx, command)
		if err != nil {
			t.Fatal(err)
		}
		check(nested, true)
		check(cross, true)
		time.Sleep(time.Until(expiry) + 20*time.Millisecond)
		check(nested, false)
		check(future, false)
		var revocations int64
		if err := db.Table("system.engine_access_grant_revocations").Where("request_id=?", finite.RequestID).Count(&revocations).Error; err != nil || revocations != 0 {
			t.Fatal("natural expiry was replaced by revocation", err)
		}
		raw, _ := json.Marshal(scope)
		for _, bad := range []string{`{}`, `{"version":"catalog.path/v1","segments":[]}`, `{"version":"catalog.path/v1","engine_id":` + fmt.Sprint(base.EngineID) + `,"segments":[{"term":"root","kind":"root","name":""},{}]}`} {
			var valid, covered bool
			if err := db.Raw("SELECT system.valid_source_node_path(?::jsonb,?),system.source_node_covers(?::jsonb,true,?::jsonb)", bad, base.EngineID, string(raw), bad).Row().Scan(&valid, &covered); err != nil || valid || covered {
				t.Fatalf("malformed scope: valid=%v covered=%v err=%v", valid, covered, err)
			}
		}
	})
	t.Run("MinIO bucket and prefix scope uses current complete boundaries", func(t *testing.T) {
		ctx := t.Context()
		// This existing disposable fixture changes profile only inside this
		// subtest; no real engine or source is contacted.
		if err := db.Exec("UPDATE system.engines SET engine_type='minio' WHERE tenant_id=? AND id=?", actor.TenantID, base.EngineID).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Exec("UPDATE system.engines SET engine_type='postgresql' WHERE tenant_id=? AND id=?", actor.TenantID, base.EngineID).Error; err != nil {
				t.Error(err)
			}
		})
		service.WithNodeIdentityReader(nodeIdentityReaderFunc(func(_ context.Context, tenant uint, q authorizationfacts.NodeIdentityQuery) (*authorizationfacts.NodeIdentity, error) {
			return &authorizationfacts.NodeIdentity{TenantID: tenant, Target: q.Target, Exists: true}, nil
		}))
		t.Cleanup(func() { service.WithNodeIdentityReader(nil) })
		user, _ := newUser(t, time.Hour)
		scope := plugin.ObjectDirectoryPath(base.EngineID, "models", " exact.dir ")
		direct := false
		command := CreateIndependentGrantInput{Actor: actor, EngineID: int64(base.EngineID), RequestID: uuid.New(), CatalogPath: scope, NodeRecursive: &direct, RecipientType: "user", RecipientID: user.PrincipalID, Action: shared.SourceAccessRead, ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "MinIO current and future items"}
		bucketCommand := command
		bucketCommand.RequestID = uuid.New()
		bucketCommand.CatalogPath = plugin.ObjectDirectoryPath(base.EngineID, "models", "")
		bucketCommand.Action = shared.SourceAccessMetadataRead
		bucketGrant, bucketErr := service.CreateIndependentGrant(ctx, bucketCommand)
		if bucketErr != nil {
			t.Fatal(bucketErr)
		}
		rootCommand := bucketCommand
		rootCommand.RequestID = uuid.New()
		rootCommand.CatalogPath = plugin.ObjectRootPath(base.EngineID)
		if _, e := service.CreateIndependentGrant(ctx, rootCommand); !errors.Is(e, commonapi.ErrBadRequest) {
			t.Fatalf("service root Grant admitted: %v", e)
		}
		if _, e := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(base.EngineID), RequestID: bucketGrant.RequestID, Reason: "End bucket setup"}); e != nil {
			t.Fatal(e)
		}
		service.WithNodeIdentityReader(nil)
		rejected := command
		rejected.RequestID = uuid.New()
		if _, err := service.CreateIndependentGrant(ctx, rejected); !errors.Is(err, ErrAuthorizationFactsUnavailable) {
			t.Fatalf("missing facts did not reject: %v", err)
		}
		var residue int64
		if err := db.Model(&sourceGrant{}).Where("request_id=?", rejected.RequestID).Count(&residue).Error; err != nil || residue != 0 {
			t.Fatal("failed issuance left a Grant", residue, err)
		}
		service.WithNodeIdentityReader(nodeIdentityReaderFunc(func(_ context.Context, tenant uint, q authorizationfacts.NodeIdentityQuery) (*authorizationfacts.NodeIdentity, error) {
			return &authorizationfacts.NodeIdentity{TenantID: tenant, Target: q.Target, Exists: true}, nil
		}))
		grant, err := service.CreateIndependentGrant(ctx, command)
		if err != nil {
			t.Fatal(err)
		}
		bindingFor := func(name string, whole bool, dependency string) authorizationfacts.Binding {
			t.Helper()
			primary := plugin.ObjectItemPath(base.EngineID, "models", " exact.dir /"+name)
			var boundary *authorizationfacts.AuthorizationBoundary
			if whole {
				primary = plugin.ObjectDirectoryPath(base.EngineID, "models", " exact.dir /"+name)
				boundary = &authorizationfacts.AuthorizationBoundary{Layout: "whole", MetadataTarget: &authorizationfacts.MetadataBoundary{Target: primary, Layout: "whole", DataType: "table", Format: "parquet", CompositionDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
			} else {
				refs := []authorizationfacts.FileCompositionRef{{Target: primary, Primary: true, Required: true, Role: "model"}}
				format, layout := "ifc", "single"
				if strings.HasSuffix(name, ".jpg") {
					format = "jpeg"
					refs[0].Role = "image"
				} else if strings.HasSuffix(name, ".tif") {
					format, layout = "tiff", "multi"
					refs[0].Role = "image"
					refs = append(refs, authorizationfacts.FileCompositionRef{Target: plugin.ObjectItemPath(base.EngineID, "models", " exact.dir /"+name+".aux.xml"), Role: "auxiliary_metadata"})
				}
				if dependency != "" {
					format = "3ds"
					refs = append(refs, authorizationfacts.FileCompositionRef{Target: plugin.ObjectItemPath(base.EngineID, "models", strings.TrimPrefix(dependency, "models/")), Required: true, Role: "texture"})
				}
				composition, err := authorizationfacts.NewScannedFileCompositionBoundary(format, layout, refs)
				if err != nil {
					t.Fatal(err)
				}
				boundary = &authorizationfacts.AuthorizationBoundary{Layout: layout, FileComposition: composition}
			}
			fp, err := authorizationfacts.PrimaryCandidateFingerprint(primary)
			if err != nil {
				t.Fatal(err)
			}
			facts := authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: base.EngineID, ItemType: "object", FullName: primary.StringPath(), ItemFingerprint: fp, BasisState: authorizationfacts.AuthorizationBasisComplete, Boundary: boundary, ObservedAt: time.Now().UTC()}
			hash, err := facts.ComputeBoundaryHash()
			if err != nil {
				t.Fatal(err)
			}
			facts.BoundaryHash = &hash
			binding, err := facts.MetadataBinding()
			if err != nil {
				t.Fatal(err)
			}
			return binding
		}
		check := func(binding authorizationfacts.Binding, want bool) {
			t.Helper()
			primary, err := binding.PrimaryTarget()
			if err != nil {
				t.Fatal(err)
			}
			result, err := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{Action: "metadata_read", TenantID: actor.TenantID, Source: user, Targets: []plugin.EngineCatalogPath{primary}, Bindings: []authorizationfacts.Binding{binding}})
			if err != nil || result == nil || result.Covered != want {
				t.Fatalf("covered=%v result=%+v err=%v", want, result, err)
			}
			if binding.MetadataTarget == nil {
				content, err := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{Action: "read", TenantID: actor.TenantID, Source: user, Targets: []plugin.EngineCatalogPath{primary}, Bindings: []authorizationfacts.Binding{binding}})
				if err != nil || content.Covered != want {
					t.Fatalf("content covered=%v result=%+v err=%v", want, content, err)
				}
			}
		}
		current := bindingFor("current.ifc", false, "")
		future := bindingFor("later.ifc", false, "")
		whole := bindingFor("whole.parquet", true, "")
		nested := bindingFor("child/nested.ifc", false, "")
		cross := bindingFor("scene.3ds", false, scope.StringPath()+"/child/texture.png")
		check(bindingFor("photo.jpg", false, ""), true)
		check(bindingFor("nested/scene.tif", false, ""), false)
		check(current, true)
		check(future, true)
		check(whole, true)
		check(nested, false)
		check(cross, false)
		other, _ := newUser(t, time.Hour)
		primary, _ := current.PrimaryTarget()
		foreign, err := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{Action: "read", TenantID: actor.TenantID, Source: other, Targets: []plugin.EngineCatalogPath{primary}, Bindings: []authorizationfacts.Binding{current}})
		if err != nil || foreign.Covered {
			t.Fatalf("unauthorized account read: %+v %v", foreign, err)
		}
		bindingSibling := bindingFor("sibling.ifc", false, "")
		bindingSibling.FileComposition.Refs[0].Target = plugin.ObjectItemPath(base.EngineID, "another-bucket", " exact.dir /sibling.ifc")
		if _, err := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{Action: "read", TenantID: actor.TenantID, Source: user, Targets: []plugin.EngineCatalogPath{primary}, Bindings: []authorizationfacts.Binding{bindingSibling}}); err == nil {
			t.Fatal("mismatched frozen boundary accepted")
		}

		if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(base.EngineID), RequestID: grant.RequestID, Reason: "Withdraw MinIO direct scope"}); err != nil {
			t.Fatal(err)
		}
		check(current, false)
		check(whole, false)
		metadataCommand := command
		metadataCommand.RequestID = uuid.New()
		metadataCommand.Action = shared.SourceAccessMetadataRead
		metadataGrant, err := service.CreateIndependentGrant(ctx, metadataCommand)
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{"metadata_read", "read"} {
			result, e := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{Action: action, TenantID: actor.TenantID, Source: user, Targets: []plugin.EngineCatalogPath{primary}, Bindings: []authorizationfacts.Binding{current}})
			if e != nil || result.Covered != (action == "metadata_read") {
				t.Fatalf("metadata-only scope upgraded read: %+v %v", result, e)
			}
		}
		if _, e := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(base.EngineID), RequestID: metadataGrant.RequestID, Reason: "Withdraw metadata independently"}); e != nil {
			t.Fatal(e)
		}

		recursive := true
		command.NodeRecursive = &recursive
		command.RequestID = uuid.New()
		var now time.Time
		if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			t.Fatal(err)
		}
		expiry := now.Add(2 * time.Second)
		command.ExpiryMode = shared.SharingExpiryAtTime
		command.ExpiresAt = &expiry
		finite, err := service.CreateIndependentGrant(ctx, command)
		if err != nil {
			t.Fatal(err)
		}
		check(bindingFor("nested/scene.tif", false, ""), true)
		check(nested, true)
		check(cross, true)
		time.Sleep(time.Until(expiry) + 20*time.Millisecond)
		check(nested, false)
		check(future, false)
		var revocations int64
		if err := db.Table("system.engine_access_grant_revocations").Where("request_id=?", finite.RequestID).Count(&revocations).Error; err != nil || revocations != 0 {
			t.Fatal("natural expiry was replaced by revocation", err)
		}
		raw, _ := json.Marshal(scope)
		for _, bad := range []string{`{}`, `{"version":"catalog.path/v1","segments":[]}`, `{"version":"catalog.path/v1","engine_id":` + fmt.Sprint(base.EngineID) + `,"segments":[{"term":"root","kind":"root","name":""},{}]}`} {
			var valid, covered bool
			if err := db.Raw("SELECT system.valid_source_node_path(?::jsonb,?),system.source_node_covers(?::jsonb,true,?::jsonb)", bad, base.EngineID, string(raw), bad).Row().Scan(&valid, &covered); err != nil || valid || covered {
				t.Fatalf("malformed scope: valid=%v covered=%v err=%v", valid, covered, err)
			}
		}
	})
}
