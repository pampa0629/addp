package scanresource

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
)

func TestRefreshScopeSeparatesEnumerationFromExactComposition(t *testing.T) {
	parent := plugin.FileDirectoryPath(9, "data")
	single := refreshMetadataBinding(t, plugin.FileItemPath(9, "data/roads.shp"), "multi")
	scope := &authorization.MetadataRefreshScope{Ranges: []authorization.MetadataRefreshRange{{Item: &single}}}
	ctx := WithMetadataRefreshScope(t.Context(), scope)
	if !errors.Is(CheckRefreshEnumeration(ctx, parent, false), commonapi.ErrForbidden) {
		t.Fatal("exact item authorized parent enumeration")
	}
	sidecar := plugin.FileItemPath(9, "data/roads.dbf")
	if CheckRefreshContent(ctx, sidecar) == nil {
		t.Fatal("unproved sidecar allowed")
	}
	ctx = WithVerifiedRefreshItemRefs(ctx, []plugin.EngineCatalogPath{sidecar})
	if err := CheckRefreshContent(ctx, sidecar); err != nil {
		t.Fatal(err)
	}
	if CheckRefreshContent(ctx, plugin.FileItemPath(9, "data/other.dbf")) == nil || CheckRefreshContent(ctx, plugin.FileItemPath(10, "data/roads.dbf")) == nil {
		t.Fatal("refs expanded to another item or engine")
	}
	if CheckRefreshEnumeration(ctx, parent, false) == nil {
		t.Fatal("verified refs enabled directory discovery")
	}
	scope.Ranges = []authorization.MetadataRefreshRange{{Node: &authorization.SourceNodeScope{Target: parent}}}
	if CheckRefreshEnumeration(ctx, parent, false) != nil || CheckRefreshEnumeration(ctx, parent, true) == nil || CheckRefreshEnumeration(ctx, plugin.FileDirectoryPath(9, "data/child"), false) == nil {
		t.Fatal("nonrecursive node boundary changed")
	}
	if CheckRefreshContent(ctx, plugin.FileItemPath(9, "data/new.ifc")) != nil || CheckRefreshContent(ctx, plugin.FileItemPath(9, "data/child/private.ifc")) == nil {
		t.Fatal("direct current/future membership boundary changed")
	}
	whole := refreshMetadataBinding(t, plugin.FileDirectoryPath(9, "data/maps.gdb"), "whole")
	scope.Ranges = []authorization.MetadataRefreshRange{{Item: &whole}}
	if CheckRefreshEnumeration(ctx, plugin.FileDirectoryPath(9, "data/maps.gdb"), true) != nil || CheckRefreshContent(ctx, plugin.FileItemPath(9, "data/maps.gdb/new.gdbtable")) != nil || CheckRefreshContent(ctx, plugin.FileItemPath(9, "data/private.gdb/table")) == nil {
		t.Fatal("whole container leaked its boundary")
	}
}

type refreshTestReader struct {
	plugin.ContentReadableProvider
	opens int
}

func (r *refreshTestReader) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	r.opens++
	return io.NopCloser(strings.NewReader("value")), nil
}

type refreshTestRangeReader struct {
	*refreshTestReader
	rangeOpens int
}

func (r *refreshTestRangeReader) OpenRange(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	r.rangeOpens++
	return io.NopCloser(strings.NewReader("val")), nil
}

func TestRefreshReaderRevocationExpiryCancellationAndOptionalRange(t *testing.T) {
	for _, cause := range []string{"revoked", "expired", "dependency failed"} {
		t.Run(cause, func(t *testing.T) {
			path := plugin.FileItemPath(9, "data/a.ifc")
			ctx := WithMetadataRefreshScope(t.Context(), &authorization.MetadataRefreshScope{Ranges: []authorization.MetadataRefreshRange{{Node: &authorization.SourceNodeScope{Target: plugin.FileDirectoryPath(9, "data")}}}})
			denied := errors.New(cause)
			active := true
			ctx = WithCollectionReadCheck(ctx, func(_ context.Context, resource *commonmodels.Engine) error {
				if resource.ID != 9 {
					t.Fatal("wrong source")
				}
				if !active {
					return denied
				}
				return nil
			})
			raw := &refreshTestReader{}
			ordinary := BoundRefreshReader(ctx, &commonmodels.Engine{ID: 9}, raw)
			if _, invented := ordinary.(plugin.RangeReadableProvider); invented {
				t.Fatal("invented range capability")
			}
			reader := &refreshTestRangeReader{refreshTestReader: raw}
			bounded := BoundRefreshReader(ctx, &commonmodels.Engine{ID: 9}, reader)
			inFlight, err := bounded.OpenContent(t.Context(), nil, path, plugin.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			active = false
			if b, err := io.ReadAll(inFlight); err != nil || string(b) != "value" {
				t.Fatal("already admitted read was interrupted")
			}
			inFlight.Close()
			if _, err := bounded.OpenContent(t.Context(), nil, path, plugin.ReadOptions{}); !errors.Is(err, denied) {
				t.Fatal(err)
			}
			if _, err := bounded.(plugin.RangeReadableProvider).OpenRange(t.Context(), nil, path, plugin.ReadOptions{}); !errors.Is(err, denied) {
				t.Fatal(err)
			}
			if raw.opens != 1 || reader.rangeOpens != 0 {
				t.Fatal("denial contacted source")
			}
			cancelCtx, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := BoundRefreshReader(cancelCtx, &commonmodels.Engine{ID: 9}, reader).OpenContent(t.Context(), nil, path, plugin.ReadOptions{}); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if raw.opens != 1 {
				t.Fatal("cancelled source read")
			}
		})
	}
}

func refreshMetadataBinding(t *testing.T, target plugin.EngineCatalogPath, layout string) authorizationfacts.Binding {
	t.Helper()
	facts := authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: target.EngineID, ItemType: "file", FullName: target.StringPath(), ItemFingerprint: commonmodels.GenerateItemFingerprint(target.EngineID, target.StringPath()), BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now(), Boundary: &authorizationfacts.AuthorizationBoundary{Layout: layout, MetadataTarget: &authorizationfacts.MetadataBoundary{Target: target, Layout: layout, CompositionDigest: strings.Repeat("a", 64)}}}
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

func TestUploadIdentificationCanReadExactCommittedMembersButNeverEnumerateParent(t *testing.T) {
	target := plugin.FileItemPath(9, "dir/a.ifc")
	ctx := WithCommittedContentTargets(t.Context(), []plugin.EngineCatalogPath{target})
	if err := CheckRefreshContent(ctx, target); err != nil {
		t.Fatal(err)
	}
	if err := CheckRefreshContent(ctx, plugin.FileItemPath(9, "dir/other.ifc")); err == nil {
		t.Fatal("upload scope read an unrelated file")
	}
	if err := CheckRefreshEnumeration(ctx, plugin.FileDirectoryPath(9, "dir"), false); err == nil {
		t.Fatal("upload scope enumerated parent")
	}
}
