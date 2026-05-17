package executor

import (
	"context"
	"io"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/resource"
)

func TestContentComponentWriterReplacesFileLeaf(t *testing.T) {
	provider := &componentWriterTestProvider{}
	target := engineplugin.FileItemPath(7, "exports/roads.shp")
	writer := newContentComponentWriter(provider, nil, target, engineplugin.WriteOptions{Overwrite: true}, []resource.ComponentSpec{
		{Extension: ".shp", Role: "main", Required: true},
		{Extension: ".dbf", Role: "attributes", Required: true},
	})

	components := writer.Components()
	if len(components) != 2 {
		t.Fatalf("component count = %d, want 2", len(components))
	}
	if _, err := writer.CreateComponent(context.Background(), components[1]); err != nil {
		t.Fatalf("CreateComponent failed: %v", err)
	}
	if got, want := provider.paths[0].StringPath(), "exports/roads.dbf"; got != want {
		t.Fatalf("component path = %q, want %q", got, want)
	}
	if !provider.opts[0].Overwrite {
		t.Fatal("overwrite option was not forwarded")
	}
}

func TestContentComponentWriterReplacesObjectLeaf(t *testing.T) {
	provider := &componentWriterTestProvider{}
	target := engineplugin.ObjectItemPath(7, "bucket", "exports/roads.shp")
	writer := newContentComponentWriter(provider, nil, target, engineplugin.WriteOptions{}, []resource.ComponentSpec{
		{Extension: ".shx", Role: "index", Required: true},
	})

	components := writer.Components()
	if _, err := writer.CreateComponent(context.Background(), components[0]); err != nil {
		t.Fatalf("CreateComponent failed: %v", err)
	}
	if got, want := provider.paths[0].StringPath(), "bucket/exports/roads.shx"; got != want {
		t.Fatalf("component object path = %q, want %q", got, want)
	}
}

type componentWriterTestProvider struct {
	paths []engineplugin.CatalogPath
	opts  []engineplugin.WriteOptions
}

func (p *componentWriterTestProvider) Type() string { return "component_writer_test" }

func (p *componentWriterTestProvider) DisplayName() string { return "Component Writer Test" }

func (p *componentWriterTestProvider) EngineOrigin() string { return "general" }

func (p *componentWriterTestProvider) DefaultPort() int { return 0 }

func (p *componentWriterTestProvider) RequiredFields() []string { return nil }

func (p *componentWriterTestProvider) SensitiveFields() []string { return nil }

func (p *componentWriterTestProvider) ValidateConnectionInfo(engineplugin.ConnectionInfo) error {
	return nil
}

func (p *componentWriterTestProvider) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}

func (p *componentWriterTestProvider) Capabilities() engineplugin.EngineCapabilities {
	return engineplugin.EngineCapabilities{}
}

func (p *componentWriterTestProvider) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}

func (p *componentWriterTestProvider) CreateContent(ctx context.Context, connInfo engineplugin.ConnectionInfo, path engineplugin.CatalogPath, opts engineplugin.WriteOptions) (io.WriteCloser, error) {
	p.paths = append(p.paths, path)
	p.opts = append(p.opts, opts)
	return nopWriteCloser{Writer: io.Discard}, nil
}

type nopWriteCloser struct {
	io.Writer
}

func (w nopWriteCloser) Close() error {
	return nil
}
