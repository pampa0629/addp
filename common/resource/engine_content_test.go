package resource

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
)

type fakeEngineContentProvider struct {
	files map[string][]byte
}

func (p *fakeEngineContentProvider) Type() string { return "fake_content" }

func (p *fakeEngineContentProvider) DisplayName() string { return "Fake Content" }

func (p *fakeEngineContentProvider) EngineOrigin() string { return "general" }

func (p *fakeEngineContentProvider) DefaultPort() int { return 0 }

func (p *fakeEngineContentProvider) RequiredFields() []string { return nil }

func (p *fakeEngineContentProvider) SensitiveFields() []string { return nil }

func (p *fakeEngineContentProvider) ValidateConnectionInfo(engineplugin.ConnectionInfo) error {
	return nil
}

func (p *fakeEngineContentProvider) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}

func (p *fakeEngineContentProvider) Capabilities() engineplugin.EngineCapabilities {
	return engineplugin.EngineCapabilities{}
}

func (p *fakeEngineContentProvider) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}

func (p *fakeEngineContentProvider) OpenContent(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath, _ engineplugin.ReadOptions) (io.ReadCloser, error) {
	data, ok := p.files[path.StringPath()]
	if !ok {
		return nil, fmt.Errorf("content %s not found", path.StringPath())
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (p *fakeEngineContentProvider) OpenRange(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath, opts engineplugin.ReadOptions) (io.ReadCloser, error) {
	data, ok := p.files[path.StringPath()]
	if !ok {
		return nil, fmt.Errorf("content %s not found", path.StringPath())
	}
	end := opts.Offset + opts.Length
	if opts.Offset < 0 || opts.Length < 0 || opts.Offset > int64(len(data)) {
		return nil, ErrResourceNotFound
	}
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	return io.NopCloser(bytes.NewReader(data[opts.Offset:end])), nil
}

func (p *fakeEngineContentProvider) CreateContent(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath, _ engineplugin.WriteOptions) (io.WriteCloser, error) {
	buf := &bytes.Buffer{}
	return writeCloserFunc{
		Writer: buf,
		close: func() error {
			p.files[path.StringPath()] = append([]byte(nil), buf.Bytes()...)
			return nil
		},
	}, nil
}

type writeCloserFunc struct {
	io.Writer
	close func() error
}

func (w writeCloserFunc) Close() error {
	if w.close == nil {
		return nil
	}
	return w.close()
}

func TestEngineContentReaderOpensResourceWithSameParentPath(t *testing.T) {
	base := engineplugin.FileItemPath(10, "exports/result.csv")
	provider := &fakeEngineContentProvider{files: map[string][]byte{
		"exports/result.dbf": []byte("attributes"),
	}}
	reader := NewEngineContentReader(provider, nil, base, engineplugin.ReadOptions{})

	rc, err := reader.Open(context.Background(), NewResourceRef("elsewhere/result.dbf", ResourceRoleComponent))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got, want := string(data), "attributes"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEngineContentReaderOpensRange(t *testing.T) {
	base := engineplugin.FileItemPath(10, "exports/result.shx")
	provider := &fakeEngineContentProvider{files: map[string][]byte{
		"exports/result.shx": []byte("0123456789"),
	}}
	reader := NewEngineContentReader(provider, nil, base, engineplugin.ReadOptions{})

	rangeReader, ok := reader.(RangeReader)
	if !ok {
		t.Fatal("NewEngineContentReader() does not implement RangeReader")
	}
	rc, err := rangeReader.OpenRange(context.Background(), NewResourceRef("result.shx", ResourceRoleComponent), 3, 4)
	if err != nil {
		t.Fatalf("OpenRange() error = %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got, want := string(data), "3456"; got != want {
		t.Fatalf("range content = %q, want %q", got, want)
	}
}

func TestEngineContentWriterCreatesResourceWithSameParentPath(t *testing.T) {
	base := engineplugin.ObjectItemPath(9, "bucket", "exports/result.shp")
	provider := &fakeEngineContentProvider{files: map[string][]byte{}}
	writer := NewEngineContentWriter(provider, nil, base, engineplugin.WriteOptions{Overwrite: true})

	wc, err := writer.Create(context.Background(), NewResourceRef("result.dbf", ResourceRoleComponent))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := wc.Write([]byte("attributes")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got, want := string(provider.files["bucket/exports/result.dbf"]), "attributes"; got != want {
		t.Fatalf("created content = %q, want %q", got, want)
	}
}
