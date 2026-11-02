package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
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
}
