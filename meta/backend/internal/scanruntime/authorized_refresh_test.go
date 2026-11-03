package scanruntime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metarepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
)

type boundedRefreshProvider struct {
	filesystemScanTestProvider
	calls     []string
	recursive []bool
	afterList func()
}

func (p *boundedRefreshProvider) ListChildren(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	p.calls = append(p.calls, path.StringPath())
	p.recursive = append(p.recursive, opts.Recursive)
	entries, err := p.filesystemScanTestProvider.ListChildren(ctx, conn, path, opts)
	if p.afterList != nil {
		p.afterList()
	}
	return entries, err
}
func TestNonrecursiveRefreshPreservesUnscannedChildrenAndParentCompletion(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	repo := metarepo.NewScanRepository(db)
	runtime := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	provider := &boundedRefreshProvider{filesystemScanTestProvider: filesystemScanTestProvider{entriesByPath: map[string][]plugin.EngineCatalogEntry{"data": {
		{Name: "child", Role: plugin.EngineCatalogRoleBranch, Term: "directory", Kind: "directory", Path: plugin.FileDirectoryPath(26, "data/child"), Storage: &plugin.EngineCatalogStorageFacts{Path: "data/child"}},
		{Name: "new.csv", Role: plugin.EngineCatalogRoleLeaf, Term: "file", Kind: "file", Path: plugin.FileItemPath(26, "data/new.csv"), Storage: &plugin.EngineCatalogStorageFacts{Path: "data/new.csv"}},
	}}}}
	pluginRegisterForTest(t, provider)
	resource := &commonmodels.Engine{ID: 26, EngineType: provider.Type(), Name: "NFS"}
	_, parent, err := runtime.ensureFilesystemScanRoot(1, resource, provider, "data")
	if err != nil {
		t.Fatal(err)
	}
	_, child, err := runtime.ensureFilesystemScanRoot(1, resource, provider, "data/child")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.FinalizeNodeStateWithDepth(parent, "completed", "", "deep"); err != nil {
		t.Fatal(err)
	}
	before, err := repo.GetNodeByIDForTenant(1, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := repo.UpsertItemWithDepth(1, 26, child, "file", "private.csv", "data/child/private.csv", models.JSONMap{}, nil, nil, nil, "basic")
	if err != nil {
		t.Fatal(err)
	}
	ctx := scanresource.WithMetadataRefreshScope(t.Context(), &authorization.MetadataRefreshScope{Query: authorization.MetadataRefreshQuery{Kind: "node"}, Ranges: []authorization.MetadataRefreshRange{{Node: &authorization.SourceNodeScope{Target: plugin.FileDirectoryPath(26, "data")}}}})
	ctx = scanresource.WithDirectDirectoryRefresh(ctx)
	result, err := runtime.ScanPaths(ctx, resource, 1, []string{"data"}, scanflow.ScanDepthBasic, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(provider.calls, []string{"data"}) || !reflect.DeepEqual(provider.recursive, []bool{false}) || result.Items != 1 {
		t.Fatalf("source calls=%v recursive=%v result=%+v", provider.calls, provider.recursive, result)
	}
	if _, err := repo.GetItemByID(1, hidden.ID); err != nil {
		t.Fatal("unscanned item was deleted", err)
	}
	if _, err := repo.GetNodeByIDForTenant(1, child.ID); err != nil {
		t.Fatal("unscanned child was deleted", err)
	}
	after, err := repo.GetNodeByIDForTenant(1, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ScanStatus != before.ScanStatus || after.ScannedDepth != before.ScannedDepth || !after.ScannedAt.Equal(*before.ScannedAt) {
		t.Fatal("local refresh marked entire subtree completed")
	}
}

func TestRefreshDenialAfterAdmittedEnumerationStopsNextSourceUnit(t *testing.T) {
	for _, cause := range []string{"revoked", "expired", "dependency unavailable"} {
		t.Run(cause, func(t *testing.T) {
			denied := errors.New(cause)
			active := true
			provider := &boundedRefreshProvider{filesystemScanTestProvider: filesystemScanTestProvider{entriesByPath: map[string][]plugin.EngineCatalogEntry{"data": {{Name: "child", Role: plugin.EngineCatalogRoleBranch, Term: "directory", Kind: "directory", Path: plugin.FileDirectoryPath(26, "data/child"), Storage: &plugin.EngineCatalogStorageFacts{Path: "data/child"}}}}}, afterList: func() { active = false }}
			scope := &authorization.MetadataRefreshScope{Ranges: []authorization.MetadataRefreshRange{{Node: &authorization.SourceNodeScope{Target: plugin.FileDirectoryPath(26, "data"), Recursive: true}}}}
			ctx := scanresource.WithMetadataRefreshScope(t.Context(), scope)
			ctx = scanresource.WithCollectionReadCheck(ctx, func(context.Context, *commonmodels.Engine) error {
				if !active {
					return denied
				}
				return nil
			})
			// Both the normal directory runtime and whole-item rediscovery must use the
			// same read-unit guard; no fallback may issue the next recursive request.
			runtime := &FilesystemCatalogRuntime{}
			resource := &commonmodels.Engine{ID: 26}
			if _, _, err := runtime.listDirectory(ctx, resource, provider, nil, "data"); err != nil {
				t.Fatal(err)
			}
			if _, _, err := runtime.listDirectoryRecursive(ctx, resource, provider, nil, "data"); !errors.Is(err, denied) {
				t.Fatal(err)
			}
			if _, _, err := listKnownFileDirectory(ctx, resource, provider, nil, "data", true); !errors.Is(err, denied) {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(provider.calls, []string{"data"}) {
				t.Fatal("source contacted after denial", provider.calls)
			}
		})
	}
}
