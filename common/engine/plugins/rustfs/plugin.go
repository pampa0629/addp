package rustfs

import (
	"context"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/objectstore"
	"io"
)

// RustFSPlugin RustFS 对象存储插件
type RustFSPlugin struct{}

func init() { plugin.Register(&RustFSPlugin{}) }

func (p *RustFSPlugin) Type() string {
	return "rustfs"
}

func (p *RustFSPlugin) DisplayName() string {
	return "RustFS"
}

func (p *RustFSPlugin) EngineOrigin() string {
	return "general"
}

func (p *RustFSPlugin) ConnectionSpec() plugin.ConnectionSpec {
	spec := plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "endpoint", LabelKey: "storageEngine.endpoint", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "localhost:9004", PlaceholderKey: "storageEngine.endpointPlaceholder"},
		plugin.ConnectionFieldSpec{Key: "access_key", LabelKey: "storageEngine.accessKey", Input: plugin.ConnectionFieldPassword, Required: true, Sensitive: true},
		plugin.ConnectionFieldSpec{Key: "secret_key", LabelKey: "storageEngine.secretKey", Input: plugin.ConnectionFieldPassword, Required: true, Sensitive: true},
		plugin.ConnectionFieldSpec{Key: "bucket", LabelKey: "storageEngine.bucket", Input: plugin.ConnectionFieldText, PlaceholderKey: "storageEngine.bucketPlaceholder"},
		plugin.ConnectionFieldSpec{Key: "use_ssl", LabelKey: "storageEngine.useSsl", Input: plugin.ConnectionFieldBoolean, Default: false},
	)
	spec.DefaultPort = 9000
	return spec
}

func (p *RustFSPlugin) DefaultPort() int {
	return p.ConnectionSpec().DefaultPortValue()
}

func (p *RustFSPlugin) RequiredFields() []string {
	return p.ConnectionSpec().RequiredFields()
}

func (p *RustFSPlugin) SensitiveFields() []string {
	return p.ConnectionSpec().SensitiveFields()
}

func (p *RustFSPlugin) ConnectionIdentityFields() []string {
	return p.ConnectionSpec().IdentityFields()
}

func (p *RustFSPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewObjectCapabilities(p.Type())
}

func (p *RustFSPlugin) StoreSemantics() plugin.StoreSemantics {
	capabilities := p.Capabilities()
	return plugin.StoreSemantics{
		Semantics:    capabilities.Storage.Semantics,
		NotSupported: capabilities.Storage.NotSupported,
	}
}

func (p *RustFSPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return (objectstore.Protocol{DefaultSSL: false}).EngineCatalogModel()
}

func (p *RustFSPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return (objectstore.Protocol{DefaultSSL: false}).ListChildren(ctx, connInfo, parent, opts)
}

func (p *RustFSPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return (objectstore.Protocol{DefaultSSL: false}).ResolvePath(ctx, connInfo, path)
}

func (p *RustFSPlugin) DescribeEngineCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return (objectstore.Protocol{DefaultSSL: false}).DescribeEngineCatalogFacts(ctx, connInfo, path, opts)
}

func (p *RustFSPlugin) OpenContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	return (objectstore.Protocol{DefaultSSL: false}).OpenContent(ctx, connInfo, path, opts)
}

func (p *RustFSPlugin) OpenRange(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	return (objectstore.Protocol{DefaultSSL: false}).OpenRange(ctx, connInfo, path, opts)
}

func (p *RustFSPlugin) CreateContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.WriteOptions) (io.WriteCloser, error) {
	return (objectstore.Protocol{DefaultSSL: false}).CreateContent(ctx, connInfo, path, opts)
}

func (p *RustFSPlugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) error {
	return (objectstore.Protocol{DefaultSSL: false}).DeleteResource(ctx, connInfo, path)
}

func (p *RustFSPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	return (objectstore.Protocol{DefaultSSL: false}).TestConnection(ctx, connInfo)
}

func (p *RustFSPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}
