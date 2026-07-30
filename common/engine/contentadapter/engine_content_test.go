package contentadapter

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/addp/common/contentio"
	engineplugin "github.com/addp/common/engine/plugin"
)

func TestEngineContentReaderUsesRefBasename(t *testing.T) {
	provider := &contentProviderStub{data: map[string]string{"bucket/result.dbf": "ok"}}
	reader := NewReader(provider, nil, baseCatalogPath(), engineplugin.ReadOptions{})

	rc, err := reader.Open(context.Background(), contentio.NewRef("elsewhere/result.dbf", contentio.RoleAuxiliary))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "ok" {
		t.Fatalf("Open() data = %q, want ok", data)
	}
}

func TestEngineContentReaderOpenRange(t *testing.T) {
	provider := &contentProviderStub{data: map[string]string{"bucket/result.shx": "0123456789"}}
	reader := NewReader(provider, nil, baseCatalogPath(), engineplugin.ReadOptions{})
	rangeReader, ok := reader.(contentio.RangeReader)
	if !ok {
		t.Fatalf("reader does not implement RangeReader")
	}

	rc, err := rangeReader.OpenRange(context.Background(), contentio.NewRef("result.shx", "index"), 3, 4)
	if err != nil {
		t.Fatalf("OpenRange() error = %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "3456" {
		t.Fatalf("OpenRange() data = %q, want 3456", data)
	}
}

func TestEngineContentReaderStatUsesCatalog(t *testing.T) {
	modifiedAt := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	provider := &contentProviderStub{
		data:       map[string]string{"bucket/result.parquet": "PAR1data"},
		modifiedAt: modifiedAt,
	}
	reader := NewReader(provider, nil, baseCatalogPath(), engineplugin.ReadOptions{})

	stat, err := reader.Stat(context.Background(), contentio.NewRef("elsewhere/result.parquet", contentio.RoleMain))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !stat.Exists || stat.Size != int64(len("PAR1data")) || stat.ContentType != "application/octet-stream" {
		t.Fatalf("stat = %#v, want existing size/content type", stat)
	}
	if stat.ModifiedAt == nil || !stat.ModifiedAt.Equal(modifiedAt) {
		t.Fatalf("modified_at = %v, want %v", stat.ModifiedAt, modifiedAt)
	}
}

func TestEngineContentWriterUsesRefBasename(t *testing.T) {
	provider := &contentProviderStub{data: map[string]string{}}
	writer := NewWriter(provider, nil, baseCatalogPath(), engineplugin.WriteOptions{Overwrite: true})

	wc, err := writer.Create(context.Background(), contentio.NewRef("result.dbf", "attributes"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := wc.Write([]byte("new")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := provider.data["bucket/result.dbf"]; got != "new" {
		t.Fatalf("written data = %q, want new", got)
	}
}

func TestMappedReaderUsesObjectRefPath(t *testing.T) {
	provider := &contentProviderStub{data: map[string]string{"graph/build/input.txt": "ok"}}
	reader := NewMappedReader(provider, nil, ObjectPathMapper(42), engineplugin.ReadOptions{})

	rc, err := reader.Open(context.Background(), contentio.NewRef("graph/build/input.txt", contentio.RoleMain))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "ok" {
		t.Fatalf("Open() data = %q, want ok", data)
	}
}

func TestMappedWriterUsesObjectRefPath(t *testing.T) {
	provider := &contentProviderStub{data: map[string]string{}}
	writer := NewMappedWriter(provider, nil, ObjectPathMapper(42), engineplugin.WriteOptions{Overwrite: true})

	wc, err := writer.Create(context.Background(), contentio.NewRef("graph/build/output.txt", contentio.RoleMain))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := wc.Write([]byte("new")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := provider.data["graph/build/output.txt"]; got != "new" {
		t.Fatalf("written data = %q, want new", got)
	}
}

func TestMappedReaderUsesFixedCatalogPath(t *testing.T) {
	provider := &contentProviderStub{data: map[string]string{"bucket/source.shp": "ok"}}
	reader := NewMappedReader(provider, nil, FixedPathMapper(baseCatalogPath()), engineplugin.ReadOptions{})

	rc, err := reader.Open(context.Background(), contentio.NewRef("ignored.csv", contentio.RoleMain))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "ok" {
		t.Fatalf("Open() data = %q, want ok", data)
	}
}

func TestFixedPathMapperReturnsIndependentCatalogPath(t *testing.T) {
	mapRef := FixedPathMapper(baseCatalogPath())
	first, err := mapRef(contentio.NewRef("ignored.csv", contentio.RoleMain))
	if err != nil {
		t.Fatalf("first map error = %v", err)
	}
	first.Segments[1].Name = "changed.csv"

	second, err := mapRef(contentio.NewRef("ignored.csv", contentio.RoleMain))
	if err != nil {
		t.Fatalf("second map error = %v", err)
	}
	if got := second.StringPath(); got != "bucket/source.shp" {
		t.Fatalf("second path = %q, want bucket/source.shp", got)
	}
}

type contentProviderStub struct {
	data       map[string]string
	modifiedAt time.Time
}

func (p *contentProviderStub) OpenContent(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath, _ engineplugin.ReadOptions) (io.ReadCloser, error) {
	value := p.data[path.StringPath()]
	return io.NopCloser(bytes.NewBufferString(value)), nil
}

func (p *contentProviderStub) Type() string { return "content-test" }

func (p *contentProviderStub) DisplayName() string { return "Content Test" }

func (p *contentProviderStub) EngineOrigin() string { return "extension" }

func (p *contentProviderStub) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}

func (p *contentProviderStub) ValidateConnectionInfo(engineplugin.ConnectionInfo) error { return nil }

func (p *contentProviderStub) DefaultPort() int { return 0 }

func (p *contentProviderStub) RequiredFields() []string { return nil }

func (p *contentProviderStub) SensitiveFields() []string          { return nil }
func (p *contentProviderStub) ConnectionIdentityFields() []string { return []string{"host"} }

func (p *contentProviderStub) Capabilities() engineplugin.EngineCapabilities {
	return engineplugin.EngineCapabilities{}
}

func (p *contentProviderStub) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}

func (p *contentProviderStub) OpenRange(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath, opts engineplugin.ReadOptions) (io.ReadCloser, error) {
	value := p.data[path.StringPath()]
	end := opts.Offset + opts.Length
	if end > int64(len(value)) {
		end = int64(len(value))
	}
	return io.NopCloser(bytes.NewBufferString(value[opts.Offset:end])), nil
}

func (p *contentProviderStub) ListChildren(context.Context, engineplugin.ConnectionInfo, engineplugin.CatalogPath, engineplugin.ListOptions) ([]engineplugin.CatalogEntry, error) {
	return nil, nil
}

func (p *contentProviderStub) ResolvePath(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath) (*engineplugin.CatalogEntry, error) {
	value, ok := p.data[path.StringPath()]
	if !ok {
		return nil, contentio.ErrContentNotFound
	}
	var updatedAt *time.Time
	if !p.modifiedAt.IsZero() {
		modifiedAt := p.modifiedAt
		updatedAt = &modifiedAt
	}
	sizeBytes := int64(len(value))
	return &engineplugin.CatalogEntry{
		Name: path.StringPath(),
		Path: path,
		Role: engineplugin.CatalogRoleLeaf,
		Storage: &engineplugin.CatalogStorageFacts{
			ContentType: "application/octet-stream",
			SizeBytes:   &sizeBytes,
		},
		UpdatedAt: updatedAt,
	}, nil
}

func (p *contentProviderStub) CreateContent(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath, _ engineplugin.WriteOptions) (io.WriteCloser, error) {
	return &captureWriter{
		close: func(data string) {
			p.data[path.StringPath()] = data
		},
	}, nil
}

type captureWriter struct {
	bytes.Buffer
	close func(string)
}

func (w *captureWriter) Close() error {
	w.close(w.String())
	return nil
}

func baseCatalogPath() engineplugin.CatalogPath {
	return engineplugin.CatalogPath{
		Version: engineplugin.CatalogPathVersion,
		Segments: []engineplugin.CatalogSegment{
			{Name: "bucket", Term: engineplugin.CatalogTermBucket, Kind: engineplugin.CatalogKindBucket},
			{Name: "source.shp", Term: engineplugin.CatalogTermFile, Kind: engineplugin.CatalogKindFile},
		},
	}
}
