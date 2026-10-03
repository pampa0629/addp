// Package redis provides bounded standalone Redis catalog and native value access.
package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	rdb "github.com/redis/go-redis/v9"
)

type RedisPlugin struct{}

func init()                               { plugin.Register(&RedisPlugin{}) }
func (*RedisPlugin) Type() string         { return "redis" }
func (*RedisPlugin) DisplayName() string  { return "Redis" }
func (*RedisPlugin) EngineOrigin() string { return "general" }
func (*RedisPlugin) ConnectionSpec() plugin.ConnectionSpec {
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
func (p *RedisPlugin) DefaultPort() int                   { return p.ConnectionSpec().DefaultPortValue() }
func (p *RedisPlugin) RequiredFields() []string           { return p.ConnectionSpec().RequiredFields() }
func (p *RedisPlugin) SensitiveFields() []string          { return p.ConnectionSpec().SensitiveFields() }
func (p *RedisPlugin) ConnectionIdentityFields() []string { return p.ConnectionSpec().IdentityFields() }
func (p *RedisPlugin) Capabilities() plugin.EngineCapabilities {
	model := p.EngineCatalogModel()
	return plugin.EngineCapabilities{SchemaVersion: plugin.CapabilitiesSchemaVersion, EngineType: p.Type(), EngineFamily: "key_value", Storage: &plugin.StorageCapabilities{
		CatalogModel: &model, Catalog: &plugin.EngineCatalogCapability{Supported: true, RealTime: true},
		Facts: &plugin.EngineCatalogFactsCapability{Supported: true, NativeFacts: true},
		Store: &plugin.StoreCapability{KeyValueRead: true},
	}}
}

func (*RedisPlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.EngineCatalogModelSpec{PathVersion: plugin.EngineCatalogPathVersion, RootTerm: plugin.EngineCatalogTermServer,
		Levels: []plugin.EngineCatalogLevelSpec{{Term: plugin.EngineCatalogTermKey, Kinds: []string{plugin.EngineCatalogTermKey}, Role: plugin.EngineCatalogRoleLeaf, I18nKey: "engine.term.key"}}}
}

func (p *RedisPlugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func connectionInteger(c plugin.ConnectionInfo, key string, fallback, min, max int) (int, error) {
	v, exists := c[key]
	if !exists {
		return fallback, nil
	}
	var n float64
	switch value := v.(type) {
	case int:
		n = float64(value)
	case float64:
		n = value
	default:
		return 0, fmt.Errorf("Redis %s must be an integer", key)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || n < float64(min) || n > float64(max) {
		return 0, fmt.Errorf("Redis %s must be an integer between %d and %d", key, min, max)
	}
	return int(n), nil
}

func validHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-') {
				return false
			}
		}
	}
	return true
}

func (p *RedisPlugin) ValidateConnectionInfo(c plugin.ConnectionInfo) error {
	host, ok := c["host"].(string)
	if !ok || !validHost(strings.TrimSpace(host)) {
		return fmt.Errorf("Redis host must be a hostname or IP address")
	}
	password, ok := c["password"].(string)
	if !ok || password == "" {
		return fmt.Errorf("Redis password is required")
	}
	if user, exists := c["user"]; exists {
		value, ok := user.(string)
		if !ok || value == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("Redis user must be a non-empty string without surrounding whitespace")
		}
	}
	if _, err := connectionInteger(c, "port", 6379, 1, 65535); err != nil {
		return err
	}
	if _, err := connectionInteger(c, "database", 0, 0, 2147483647); err != nil {
		return err
	}
	if value, exists := c["use_ssl"]; exists {
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("Redis use_ssl must be a boolean")
		}
	}
	if value, exists := c["tls_ca_cert"]; exists {
		ca, ok := value.(string)
		if !ok {
			return fmt.Errorf("Redis CA certificate must be PEM text")
		}
		if ca != "" {
			if !plugin.GetBool(c, "use_ssl") {
				return fmt.Errorf("Redis CA certificate requires TLS")
			}
			if !x509.NewCertPool().AppendCertsFromPEM([]byte(ca)) {
				return fmt.Errorf("invalid Redis CA certificate")
			}
		}
	}
	return nil
}

func (p *RedisPlugin) client(c plugin.ConnectionInfo) (*rdb.Client, error) {
	if err := p.ValidateConnectionInfo(c); err != nil {
		return nil, err
	}
	port, _ := connectionInteger(c, "port", 6379, 1, 65535)
	database, _ := connectionInteger(c, "database", 0, 0, 2147483647)
	host := strings.TrimSpace(plugin.GetString(c, "host"))
	user := "default"
	if value, exists := c["user"]; exists {
		user = value.(string)
	}
	options := &rdb.Options{Addr: net.JoinHostPort(plugin.NormalizeHost(host), strconv.Itoa(port)), Username: user, Password: plugin.GetString(c, "password"), DB: database, Protocol: 2, DisableIdentity: true, PoolSize: 1, MaxRetries: -1, DialTimeout: 3 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, ContextTimeoutEnabled: true}
	if plugin.GetBool(c, "use_ssl") {
		options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
		if ca := plugin.GetString(c, "tls_ca_cert"); ca != "" {
			roots, err := x509.SystemCertPool()
			if err != nil {
				return nil, fmt.Errorf("load Redis TLS trust roots: %w", err)
			}
			roots.AppendCertsFromPEM([]byte(ca))
			options.TLSConfig.RootCAs = roots
		}
	}
	baseDialer := rdb.NewDialer(options)
	options.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := baseDialer(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return newBudgetConn(conn), nil
	}
	return rdb.NewClient(options), nil
}

func (p *RedisPlugin) open(ctx context.Context, c plugin.ConnectionInfo) (*rdb.Client, *rdb.Conn, error) {
	client, err := p.client(c)
	if err != nil {
		return nil, nil, err
	}
	// Cancellation must interrupt an in-flight socket read, not only its next command.
	context.AfterFunc(ctx, func() { _ = client.Close() })
	connection := client.Conn()
	fail := func(err error) (*rdb.Client, *rdb.Conn, error) {
		_ = connection.Close()
		_ = client.Close()
		return nil, nil, err
	}
	// Probe mode on the authenticated dedicated connection without resending credentials.
	server, err := connection.Hello(ctx, 2, "", "", "").Result()
	if err != nil {
		return fail(fmt.Errorf("Redis authenticated handshake failed: %w", err))
	}
	if server["mode"] != "standalone" {
		return fail(fmt.Errorf("Redis connection requires standalone server mode"))
	}
	database, _ := connectionInteger(c, "database", 0, 0, 2147483647)
	if err := connection.Select(ctx, database).Err(); err != nil {
		return fail(fmt.Errorf("Redis database selection failed: %w", err))
	}
	return client, connection, nil
}

func (p *RedisPlugin) TestConnection(ctx context.Context, c plugin.ConnectionInfo) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client, connection, err := p.open(ctx, c)
	if err != nil {
		return err
	}
	defer client.Close()
	defer connection.Close()
	if err := connection.DBSize(ctx).Err(); err != nil {
		return fmt.Errorf("Redis database read failed: %w", err)
	}
	return nil
}
