package preview

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

func TestWholeFileGDBContentAlwaysChecksCurrentUserBeforeRuntime(t *testing.T) {
	source := &PreviewRequest{ScopePath: "exports/test.gdb", ChildName: "future_table", Attributes: map[string]any{"item": map[string]any{"layout": "whole", "data_type": "container", "format": "filegdb"}}}
	req := &PreviewResolverRequest{Locator: &resourcetree.ResourceLocator{EngineID: 14, Path: []string{"exports", "test.gdb"}}, Engine: &commonmodels.Engine{ID: 14, EngineType: "nfs"}, ItemFullName: source.ScopePath, ItemFingerprint: commonmodels.GenerateItemFingerprint(14, source.ScopePath)}
	if err := checkWholeContainerRead(context.Background(), req, source); !errors.Is(err, ErrEngineAccessDenied) {
		t.Fatal("missing User check allowed content", err)
	}
	calls := 0
	denied := errors.New("current grant revoked")
	req.CheckWholeContainerRead = func(_ context.Context, path plugin.EngineCatalogPath) error {
		calls++
		if path.StringPath() != source.ScopePath || path.EngineID != 14 || path.Segments[len(path.Segments)-1].Term != "directory" {
			t.Fatal("wrong whole target")
		}
		return denied
	}
	if err := checkWholeContainerRead(context.Background(), req, source); !errors.Is(err, denied) || calls != 1 {
		t.Fatal("current denial ignored", err, calls)
	}
	denied = nil
	if err := checkWholeContainerRead(context.Background(), req, source); err != nil || calls != 2 {
		t.Fatal("future child is outside authorized whole", err)
	}
	req.Locator.Path = []string{"exports", "other.gdb"}
	if err := checkWholeContainerRead(context.Background(), req, source); !errors.Is(err, ErrEngineAccessDenied) || calls != 2 {
		t.Fatal("different runtime source reached checker", err)
	}
	req.Locator.Path = []string{"exports", "test.gdb"}
	source.RefPath = "../outside.gdb"
	if err := checkWholeContainerRead(context.Background(), req, source); !errors.Is(err, ErrEngineAccessDenied) || calls != 2 {
		t.Fatal("external reference reached checker", err)
	}
	source.RefPath = ""
	req.ItemFingerprint = commonmodels.GenerateItemFingerprint(14, "exports/other.gdb")
	if err := checkWholeContainerRead(context.Background(), req, source); !errors.Is(err, ErrEngineAccessDenied) {
		t.Fatal("wrong identity allowed")
	}
	source.ChildName = ""
	if err := checkWholeContainerRead(context.Background(), req, source); err != nil || calls != 2 {
		t.Fatal("parent Meta index triggered source authorization", err)
	}
}

func TestResolverDeniesWholeReadBeforeProviderAndRuntimeResolution(t *testing.T) {
	previous, previousErr := plugin.Get("nfs")
	source := &wholePreviewSource{recordingFileCatalogPreviewPlugin: &recordingFileCatalogPreviewPlugin{engineType: "nfs"}}
	plugin.Register(source)
	t.Cleanup(func() {
		if previousErr == nil {
			plugin.Register(previous)
		} else {
			plugin.Unregister("nfs")
		}
	})
	registry := NewPreviewRegistry()
	registry.Register(namedPreviewProvider{name: "builtin:scope-table"})
	resolver := NewPreviewResolver(registry, nil, nil)
	attrs := map[string]interface{}{"item": map[string]interface{}{"layout": "whole", "data_type": "container", "format": "filegdb"}}
	req := &PreviewResolverRequest{Locator: &resourcetree.ResourceLocator{EngineID: 14, Path: []string{"exports", "test.gdb"}, Type: resourcetree.TypeFile},
		Engine: &commonmodels.Engine{ID: 14, EngineType: "nfs"}, Metadata: &commonmodels.MetaNode{Attributes: attrs}, ItemType: "file", ItemFullName: "exports/test.gdb", ItemFingerprint: commonmodels.GenerateItemFingerprint(14, "exports/test.gdb"), ScopePath: "exports/test.gdb", ChildName: "new_table"}
	denied := errors.New("current user cannot read whole FileGDB")
	calls := 0
	req.CheckWholeContainerRead = func(context.Context, plugin.EngineCatalogPath) error { calls++; return denied }
	result, err := resolver.Preview(context.Background(), req)
	if result != nil || !errors.Is(err, denied) || calls != 1 {
		t.Fatalf("must reject before runtime resolution: result=%v err=%v calls=%d", result, err, calls)
	}
	if source.openContentCalls != 0 || source.describeCalls != 0 || source.listChildrenCalls != 0 {
		t.Fatal("unauthorized source I/O")
	}
}

func TestFileGDBMalformedIdentityCannotSkipWholeReadGuard(t *testing.T) {
	for _, identity := range []map[string]any{
		{"format": "filegdb", "layout": "single", "data_type": "container"},
		{"format": "filegdb", "layout": "whole", "data_type": "table"},
		{"format": "filegdb", "layout": "whole"},
	} {
		for _, child := range []string{"", "people"} {
			source := &PreviewRequest{ScopePath: "exports/test.gdb", ChildName: child, Attributes: map[string]any{"item": identity}}
			req := &PreviewResolverRequest{Engine: &commonmodels.Engine{ID: 14, EngineType: "nfs"}}
			if err := checkWholeContainerRead(context.Background(), req, source); !errors.Is(err, ErrEngineAccessDenied) {
				t.Fatalf("malformed FileGDB skipped the guard: identity=%v child=%q err=%v", identity, child, err)
			}
		}
	}
}

type wholePreviewSource struct {
	*recordingFileCatalogPreviewPlugin
}

func (*wholePreviewSource) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.FileCatalogModel()
}
