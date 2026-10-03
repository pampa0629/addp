// Package elasticsearch provides exact-index catalog and read-only document access.
package elasticsearch

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	es "github.com/elastic/go-elasticsearch/v9"
)

type ElasticsearchPlugin struct{}

type esClient struct {
	*es.Client
	transport *http.Transport
}

func init()                                       { plugin.Register(&ElasticsearchPlugin{}) }
func (*ElasticsearchPlugin) Type() string         { return "elasticsearch" }
func (*ElasticsearchPlugin) DisplayName() string  { return "Elasticsearch" }
func (*ElasticsearchPlugin) EngineOrigin() string { return "general" }
func (*ElasticsearchPlugin) ConnectionSpec() plugin.ConnectionSpec {
	return plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "endpoint", LabelKey: "storageEngine.endpoint", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Placeholder: "https://localhost:9200"},
		plugin.ConnectionFieldSpec{Key: "user", LabelKey: "storageEngine.username", Input: plugin.ConnectionFieldText, Required: true, Identity: true},
		plugin.ConnectionFieldSpec{Key: "password", LabelKey: "storageEngine.password", Input: plugin.ConnectionFieldPassword, Required: true, Sensitive: true},
		plugin.ConnectionFieldSpec{Key: "tls_ca_cert", LabelKey: "storageEngine.tlsCaCertOptional", Input: plugin.ConnectionFieldTextarea, Rows: 3, PlaceholderKey: "storageEngine.pemPlaceholder"},
	)
}
func (p *ElasticsearchPlugin) DefaultPort() int          { return p.ConnectionSpec().DefaultPortValue() }
func (p *ElasticsearchPlugin) RequiredFields() []string  { return p.ConnectionSpec().RequiredFields() }
func (p *ElasticsearchPlugin) SensitiveFields() []string { return p.ConnectionSpec().SensitiveFields() }
func (p *ElasticsearchPlugin) ConnectionIdentityFields() []string {
	return p.ConnectionSpec().IdentityFields()
}
func (p *ElasticsearchPlugin) ValidateConnectionInfo(c plugin.ConnectionInfo) error {
	if err := plugin.ValidateRequiredFields(c, p.RequiredFields()); err != nil {
		return err
	}
	u, err := url.Parse(plugin.GetString(c, "endpoint"))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("Elasticsearch endpoint requires an HTTP(S) origin without credentials, path or query")
	}
	if ca := plugin.GetString(c, "tls_ca_cert"); ca != "" {
		if u.Scheme != "https" {
			return fmt.Errorf("CA certificate requires HTTPS")
		}
		if !x509.NewCertPool().AppendCertsFromPEM([]byte(ca)) {
			return fmt.Errorf("invalid CA certificate")
		}
	}
	return nil
}
func (p *ElasticsearchPlugin) client(c plugin.ConnectionInfo) (*esClient, error) {
	if err := p.ValidateConnectionInfo(c); err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	if ca := plugin.GetString(c, "tls_ca_cert"); ca != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		roots.AppendCertsFromPEM([]byte(ca))
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	// Redirects are never followed; the SDK transport sends credentials only to this origin.
	client, err := es.NewClient(es.Config{Addresses: []string{strings.TrimRight(plugin.GetString(c, "endpoint"), "/")}, Username: plugin.GetString(c, "user"), Password: plugin.GetString(c, "password"), Transport: transport, DisableRetry: true})
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	return &esClient{Client: client, transport: transport}, nil
}
func (p *ElasticsearchPlugin) TestConnection(ctx context.Context, c plugin.ConnectionInfo) error {
	client, err := p.client(c)
	if err != nil {
		return err
	}
	defer closeClient(client)
	_, err = resolveIndices(ctx, client, "*")
	return err
}
func (*ElasticsearchPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.EngineCatalogModelSpec{PathVersion: plugin.EngineCatalogPathVersion, RootTerm: plugin.EngineCatalogTermService, Levels: []plugin.EngineCatalogLevelSpec{{Term: "index", Kinds: []string{"index"}, Role: plugin.EngineCatalogRoleLeaf, I18nKey: plugin.EngineCatalogTermI18nKey("index")}}}
}
func (p *ElasticsearchPlugin) Capabilities() plugin.EngineCapabilities {
	model := p.EngineCatalogModel()
	return plugin.EngineCapabilities{SchemaVersion: plugin.CapabilitiesSchemaVersion, EngineType: p.Type(), EngineFamily: "dynamic_schema", Storage: &plugin.StorageCapabilities{CatalogModel: &model, Catalog: &plugin.EngineCatalogCapability{Supported: true, RealTime: true, SystemFiltering: true, NodeKinds: []string{"index"}}, Facts: &plugin.EngineCatalogFactsCapability{Supported: true, FieldInfo: true, Statistics: true, NativeFacts: true}, Store: &plugin.StoreCapability{RecordReadSession: true}, Semantics: []string{"record_read_session"}}, Compute: &plugin.ComputeCapabilities{Query: &plugin.QueryCapability{Supported: true, Languages: []string{"es_dsl"}, DefaultLanguage: "es_dsl", ResultKinds: []string{"table"}, ReadOnly: true, SupportsCancel: true}}}
}
func (p *ElasticsearchPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}
