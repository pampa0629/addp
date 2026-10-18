// Package falkordb connects user-owned property graphs, independently of Infra.
package falkordb

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/propertygraph"
	rdb "github.com/redis/go-redis/v9"
)

type FalkorDBPlugin struct{}

func init()                                  { plugin.Register(&FalkorDBPlugin{}) }
func (*FalkorDBPlugin) Type() string         { return "falkordb" }
func (*FalkorDBPlugin) DisplayName() string  { return "FalkorDB" }
func (*FalkorDBPlugin) EngineOrigin() string { return "general" }
func (*FalkorDBPlugin) ConnectionSpec() plugin.ConnectionSpec {
	return plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "host", LabelKey: "storageEngine.host", Input: plugin.ConnectionFieldText, Required: true, Identity: true},
		plugin.ConnectionFieldSpec{Key: "port", LabelKey: "storageEngine.port", Input: plugin.ConnectionFieldNumber, Default: 6379, Identity: true, Min: plugin.Int(1), Max: plugin.Int(65535)},
		plugin.ConnectionFieldSpec{Key: "user", LabelKey: "storageEngine.username", Input: plugin.ConnectionFieldText, Default: "default", Identity: true},
		plugin.ConnectionFieldSpec{Key: "password", LabelKey: "storageEngine.password", Input: plugin.ConnectionFieldPassword, Required: true, Sensitive: true},
		plugin.ConnectionFieldSpec{Key: "database", LabelKey: "storageEngine.databaseIndex", Input: plugin.ConnectionFieldNumber, Default: 0, Identity: true, Min: plugin.Int(0), Max: plugin.Int(2147483647)},
		plugin.ConnectionFieldSpec{Key: "use_ssl", LabelKey: "storageEngine.useSsl", Input: plugin.ConnectionFieldBoolean, Default: false},
		plugin.ConnectionFieldSpec{Key: "tls_ca_cert", LabelKey: "storageEngine.tlsCaCertOptional", Input: plugin.ConnectionFieldTextarea, Rows: 3, PlaceholderKey: "storageEngine.pemPlaceholder"},
	)
}
func (p *FalkorDBPlugin) DefaultPort() int          { return p.ConnectionSpec().DefaultPortValue() }
func (p *FalkorDBPlugin) RequiredFields() []string  { return p.ConnectionSpec().RequiredFields() }
func (p *FalkorDBPlugin) SensitiveFields() []string { return p.ConnectionSpec().SensitiveFields() }
func (p *FalkorDBPlugin) ConnectionIdentityFields() []string {
	return p.ConnectionSpec().IdentityFields()
}
func (p *FalkorDBPlugin) Capabilities() plugin.EngineCapabilities {
	c := plugin.NewGraphCapabilities(p.Type())
	m := p.EngineCatalogModel()
	c.Storage.CatalogModel = &m
	c.Storage.Catalog.NodeKinds = []string{"graph"}
	c.Storage.Semantics = []string{"graph"}
	return c
}
func (*FalkorDBPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.EngineCatalogModelSpec{PathVersion: plugin.EngineCatalogPathVersion, RootTerm: plugin.EngineCatalogTermServer, Levels: []plugin.EngineCatalogLevelSpec{{Term: plugin.EngineCatalogTermGraph, Kinds: []string{plugin.EngineCatalogKindGraph}, Role: plugin.EngineCatalogRoleLeaf, I18nKey: "engine.term.graph"}}}
}
func (p *FalkorDBPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}
func (*FalkorDBPlugin) QueryLanguages() []string { return []string{"cypher"} }

func (p *FalkorDBPlugin) connectionOptions(c plugin.ConnectionInfo) (*rdb.Options, error) {
	host := plugin.GetString(c, "host")
	if host == "" || strings.TrimSpace(host) != host || strings.ContainsAny(host, "/\x00\r\n") {
		return nil, fmt.Errorf("invalid FalkorDB host")
	}
	integer := func(key string, defaultValue, min, max int) (int, error) {
		v, ok := c[key]
		if !ok {
			return defaultValue, nil
		}
		var number float64
		switch value := v.(type) {
		case int:
			number = float64(value)
		case int64:
			number = float64(value)
		case float64:
			number = value
		default:
			return 0, fmt.Errorf("invalid FalkorDB %s", key)
		}
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < float64(min) || number > float64(max) {
			return 0, fmt.Errorf("invalid FalkorDB %s", key)
		}
		n := int(number)
		return n, nil
	}
	port, err := integer("port", 6379, 1, 65535)
	if err != nil {
		return nil, err
	}
	db, err := integer("database", 0, 0, 2147483647)
	if err != nil {
		return nil, err
	}
	password := plugin.GetString(c, "password")
	if password == "" {
		return nil, fmt.Errorf("FalkorDB password is required")
	}
	user := "default"
	if v, ok := c["user"]; ok {
		var valid bool
		user, valid = v.(string)
		if !valid || user == "" || strings.TrimSpace(user) != user {
			return nil, fmt.Errorf("invalid FalkorDB user")
		}
	}
	o := &rdb.Options{Addr: net.JoinHostPort(plugin.NormalizeHost(host), strconv.Itoa(port)), Username: user, Password: password, DB: db, Protocol: 2, DisableIdentity: true, MaxRetries: -1, PoolSize: 1, MaxActiveConns: 1, DialTimeout: 3 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 3 * time.Second, ContextTimeoutEnabled: true}
	if v, ok := c["use_ssl"]; ok {
		if _, ok := v.(bool); !ok {
			return nil, fmt.Errorf("invalid FalkorDB TLS flag")
		}
	}
	if plugin.GetBool(c, "use_ssl") {
		o.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	}
	if v, ok := c["tls_ca_cert"]; ok {
		ca, valid := v.(string)
		if !valid {
			return nil, fmt.Errorf("invalid FalkorDB CA")
		}
		if ca != "" {
			if o.TLSConfig == nil {
				return nil, fmt.Errorf("FalkorDB CA requires TLS")
			}
			roots, err := x509.SystemCertPool()
			if err != nil {
				return nil, err
			}
			if !roots.AppendCertsFromPEM([]byte(ca)) {
				return nil, fmt.Errorf("invalid FalkorDB CA")
			}
			o.TLSConfig.RootCAs = roots
		}
	}
	return o, nil
}
func (p *FalkorDBPlugin) ValidateConnectionInfo(c plugin.ConnectionInfo) error {
	_, err := p.connectionOptions(c)
	return err
}
func (p *FalkorDBPlugin) open(ctx context.Context, c plugin.ConnectionInfo) (*rdb.Client, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	o, err := p.connectionOptions(c)
	if err != nil {
		return nil, nil, err
	}
	client := rdb.NewClient(o)
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	close := func() { stop(); _ = client.Close() }
	hello, err := client.Info(ctx, "server").Result()
	if err != nil {
		close()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, err
	}
	if !strings.Contains(hello, "redis_mode:standalone\r\n") {
		close()
		return nil, nil, fmt.Errorf("FalkorDB requires a standalone endpoint")
	}
	return client, close, nil
}
func (p *FalkorDBPlugin) ProbeConnection(ctx context.Context, c plugin.ConnectionInfo) (plugin.ConnectionProbeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client, close, err := p.open(ctx, c)
	if err != nil {
		return plugin.ConnectionProbeResult{}, err
	}
	defer close()
	version, err := moduleVersion(ctx, client)
	if err == nil && version != 42006 {
		err = errLocalRead
	}
	if err != nil {
		return plugin.ConnectionProbeResult{}, err
	}
	if _, err = client.Do(ctx, "GRAPH.LIST").StringSlice(); err != nil {
		return plugin.ConnectionProbeResult{}, err
	}
	return plugin.ConnectionProbeResult{ServerVersion: fmt.Sprintf("%d.%d.%d", version/10000, version/100%100, version%100)}, nil
}
func (p *FalkorDBPlugin) TestConnection(ctx context.Context, c plugin.ConnectionInfo) error {
	_, err := p.ProbeConnection(ctx, c)
	return err
}
func moduleVersion(ctx context.Context, c *rdb.Client) (int64, error) {
	modules, err := c.Do(ctx, "MODULE", "LIST").Slice()
	if err != nil {
		return 0, err
	}
	for _, raw := range modules {
		fields, ok := raw.([]interface{})
		if !ok {
			return 0, fmt.Errorf("invalid module metadata")
		}
		var name string
		var version int64
		for i := 0; i+1 < len(fields); i += 2 {
			switch fields[i] {
			case "name":
				name, _ = fields[i+1].(string)
			case "ver":
				version, _ = fields[i+1].(int64)
			}
		}
		if name == "graph" {
			return version, nil
		}
	}
	return 0, fmt.Errorf("endpoint has no FalkorDB graph module")
}
func (p *FalkorDBPlugin) graphPath(engineID uint, name string) plugin.EngineCatalogPath {
	path := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), engineID)
	path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: "graph", Kind: "graph", Name: name})
	return path
}
func (p *FalkorDBPlugin) graphName(path plugin.EngineCatalogPath) (string, error) {
	if err := propertygraph.ValidateTarget(path, p.EngineCatalogModel()); err != nil {
		return "", plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, err)
	}
	if !validGraphName(path.Segments[1].Name) {
		return "", plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("FalkorDB requires a graph leaf path"))
	}
	return path.Segments[1].Name, nil
}
func (p *FalkorDBPlugin) ListChildren(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	if !plugin.IsEngineCatalogRootPath(path) {
		if _, err := p.ResolvePath(ctx, c, path); err != nil {
			return nil, err
		}
		return []plugin.EngineCatalogEntry{}, nil
	}
	if path.Version != plugin.EngineCatalogPathVersion || path.EngineID == 0 || path.Segments[0] != (plugin.EngineCatalogSegment{Term: "server", Kind: "server"}) {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("invalid FalkorDB root"))
	}
	client, close, err := p.open(ctx, c)
	if err != nil {
		return nil, err
	}
	defer close()
	names, err := client.Do(ctx, "GRAPH.LIST").StringSlice()
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	entries := make([]plugin.EngineCatalogEntry, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		// GRAPH.LIST uses a global registry, including graphs in other Redis
		// databases. TYPE is scoped to this connection's fixed database.
		kind, err := client.Type(ctx, name).Result()
		if err != nil {
			return nil, err
		}
		if kind != "graphdata" {
			continue
		}
		if !validGraphName(name) {
			return nil, fmt.Errorf("FalkorDB graph key cannot be represented as a catalog name")
		}
		entries = append(entries, plugin.EngineCatalogEntry{Name: name, Term: "graph", Kind: "graph", Role: plugin.EngineCatalogRoleLeaf, Path: p.graphPath(path.EngineID, name)})
	}
	return entries, nil
}
func (p *FalkorDBPlugin) ResolvePath(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	if plugin.IsEngineCatalogRootPath(path) {
		if path.Version != plugin.EngineCatalogPathVersion || path.Segments[0] != (plugin.EngineCatalogSegment{Term: "server", Kind: "server"}) || path.EngineID == 0 {
			return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("invalid FalkorDB root"))
		}
		return &plugin.EngineCatalogEntry{Path: path, Term: "server", Kind: "server", Role: plugin.EngineCatalogRoleBranch}, nil
	}
	name, err := p.graphName(path)
	if err != nil {
		return nil, err
	}
	client, close, err := p.open(ctx, c)
	if err != nil {
		return nil, err
	}
	defer close()
	if err = graphExists(ctx, client, name); err != nil {
		return nil, err
	}
	return &plugin.EngineCatalogEntry{Name: name, Path: path, Term: "graph", Kind: "graph", Role: plugin.EngineCatalogRoleLeaf}, nil
}
func graphExists(ctx context.Context, c *rdb.Client, name string) error {
	kind, err := c.Type(ctx, name).Result()
	if err != nil {
		return err
	}
	if kind == "none" {
		return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorNotFound, fmt.Errorf("FalkorDB graph does not exist"))
	}
	if kind != "graphdata" {
		return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("FalkorDB target is not a graph"))
	}
	return nil
}

func validGraphName(name string) bool {
	return name != "" && name == strings.TrimSpace(name) && len(name) <= 1024 && utf8.ValidString(name) && !strings.ContainsRune(name, 0)
}
