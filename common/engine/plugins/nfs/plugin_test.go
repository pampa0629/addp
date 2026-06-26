package nfs

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/addp/common/engine/plugin"
)

type testReadSeekCloser struct {
	*strings.Reader
}

func (r testReadSeekCloser) Close() error {
	return nil
}

func TestNFSRootCatalogEntryIsSemanticRoot(t *testing.T) {
	p := &NFSPlugin{}

	root := plugin.CatalogRootEntry(p.CatalogModel(), 7, "Business NFS")
	if root.Name != "Business NFS" {
		t.Fatalf("expected root name from engine, got %q", root.Name)
	}
	if got := root.Path.StringPath(); got != "" {
		t.Fatalf("expected root string path '', got %q", got)
	}
	if root.Term != plugin.CatalogTermRoot || root.Kind != plugin.CatalogKindRoot {
		t.Fatalf("expected root term/kind, got %s/%s", root.Term, root.Kind)
	}
	if got := p.CatalogModel().RootTerm; got != plugin.CatalogTermRoot {
		t.Fatalf("expected file catalog root term %q, got %q", plugin.CatalogTermRoot, got)
	}
}

func TestFileCatalogNormalizesDotRoot(t *testing.T) {
	callbacks := plugin.FileCatalogCallbacks{
		ListDirectoryFunc: func(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath) ([]plugin.CatalogEntry, error) {
			if path := parent.StringPath(); path != "" {
				t.Fatalf("expected semantic root list path '', got %q", path)
			}
			return []plugin.CatalogEntry{
				plugin.FileDirectoryCatalogEntry(parent, "shp", "./shp/"),
			}, nil
		},
	}

	children, err := plugin.ListFileCatalogChildren(context.Background(), callbacks, nil, 7, plugin.FileRootPath(7), plugin.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 {
		t.Fatalf("expected one child, got %d", len(children))
	}
	if got := children[0].Path.StringPath(); got != "shp" {
		t.Fatalf("expected child path 'shp', got %q", got)
	}
	if children[0].Storage == nil || children[0].Storage.Path != "shp" {
		t.Fatalf("expected child storage path 'shp', got %#v", children[0].Storage)
	}
}

func TestNFSCapabilitiesDeclareRangeRead(t *testing.T) {
	p := &NFSPlugin{}

	if _, ok := interface{}(p).(plugin.RangeReadableProvider); !ok {
		t.Fatal("NFSPlugin should implement RangeReadableProvider")
	}
	caps := p.Capabilities()
	if caps.Storage == nil || caps.Storage.Store == nil || !caps.Storage.Store.RangeRead {
		t.Fatalf("NFS range_read capability = %#v, want true", caps.Storage)
	}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatalf("ValidatePluginCapabilities() error = %v", err)
	}
}

func TestNFSFileReadCloserSupportsSeekEndWithKnownSize(t *testing.T) {
	reader := &nfsFileReadCloser{
		ReadCloser: testReadSeekCloser{Reader: strings.NewReader("abcdef")},
		mu:         &sync.Mutex{},
		size:       6,
	}
	defer reader.Close()

	pos, err := reader.Seek(-2, io.SeekEnd)
	if err != nil {
		t.Fatalf("SeekEnd() error = %v", err)
	}
	if pos != 4 {
		t.Fatalf("SeekEnd position = %d, want 4", pos)
	}
	buf := make([]byte, 2)
	n, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 2 || string(buf) != "ef" {
		t.Fatalf("Read after SeekEnd = n:%d data:%q, want ef", n, string(buf))
	}
}
