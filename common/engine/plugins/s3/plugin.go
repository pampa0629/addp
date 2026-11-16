package s3

import (
	"context"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/objectstore"
	"io"
)

// S3Plugin Amazon S3 对象存储插件
// 使用 MinIO SDK (S3 兼容)
type S3Plugin struct{}

func init() {
	plugin.Register(&S3Plugin{})
}

func (p *S3Plugin) Type() string {
	return "s3"
}

func (p *S3Plugin) DisplayName() string {
	return "Amazon S3"
}

func (p *S3Plugin) EngineOrigin() string {
	return "general"
}

func (p *S3Plugin) ConnectionSpec() plugin.ConnectionSpec {
	spec := plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "endpoint", LabelKey: "storageEngine.endpoint", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "s3.amazonaws.com", PlaceholderKey: "storageEngine.endpointPlaceholder"},
		plugin.ConnectionFieldSpec{Key: "access_key", LabelKey: "storageEngine.accessKey", Input: plugin.ConnectionFieldPassword, Required: true, Sensitive: true},
		plugin.ConnectionFieldSpec{Key: "secret_key", LabelKey: "storageEngine.secretKey", Input: plugin.ConnectionFieldPassword, Required: true, Sensitive: true},
		plugin.ConnectionFieldSpec{Key: "bucket", LabelKey: "storageEngine.bucket", Input: plugin.ConnectionFieldText, PlaceholderKey: "storageEngine.bucketPlaceholder"},
		plugin.ConnectionFieldSpec{Key: "use_ssl", LabelKey: "storageEngine.useSsl", Input: plugin.ConnectionFieldBoolean, Default: true},
	)
	spec.DefaultPort = 443
	return spec
}

func (p *S3Plugin) DefaultPort() int {
	return p.ConnectionSpec().DefaultPortValue()
}

func (p *S3Plugin) RequiredFields() []string {
	return p.ConnectionSpec().RequiredFields()
}

func (p *S3Plugin) SensitiveFields() []string {
	return p.ConnectionSpec().SensitiveFields()
}

func (p *S3Plugin) ConnectionIdentityFields() []string {
	return p.ConnectionSpec().IdentityFields()
}

func (p *S3Plugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewObjectCapabilities(p.Type())
}

func (p *S3Plugin) StoreSemantics() plugin.StoreSemantics {
	capabilities := p.Capabilities()
	return plugin.StoreSemantics{
		Semantics:    capabilities.Storage.Semantics,
		NotSupported: capabilities.Storage.NotSupported,
	}
}

func (p *S3Plugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return (objectstore.Protocol{DefaultSSL: true}).EngineCatalogModel()
}

func (p *S3Plugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return (objectstore.Protocol{DefaultSSL: true}).ListChildren(ctx, connInfo, parent, opts)
}

func (p *S3Plugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return (objectstore.Protocol{DefaultSSL: true}).ResolvePath(ctx, connInfo, path)
}

func (p *S3Plugin) DescribeEngineCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return (objectstore.Protocol{DefaultSSL: true}).DescribeEngineCatalogFacts(ctx, connInfo, path, opts)
}

func (p *S3Plugin) OpenContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	return (objectstore.Protocol{DefaultSSL: true}).OpenContent(ctx, connInfo, path, opts)
}

func (p *S3Plugin) OpenRange(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	return (objectstore.Protocol{DefaultSSL: true}).OpenRange(ctx, connInfo, path, opts)
}

func (p *S3Plugin) CreateContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.WriteOptions) (io.WriteCloser, error) {
	return (objectstore.Protocol{DefaultSSL: true}).CreateContent(ctx, connInfo, path, opts)
}

func (p *S3Plugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) error {
	return (objectstore.Protocol{DefaultSSL: true}).DeleteResource(ctx, connInfo, path)
}

func (p *S3Plugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	return (objectstore.Protocol{DefaultSSL: true}).TestConnection(ctx, connInfo)
}

func (p *S3Plugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}
