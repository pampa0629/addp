package contentadapter

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/addp/common/contentio"
	engineplugin "github.com/addp/common/engine/plugin"
)

func TestEngineContentReaderUsesRefBasename(t *testing.T) {
	provider := &contentProviderStub{data: map[string]string{"result.dbf": "ok"}}
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
	provider := &contentProviderStub{data: map[string]string{"result.shx": "0123456789"}}
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
	if got := provider.data["result.dbf"]; got != "new" {
		t.Fatalf("written data = %q, want new", got)
	}
}

type contentProviderStub struct {
	data map[string]string
}

func (p *contentProviderStub) OpenContent(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath, _ engineplugin.ReadOptions) (io.ReadCloser, error) {
	value := p.data[path.Segments[len(path.Segments)-1].Name]
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

func (p *contentProviderStub) SensitiveFields() []string { return nil }

func (p *contentProviderStub) Capabilities() engineplugin.EngineCapabilities {
	return engineplugin.EngineCapabilities{}
}

func (p *contentProviderStub) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}

func (p *contentProviderStub) OpenRange(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath, opts engineplugin.ReadOptions) (io.ReadCloser, error) {
	value := p.data[path.Segments[len(path.Segments)-1].Name]
	end := opts.Offset + opts.Length
	if end > int64(len(value)) {
		end = int64(len(value))
	}
	return io.NopCloser(bytes.NewBufferString(value[opts.Offset:end])), nil
}

func (p *contentProviderStub) CreateContent(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath, _ engineplugin.WriteOptions) (io.WriteCloser, error) {
	return &captureWriter{
		close: func(data string) {
			p.data[path.Segments[len(path.Segments)-1].Name] = data
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
