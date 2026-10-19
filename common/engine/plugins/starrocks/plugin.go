package starrocks

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/mappers/mysql"
	commonquery "github.com/addp/common/query"
	mysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Plugin exposes only the admitted internal ordinary-table read surface.
type Plugin struct{}

var certifiedVersion = regexp.MustCompile(`^3\.5\.21-[0-9a-f]+$`)

func init()                          { plugin.Register(&Plugin{}) }
func (*Plugin) Type() string         { return "starrocks" }
func (*Plugin) DisplayName() string  { return "StarRocks" }
func (*Plugin) EngineOrigin() string { return "general" }
func (*Plugin) ConnectionSpec() plugin.ConnectionSpec {
	return plugin.NewConnectionSpec(
		plugin.ConnectionFieldSpec{Key: "host", LabelKey: "storageEngine.host", Input: plugin.ConnectionFieldText, Required: true, Identity: true, Default: "localhost"},
		plugin.ConnectionFieldSpec{Key: "port", LabelKey: "storageEngine.port", Input: plugin.ConnectionFieldNumber, Identity: true, Default: 9030, Min: plugin.Int(1), Max: plugin.Int(65535)},
		plugin.ConnectionFieldSpec{Key: "database", LabelKey: "storageEngine.database", Input: plugin.ConnectionFieldText, Required: true, Identity: true},
		plugin.ConnectionFieldSpec{Key: "user", LabelKey: "storageEngine.username", Input: plugin.ConnectionFieldText, Required: true, Identity: true},
		plugin.ConnectionFieldSpec{Key: "password", LabelKey: "storageEngine.password", Input: plugin.ConnectionFieldPassword, Required: true, Sensitive: true},
	)
}
func (p *Plugin) DefaultPort() int                   { return p.ConnectionSpec().DefaultPortValue() }
func (p *Plugin) RequiredFields() []string           { return p.ConnectionSpec().RequiredFields() }
func (p *Plugin) SensitiveFields() []string          { return p.ConnectionSpec().SensitiveFields() }
func (p *Plugin) ConnectionIdentityFields() []string { return p.ConnectionSpec().IdentityFields() }
func (p *Plugin) ValidateConnectionInfo(info plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(info, p.RequiredFields())
}
func (p *Plugin) BuildDSN(info plugin.ConnectionInfo) (string, error) {
	if err := p.ValidateConnectionInfo(info); err != nil {
		return "", err
	}
	parts := plugin.ParseDriverConnInfo(info, p.DefaultPort(), "")
	c := mysql.NewConfig()
	c.Net, c.Addr, c.DBName, c.User, c.Passwd = "tcp", net.JoinHostPort(parts.Host, fmt.Sprint(parts.Port)), parts.Database, parts.User, parts.Password
	c.ParseTime = true
	c.Timeout, c.ReadTimeout, c.WriteTimeout = 10*time.Second, 30*time.Second, 30*time.Second
	c.Params = map[string]string{"charset": "utf8mb4"}
	return c.FormatDSN(), nil
}
func (*Plugin) SQLDialect() string                 { return commonquery.DialectMySQL }
func (*Plugin) GORMDialect() string                { return "mysql" }
func (*Plugin) QueryLanguages() []string           { return []string{"sql"} }
func (*Plugin) SupportsParameterizedQueries() bool { return true }
func (*Plugin) ControlledReadOnlySQLBoundary() plugin.ControlledReadOnlySQLBoundary {
	return plugin.ControlledReadOnlySQLBoundaryValidatedStatement
}
func (p *Plugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	model := plugin.TabularCatalogModel(plugin.EngineCatalogTermDatabase)
	model.Levels[1].Kinds = []string{"table"}
	return model
}
func (p *Plugin) Capabilities() plugin.EngineCapabilities {
	c := plugin.NewTabularCapabilities(p.Type(), plugin.EngineCatalogTermDatabase, plugin.TabularCapabilityOptions{SupportsParameters: true, IdentifierQuote: "`"})
	c.Compute.Query.ReadOnly = true
	c.Storage.Catalog.NodeKinds = []string{"namespace", "table"}
	c.Storage.CatalogModel.Levels[1].Kinds = []string{"table"}
	return c
}
func (p *Plugin) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}
func (*Plugin) probeSQL(ctx context.Context, db *sql.DB) (plugin.ConnectionProbeResult, error) {
	var version, catalog string
	if err := db.QueryRowContext(ctx, "SELECT current_version(), catalog()").Scan(&version, &catalog); err != nil {
		return plugin.ConnectionProbeResult{}, err
	}
	if !certifiedVersion.MatchString(version) || catalog != "default_catalog" {
		return plugin.ConnectionProbeResult{}, fmt.Errorf("%w: StarRocks 3.5.21 default_catalog required", commonapi.ErrBadRequest)
	}
	return plugin.ConnectionProbeResult{ServerVersion: version}, nil
}
func (p *Plugin) ProbeConnection(ctx context.Context, info plugin.ConnectionInfo) (plugin.ConnectionProbeResult, error) {
	dsn, err := p.BuildDSN(info)
	if err != nil {
		return plugin.ConnectionProbeResult{}, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return plugin.ConnectionProbeResult{}, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return p.probeSQL(ctx, db)
}
func (p *Plugin) TestConnection(ctx context.Context, info plugin.ConnectionInfo) error {
	_, err := p.ProbeConnection(ctx, info)
	return err
}
func (p *Plugin) CreateConnectionPool(info plugin.ConnectionInfo, config *plugin.PoolConfig) (*gorm.DB, error) {
	dsn, err := p.BuildDSN(info)
	if err != nil {
		return nil, err
	}
	db, err := plugin.OpenGORMPool(gormmysql.New(gormmysql.Config{DSN: dsn, SkipInitializeWithVersion: true}), config)
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := p.probeSQL(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}
func (p *Plugin) provenance() shared.MySQLCompatibleQueryProvenance {
	return shared.MySQLCompatibleQueryProvenance{EngineName: p.DisplayName(), DefaultPort: p.DefaultPort(), CatalogModel: p.EngineCatalogModel(), BuildDSN: p.BuildDSN, IsSystemNamespace: isSystemNamespace, DescribeFacts: p.DescribeEngineCatalogFacts, TableEngine: "StarRocks"}
}
func (p *Plugin) PrepareQuery(_ context.Context, info plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.PreparedQuery, error) {
	if !req.Options.ReadOnly {
		return nil, commonapi.ErrForbidden
	}
	provenance := p.provenance()
	return plugin.PrepareSQLRuntimeQuery(p, info, req, provenance.ResolveReadSet, provenance.ResolveOutputLineage)
}
func (p *Plugin) GenerateSampleQuery(ctx context.Context, info plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return p.provenance().GenerateSampleQuery(ctx, info, opts.Path, p.SQLDialect(), 10), "sql"
}
func (p *Plugin) ExecuteSQL(ctx context.Context, info plugin.ConnectionInfo, statement string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	if !opts.ReadOnly {
		return nil, commonapi.ErrForbidden
	}
	return plugin.ExecuteSingleStatementReadOnlySQLWithConnectionPool(ctx, p, info, statement, opts, resultField)
}
func (p *Plugin) ReadBatch(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BatchReadOptions) (*plugin.BatchData, error) {
	if _, err := p.ResolvePath(ctx, info, path); err != nil {
		return nil, err
	}
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		parts := plugin.EngineCatalogPathWithoutRoot(path).Segments
		if len(parts) != 2 {
			return nil, fmt.Errorf("StarRocks table path required")
		}
		limit := opts.Limit
		if limit <= 0 {
			limit = 1000
		}
		query = commonquery.ForDialect(p.SQLDialect()).SelectTableSQL("*", parts[0].Name, parts[1].Name, "", "", limit, int(opts.Offset))
	} else if opts.Limit > 0 {
		query = commonquery.ForDialect(p.SQLDialect()).PaginateQuerySQL(query, opts.Limit, int(opts.Offset))
	}
	result, err := p.ExecuteSQL(ctx, info, query, plugin.QueryOptions{ReadOnly: true, Args: opts.Args})
	if err != nil {
		return nil, err
	}
	return &plugin.BatchData{Fields: result.Fields, Rows: result.Rows, Offset: opts.Offset}, nil
}
func nativeFieldType(native string) datatype.FieldType {
	return format.GetTypeMapper("mysql").ToCommon(native)
}
func resultField(column *sql.ColumnType) datatype.FieldInfo {
	field := plugin.SQLResultFieldMapper(nativeFieldType)(column)
	if field.Type == datatype.FieldTypeDecimal {
		precision, scale, known := column.DecimalSize()
		if known && precision >= 3 && precision <= 40 && scale >= 0 && scale <= precision-2 {
			field.Precision, field.Scale = int(precision-2), int(scale)
		} else {
			field.Type, field.Precision, field.Scale = datatype.FieldTypeUnknown, 0, 0
		}
	}
	return field
}
