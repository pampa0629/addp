package scanadapter_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanadapter"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanruntime"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type preciseCatalogBasePlugin struct {
	engineType string
	model      plugin.EngineCatalogModelSpec
	entries    []plugin.EngineCatalogEntry
}

func (p preciseCatalogBasePlugin) Type() string         { return p.engineType }
func (p preciseCatalogBasePlugin) DisplayName() string  { return "precise catalog test" }
func (p preciseCatalogBasePlugin) EngineOrigin() string { return "general" }
func (p preciseCatalogBasePlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p preciseCatalogBasePlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (p preciseCatalogBasePlugin) DefaultPort() int                                   { return 0 }
func (p preciseCatalogBasePlugin) RequiredFields() []string                           { return nil }
func (p preciseCatalogBasePlugin) SensitiveFields() []string                          { return nil }
func (p preciseCatalogBasePlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p preciseCatalogBasePlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec { return p.model }

type preciseCatalogPlugin struct {
	preciseCatalogBasePlugin
	calls     []string
	fail      bool
	failFacts bool
}

func (p *preciseCatalogPlugin) ListChildren(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	p.calls = append(p.calls, "list:"+path.StringPath())
	return p.entries, nil
}
func (p *preciseCatalogPlugin) ResolvePath(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	p.calls = append(p.calls, "resolve:"+path.StringPath())
	if p.fail {
		return nil, errors.New("source unavailable")
	}
	for _, entry := range p.entries {
		if reflect.DeepEqual(entry.Path, path) {
			return &entry, nil
		}
	}
	if len(path.Segments) == 2 && len(p.model.Levels) > 1 {
		return &plugin.EngineCatalogEntry{Name: path.Segments[1].Name, Path: path, Role: plugin.EngineCatalogRoleBranch}, nil
	}
	return nil, nil
}
func (p *preciseCatalogPlugin) DescribeEngineCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.calls = append(p.calls, "facts:"+path.StringPath())
	if p.failFacts {
		return nil, errors.New("source unavailable")
	}
	count := int64(2)
	return &plugin.EngineCatalogFacts{Path: path, Table: &datatype.TableInfo{Name: "A", RowCount: &count, Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}}}, Graph: &datatype.GraphInfo{}}, nil
}
func (p *preciseCatalogPlugin) SampleDynamicSchema(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.calls = append(p.calls, "sample:"+path.StringPath())
	return p.DescribeEngineCatalogFacts(ctx, conn, path, opts)
}

func TestPreciseCatalogScan(t *testing.T) {
	testPreciseCatalogScan(t, func(t *testing.T) *gorm.DB { return metatest.OpenMetadataDB(t) })
}

func TestPreciseCatalogScanAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("META_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	testPreciseCatalogScan(t, func(t *testing.T) *gorm.DB {
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		t.Cleanup(func() { tx.Rollback() })
		if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS meta").Error; err != nil {
			t.Fatal(err)
		}
		if err := tx.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}); err != nil {
			t.Fatal(err)
		}
		return tx
	})
}

func testPreciseCatalogScan(t *testing.T, openDB func(*testing.T) *gorm.DB) {
	cases := []struct {
		name  string
		model plugin.EngineCatalogModelSpec
		typ   string
	}{
		{"table", plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema), "table"},
		{"collection", plugin.DynamicSchemaCatalogModel(), "collection"},
		{"graph", plugin.GraphCatalogModel(), "graph"},
		{"topic", plugin.EngineCatalogModelSpec{PathVersion: plugin.EngineCatalogPathVersion, RootTerm: plugin.EngineCatalogTermService, Levels: []plugin.EngineCatalogLevelSpec{{Term: "topic", Kinds: []string{"topic"}, Role: plugin.EngineCatalogRoleLeaf}}}, "topic"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openDB(t)
			repo := metaRepo.NewScanRepository(db)
			p := &preciseCatalogPlugin{preciseCatalogBasePlugin: preciseCatalogBasePlugin{engineType: "precise-" + tc.name, model: tc.model}}
			resource := &commonModels.Engine{ID: 91, EngineType: p.Type(), Name: "source"}
			parts := []string{"A"}
			if len(tc.model.Levels) > 1 {
				parts = []string{"sales.v2", "A"}
			}
			loc := &resourcetree.ResourceLocator{EngineID: resource.ID, Type: resourcetree.ResourceType(tc.typ), Path: parts}
			path, err := resourcetree.EngineCatalogPathFromLocator(tc.model, loc)
			if err != nil {
				t.Fatal(err)
			}
			last := path.Segments[len(path.Segments)-1]
			p.entries = []plugin.EngineCatalogEntry{{Name: "A", Path: path, Term: last.Term, Kind: last.Kind, Role: plugin.EngineCatalogRoleLeaf}}
			root, err := metaRepo.EnsureEngineCatalogRootNode(repo, 91, resource, p)
			if err != nil {
				t.Fatal(err)
			}
			parent := root
			if len(parts) > 1 {
				parent, err = repo.UpsertNode(91, 91, root, path.Segments[1].Term, parts[0], nil, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			for _, n := range []*models.MetaNode{root, parent} {
				if err := db.Model(n).Updates(map[string]interface{}{"scan_status": "failed", "scan_error": "old failure", "scanned_depth": "basic", "scanned_at": now}).Error; err != nil {
					t.Fatal(err)
				}
			}
			var before []models.MetaNode
			if err := db.Order("id").Find(&before).Error; err != nil {
				t.Fatal(err)
			}
			fullName := "B"
			if len(parts) > 1 {
				fullName = parts[0] + ".B"
			}
			_, err = repo.UpsertItemWithDepth(91, 91, parent, tc.typ, "B", fullName, models.JSONMap{}, nil, nil, nil, "basic")
			if err != nil {
				t.Fatal(err)
			}
			var siblingBefore models.MetaItem
			if err := db.Where("name = ?", "B").First(&siblingBefore).Error; err != nil {
				t.Fatal(err)
			}
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			d := scanadapter.NewEngineCatalogScanDispatcher(db, repo, log, scanruntime.NewDatabaseRuntime(db, log, repo, nil), scanruntime.NewBranchLeafRuntime(db, log, repo), scanruntime.NewDirectLeafRuntime(log, repo), nil)
			req := scanflow.DispatchRequest{Context: context.Background(), Resource: resource, EnginePlugin: p, TenantID: 91, Targets: []string{loc.ToURI()}, ScanDepth: "deep", Force: true}
			result, err := d.Dispatch(req)
			if err != nil || result.Items != 1 || result.CatalogNodes != 0 {
				t.Fatalf("exact scan: %+v %v", result, err)
			}
			for _, call := range p.calls {
				if call != "resolve:"+path.StringPath() && call != "facts:"+path.StringPath() && call != "sample:"+path.StringPath() {
					t.Fatalf("scope expanded: %s", call)
				}
			}
			var after []models.MetaNode
			if err := db.Order("id").Find(&after).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("leaf scan modified parent scan facts")
			}
			var siblingAfter models.MetaItem
			if err := db.Where("name = ?", "B").First(&siblingAfter).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(siblingBefore, siblingAfter) {
				t.Fatal("leaf scan changed sibling")
			}
			p.failFacts = true
			if _, err := d.Dispatch(req); err == nil {
				t.Fatal("leaf failure reported success")
			}
			p.failFacts = false
			after = nil
			if err := db.Order("id").Find(&after).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed leaf changed parent scan facts")
			}
			// Invalid, cross-engine and missing targets cannot become root scans.
			for _, uri := range []string{"invalid", "addp://engine/92/path/A?type=" + tc.typ, " "} {
				p.calls = nil
				req.Targets = []string{uri}
				if _, err := d.Dispatch(req); err == nil {
					t.Fatalf("accepted %q", uri)
				}
				if len(p.calls) != 0 {
					t.Fatalf("invalid target reached provider: %v", p.calls)
				}
			}
			missing := *loc
			missing.Path = append([]string(nil), loc.Path...)
			missing.Path[len(missing.Path)-1] = "missing"
			p.calls = nil
			req.Targets = []string{missing.ToURI()}
			if _, err := d.Dispatch(req); err == nil || len(p.calls) != 1 {
				t.Fatalf("missing target expanded: %v %v", p.calls, err)
			}
			p.calls = nil
			req.Targets = []string{loc.ToURI(), missing.ToURI()}
			_, err = d.Dispatch(req)
			failed, samples := scanflow.FailedTargetDetails(err)
			if failed != 1 || len(samples) != 1 || samples[0].Target != missing.ToURI() {
				t.Fatalf("resolution failure attributed to the wrong target: %v %v", samples, err)
			}
			if len(p.calls) != 2 {
				t.Fatalf("resolution failure started a scan: %v", p.calls)
			}
			// Explicit full range keeps the existing cleanup semantics, including dots in names.
			if len(parts) > 1 {
				req.Targets = nil
				req.CatalogPaths = []string{parts[0]}
			} else {
				req.Targets = nil
			}
			p.calls = nil
			if _, err := d.Dispatch(req); err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := db.Model(&models.MetaItem{}).Where("name = ?", "B").Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("full scan failed to clean missing sibling: %d %v", count, err)
			}
			if len(parts) > 1 && (len(p.calls) == 0 || p.calls[0] != "list:"+parts[0]) {
				t.Fatalf("branch name truncated: %v", p.calls)
			}
			if len(parts) > 1 {
				var got models.MetaNode
				if err := db.First(&got, root.ID).Error; err != nil {
					t.Fatal(err)
				}
				if got.ScanStatus != "failed" || got.ScannedDepth != "basic" || got.ScannedAt == nil || !got.ScannedAt.Equal(now) {
					t.Fatal("branch scan marked the engine root complete")
				}
			}
		})
	}
}

type preciseContentAdapter struct {
	groups      []models.ScanRefGroup
	pathsCalled bool
}

func (a *preciseContentAdapter) ScanPaths(context.Context, *commonModels.Engine, uint, []string, string, bool, scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	a.pathsCalled = true
	return scanflow.DispatchResult{}, errors.New("leaf scan must not enumerate paths")
}
func (a *preciseContentAdapter) ScanRefGroups(_ context.Context, _ *commonModels.Engine, _ uint, groups []models.ScanRefGroup, _ string, _ bool, _ scanflow.ProgressReporter) (scanflow.DispatchResult, error) {
	a.groups = groups
	return scanflow.DispatchResult{Items: 1}, nil
}

func TestContentLocatorRetainsExactBoundaryAndRootState(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model plugin.EngineCatalogModelSpec
		parts []string
	}{
		{"file", plugin.FileCatalogModel(), []string{"results", "A.tif"}},
		{"object", plugin.ObjectCatalogModel(), []string{"bucket", "results", "A.tif"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := metatest.OpenMetadataDB(t)
			repo := metaRepo.NewScanRepository(db)
			p := &preciseCatalogPlugin{preciseCatalogBasePlugin: preciseCatalogBasePlugin{engineType: "precise-content-" + tc.name, model: tc.model}}
			resource := &commonModels.Engine{ID: 91, EngineType: p.Type()}
			loc := &resourcetree.ResourceLocator{EngineID: 91, Type: resourcetree.ResourceType(tc.name), Path: tc.parts}
			path, err := resourcetree.EngineCatalogPathFromLocator(tc.model, loc)
			if err != nil {
				t.Fatal(err)
			}
			last := path.Segments[len(path.Segments)-1]
			p.entries = []plugin.EngineCatalogEntry{{Name: last.Name, Path: path, Term: last.Term, Kind: last.Kind, Role: plugin.EngineCatalogRoleLeaf}}
			root, err := metaRepo.EnsureEngineCatalogRootNode(repo, 91, resource, p)
			if err != nil {
				t.Fatal(err)
			}
			var before models.MetaNode
			if err := db.First(&before, root.ID).Error; err != nil {
				t.Fatal(err)
			}
			adapter := &preciseContentAdapter{}
			d := scanadapter.NewEngineCatalogScanDispatcher(db, repo, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, nil, scanadapter.NewEngineCatalogContentScanner(adapter, adapter))
			result, err := d.Dispatch(scanflow.DispatchRequest{Resource: resource, EnginePlugin: p, TenantID: 91, Targets: []string{loc.ToURI()}, ScanDepth: "deep"})
			if err != nil || result.Items != 1 || adapter.pathsCalled || len(adapter.groups) != 1 || adapter.groups[0].Primary != path.StringPath() {
				t.Fatalf("content boundary: %+v %v %+v", result, err, adapter)
			}
			var after models.MetaNode
			if err := db.First(&after, root.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("content leaf marked root scan complete")
			}
		})
	}
}

type preciseNativePostgresPlugin struct {
	*postgresql.PostgreSQLPlugin
	t *testing.T
}

func (p *preciseNativePostgresPlugin) ListChildren(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	p.t.Fatal("native single-table scan enumerated parent catalog")
	return nil, errors.New("unexpected catalog enumeration")
}

func TestPreciseNativePostgresScanAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("META_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	uri, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	schema, role, password := "precise_source_"+suffix, "precise_reader_"+suffix, rand.Text()
	if err := db.Exec(fmt.Sprintf(`CREATE ROLE "%s" LOGIN PASSWORD '%s'`, role, password)).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, statement := range []string{fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schema), fmt.Sprintf(`DROP OWNED BY "%s"`, role), fmt.Sprintf(`DROP ROLE "%s"`, role)} {
			if err := db.Exec(statement).Error; err != nil {
				t.Errorf("cleanup native scope fixture: %v", err)
			}
		}
		var remains bool
		if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname=?) OR EXISTS (SELECT 1 FROM pg_namespace WHERE nspname=?)`, role, schema).Scan(&remains).Error; err != nil || remains {
			t.Errorf("native source fixture remains=%v error=%v", remains, err)
		}
	})
	for _, statement := range []string{
		fmt.Sprintf(`CREATE SCHEMA "%s"`, schema),
		fmt.Sprintf(`CREATE TABLE "%s"."A" (id bigint PRIMARY KEY, value text)`, schema),
		fmt.Sprintf(`CREATE TABLE "%s"."B" (id bigint PRIMARY KEY, private_value text)`, schema),
		fmt.Sprintf(`GRANT USAGE ON SCHEMA "%s" TO "%s"`, schema, role),
		fmt.Sprintf(`GRANT SELECT ON "%s"."A" TO "%s"`, schema, role),
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS meta").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}); err != nil {
		t.Fatal(err)
	}
	p := &preciseNativePostgresPlugin{PostgreSQLPlugin: &postgresql.PostgreSQLPlugin{}, t: t}
	resource := &commonModels.Engine{ID: 931, EngineType: p.Type(), Name: "native exact source", ConnectionInfo: commonModels.ConnectionInfo{
		"host": uri.Hostname(), "port": uri.Port(), "database": strings.TrimPrefix(uri.Path, "/"), "user": role, "password": password, "sslmode": uri.Query().Get("sslmode"),
	}}
	t.Cleanup(func() { plugin.ClosePool(resource.ID) })
	repo := metaRepo.NewScanRepository(tx)
	root, err := metaRepo.EnsureEngineCatalogRootNode(repo, 931, resource, p)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := repo.UpsertNode(931, resource.ID, root, plugin.EngineCatalogTermSchema, schema, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range []*models.MetaNode{root, parent} {
		if err := tx.Model(node).Updates(map[string]interface{}{"scan_status": "failed", "scan_error": "previous range failure", "scanned_depth": "basic"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.UpsertItemWithDepth(931, resource.ID, parent, "table", "B", schema+".B", models.JSONMap{"sentinel": "retain"}, nil, nil, nil, "basic"); err != nil {
		t.Fatal(err)
	}
	var siblingBefore models.MetaItem
	if err := tx.Where("tenant_id = ? AND engine_id = ? AND name = ?", 931, resource.ID, "B").First(&siblingBefore).Error; err != nil {
		t.Fatal(err)
	}
	var before []models.MetaNode
	if err := tx.Where("tenant_id = ? AND engine_id = ?", 931, resource.ID).Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dispatcher := scanadapter.NewEngineCatalogScanDispatcher(tx, repo, log, scanruntime.NewDatabaseRuntime(tx, log, repo, nil), nil, nil, nil)
	locator := &resourcetree.ResourceLocator{EngineID: resource.ID, Type: resourcetree.ResourceType("table"), Path: []string{schema, "A"}}
	req := scanflow.DispatchRequest{Context: t.Context(), Resource: resource, EnginePlugin: p, TenantID: 931, Targets: []string{locator.ToURI()}, ScanDepth: "deep", Force: true}
	result, err := dispatcher.Dispatch(req)
	if err != nil || result.Items != 1 || result.Fields != 2 || result.CatalogNodes != 0 {
		t.Fatalf("native exact scan: result=%+v error=%v", result, err)
	}
	var target models.MetaItem
	if err := tx.Where("tenant_id = ? AND engine_id = ? AND name = ?", 931, resource.ID, "A").First(&target).Error; err != nil {
		t.Fatal(err)
	}
	if target.ScannedDepth != "deep" {
		t.Fatalf("native target depth=%s", target.ScannedDepth)
	}
	for _, table := range []string{"B", "missing"} {
		locator.Path[1] = table
		req.Targets = []string{locator.ToURI()}
		if _, err := dispatcher.Dispatch(req); err == nil {
			t.Fatalf("native scan accepted unreadable target %s", table)
		}
	}
	locator.Path[1] = "A"
	req.Targets = []string{locator.ToURI()}
	if err := db.Exec(fmt.Sprintf(`REVOKE USAGE ON SCHEMA "%s" FROM "%s"`, schema, role)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.Dispatch(req); err == nil {
		t.Fatal("native scan accepted revoked schema visibility")
	}
	var after []models.MetaNode
	if err := tx.Where("tenant_id = ? AND engine_id = ?", 931, resource.ID).Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("native leaf scan modified parent range facts")
	}
	var siblingAfter models.MetaItem
	if err := tx.First(&siblingAfter, siblingBefore.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(siblingBefore, siblingAfter) {
		t.Fatal("native leaf scan changed unrelated metadata")
	}
}
