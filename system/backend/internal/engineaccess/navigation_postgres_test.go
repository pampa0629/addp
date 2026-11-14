package engineaccess

import (
	"context"
	"encoding/json"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"testing"
	"time"
)

func exerciseEngineNavigation(t *testing.T, db *gorm.DB, s *Service, ctx context.Context, actor Actor, user userProvenance, credential string, target plugin.EngineCatalogPath, facts authorizationfacts.AuthorizationFacts) {
	originalReader := s.nodeIdentities
	defer s.WithNodeIdentityReader(originalReader)
	s.WithNodeIdentityReader(nodeIdentityReaderFunc(func(_ context.Context, tenant uint, q authorizationfacts.NodeIdentityQuery) (*authorizationfacts.NodeIdentity, error) {
		return &authorizationfacts.NodeIdentity{TenantID: tenant, Target: q.Target, Exists: true}, nil
	}))
	revoke := func(id uuid.UUID) {
		t.Helper()
		if _, err := s.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(target.EngineID), RequestID: id, Reason: "Navigation fixture cleanup"}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(paths []plugin.EngineCatalogPath, want []bool) {
		t.Helper()
		q := shared.SourceNavigationCheckRequest{Targets: paths}
		got, err := s.checkNavigation(ctx, credential, q, false, func(c shared.AuthContext) error { return qualifyOrdinarySourceUser(c, ManagerPreviewReadPermission) })
		if err == nil && len(paths) == 1 && len(paths[0].Segments) == 1 {
			got, err = s.CheckManagerNavigation(ctx, credential, q)
		}
		if err != nil || !got.MatchesNavigation(q) {
			t.Fatal(got, err)
		}
		for i, w := range want {
			if got.Targets[i].Visible != w {
				t.Fatalf("navigation %d want %v got %+v", i, w, got)
			}
		}
	}
	root := plugin.FileDirectoryPath(target.EngineID, "")
	parent := root
	parent.Segments = append([]plugin.EngineCatalogSegment(nil), target.Segments[:len(target.Segments)-1]...)
	empty := plugin.FileDirectoryPath(target.EngineID, "empty")
	check([]plugin.EngineCatalogPath{root, parent, empty}, []bool{false, false, false})
	cmd := CreateIndependentGrantInput{Actor: actor, EngineID: int64(target.EngineID), RequestID: uuid.New(), CatalogPath: target, ItemFingerprint: facts.ItemFingerprint, ExpectedBoundaryHash: *facts.BoundaryHash, RecipientType: "user", RecipientID: user.PrincipalID, Action: shared.SourceAccessMetadataRead, ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Independent navigation item"}
	precise, err := s.CreateIndependentGrant(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	check([]plugin.EngineCatalogPath{root, parent, empty}, []bool{true, true, false})
	node := cmd
	node.RequestID = uuid.New()
	node.CatalogPath = root
	node.ItemFingerprint = ""
	node.ExpectedBoundaryHash = ""
	recursive := true
	node.NodeRecursive = &recursive
	grant, err := s.CreateIndependentGrant(ctx, node)
	if err != nil {
		t.Fatal(err)
	}
	check([]plugin.EngineCatalogPath{root, parent, empty}, []bool{true, true, true})
	revoke(grant.RequestID)
	check([]plugin.EngineCatalogPath{root, parent, empty}, []bool{true, true, false})
	revoke(precise.RequestID)
	check([]plugin.EngineCatalogPath{root, parent, empty}, []bool{false, false, false})
	node.RequestID = uuid.New()
	node.CatalogPath = empty
	direct := false
	node.NodeRecursive = &direct
	grant, err = s.CreateIndependentGrant(ctx, node)
	if err != nil {
		t.Fatal(err)
	}
	check([]plugin.EngineCatalogPath{root, parent, empty}, []bool{true, false, true})
	revoke(grant.RequestID)
	// All three root profiles use the same PostgreSQL rule storage and lifecycle.
	profile := *s // The parent TIFF fixture intentionally enables only its single profile.
	profile.catalogEligible = func(e *models.Engine) bool {
		return e.EngineType == "postgresql" || e.EngineType == "nfs" || e.EngineType == "minio"
	}
	defer func() {
		if err := db.Exec("UPDATE system.engines SET engine_type='nfs' WHERE id=? AND tenant_id=?", target.EngineID, actor.TenantID).Error; err != nil {
			t.Error(err)
		}
	}()
	for _, kind := range []string{"postgresql", "nfs", "minio"} {
		if err := db.Exec("UPDATE system.engines SET engine_type=? WHERE id=? AND tenant_id=?", kind, target.EngineID, actor.TenantID).Error; err != nil {
			t.Fatal(err)
		}
		path := root
		switch kind {
		case "postgresql":
			path = plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), target.EngineID)
		case "minio":
			path = plugin.ObjectRootPath(target.EngineID)
		}
		node.RequestID = uuid.New()
		node.CatalogPath = path
		node.NodeRecursive = &recursive
		grant, err = profile.CreateIndependentGrant(ctx, node)
		if err != nil {
			t.Fatal(kind, err)
		}
		check([]plugin.EngineCatalogPath{path}, []bool{true})
		future := plugin.FileItemPath(target.EngineID, "future/new.csv")
		switch kind {
		case "postgresql":
			future = plugin.TabularItemPath(target.EngineID, "schema", "future", "new_table")
		case "minio":
			future = plugin.ObjectItemPath(target.EngineID, "future-bucket", "new.csv")
		}
		rawNode, _ := json.Marshal(path)
		rawFuture, _ := json.Marshal(future)
		var covered bool
		if err := db.Raw("SELECT system.source_node_covers(?::jsonb,true,?::jsonb)", string(rawNode), string(rawFuture)).Scan(&covered).Error; err != nil || !covered {
			t.Fatal("future item excluded", err)
		}
		if _, e := profile.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(target.EngineID), RequestID: grant.RequestID, Reason: "Remove engine root"}); e != nil {
			t.Fatal(kind, e)
		}
		check([]plugin.EngineCatalogPath{path}, []bool{false})
	}
	if err := db.Exec("UPDATE system.engines SET engine_type='nfs' WHERE id=? AND tenant_id=?", target.EngineID, actor.TenantID).Error; err != nil {
		t.Fatal(err)
	}
	node.RequestID = uuid.New()
	node.CatalogPath = empty
	node.NodeRecursive = &direct
	node.ExpiryMode = shared.SharingExpiryAtTime
	var expiry time.Time
	if err := db.Raw("SELECT clock_timestamp()+interval '2 seconds'").Scan(&expiry).Error; err != nil {
		t.Fatal(err)
	}
	node.ExpiresAt = &expiry
	grant, err = s.CreateIndependentGrant(ctx, node)
	if err != nil {
		t.Fatal(err)
	}
	check([]plugin.EngineCatalogPath{root, empty}, []bool{true, true})
	if err := db.Exec("SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp())))+0.03)", expiry).Error; err != nil {
		t.Fatal(err)
	}
	check([]plugin.EngineCatalogPath{root, empty}, []bool{false, false})
	t.Log("navigation: ungranted hidden, precise ancestors, empty node, root revocation preserves precise item, PG/NFS/MinIO future items, expiry passed")
}
