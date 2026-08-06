package supermap_workflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
)

const (
	SuperMapTableBatchProtocol = "supermap.table-batch/v1"

	operatorTableDelete       = "table.delete"
	operatorTableReadOpen     = "table.read_open"
	operatorTableReadBatch    = "table.read_batch"
	operatorTableReadClose    = "table.read_close"
	operatorTableWritePrepare = "table.write_prepare"
	operatorTableWriteOpen    = "table.write_open"
	operatorTableWriteBatch   = "table.write_batch"
	operatorTableWriteClose   = "table.write_close"
	operatorTableWriteAbort   = "table.write_abort"
)

func RequiredTableReadOperators() []string {
	return []string{operatorTableReadOpen, operatorTableReadBatch, operatorTableReadClose}
}

func RequiredTableWriteOperators() []string {
	return []string{
		operatorTableDelete,
		operatorTableWritePrepare,
		operatorTableWriteOpen,
		operatorTableWriteBatch,
		operatorTableWriteClose,
		operatorTableWriteAbort,
	}
}

func RequiredTableOperators() []string {
	operators := RequiredTableReadOperators()
	return append(operators, RequiredTableWriteOperators()...)
}

// SDXPostgreSQLTableProvider adapts one PostgreSQL Engine Instance to its bound
// SuperMap Workflow runtime. It is instance-scoped and is never registered as a
// separate database engine type.
type SDXPostgreSQLTableProvider struct {
	runtime     plugin.WorkflowRuntimeProvider
	runtimeConn plugin.ConnectionInfo
	postgresql  *postgresql.PostgreSQLPlugin
}

func NewSDXPostgreSQLTableProvider(runtime plugin.WorkflowRuntimeProvider, runtimeConn plugin.ConnectionInfo) (*SDXPostgreSQLTableProvider, error) {
	if runtime == nil {
		return nil, fmt.Errorf("SuperMap Workflow runtime provider is required")
	}
	if err := runtime.ValidateConnectionInfo(runtimeConn); err != nil {
		return nil, fmt.Errorf("validate SuperMap Workflow runtime connection: %w", err)
	}
	return &SDXPostgreSQLTableProvider{
		runtime:     runtime,
		runtimeConn: cloneConnectionInfo(runtimeConn),
		postgresql:  &postgresql.PostgreSQLPlugin{},
	}, nil
}

func (p *SDXPostgreSQLTableProvider) Type() string { return "supermap_workflow" }

func (p *SDXPostgreSQLTableProvider) DisplayName() string {
	return "SuperMap SDX+ for PostgreSQL Table Provider"
}

func (p *SDXPostgreSQLTableProvider) EngineOrigin() string { return "extension" }

func (p *SDXPostgreSQLTableProvider) DefaultPort() int { return 0 }

func (p *SDXPostgreSQLTableProvider) RequiredFields() []string { return nil }

func (p *SDXPostgreSQLTableProvider) SensitiveFields() []string { return nil }

func (p *SDXPostgreSQLTableProvider) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	if strings.TrimSpace(plugin.GetString(connInfo, "host")) == "" || plugin.GetInt(connInfo, "port") == 0 || strings.TrimSpace(plugin.GetString(connInfo, "database")) == "" || strings.TrimSpace(plugin.GetString(connInfo, "user")) == "" {
		return fmt.Errorf("SuperMap SDX+ for PostgreSQL requires host, port, database, and user")
	}
	return nil
}

func (p *SDXPostgreSQLTableProvider) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	if err := p.ValidateConnectionInfo(connInfo); err != nil {
		return err
	}
	return p.runtime.TestConnection(ctx, p.runtimeConn)
}

func (p *SDXPostgreSQLTableProvider) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{
		SchemaVersion: plugin.CapabilitiesSchemaVersion,
		EngineType:    p.Type(),
		EngineFamily:  "workflow",
		Storage: &plugin.StorageCapabilities{Store: &plugin.StoreCapability{
			Delete:            true,
			TableReadSession:  true,
			TableWritePrepare: true,
			TableWriteSession: true,
			TableSpatialEncoding: &plugin.NativeTableSpatialEncodingCapability{
				GeometryReadEncodings:  []string{"ewkb"},
				GeometryWriteEncodings: []string{"ewkb"},
			},
		}},
	}
}

func (p *SDXPostgreSQLTableProvider) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}

func (p *SDXPostgreSQLTableProvider) CatalogModel() plugin.CatalogModelSpec {
	return p.postgresql.CatalogModel()
}

func (p *SDXPostgreSQLTableProvider) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	return p.postgresql.ListChildren(ctx, connInfo, parent, opts)
}

func (p *SDXPostgreSQLTableProvider) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	return p.postgresql.ResolvePath(ctx, connInfo, path)
}

func (p *SDXPostgreSQLTableProvider) DescribeCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	if !isSuperMapSDXTablePath(path) {
		return p.postgresql.DescribeCatalogFacts(ctx, connInfo, path, opts)
	}
	params, err := p.tableParams(connInfo, path)
	if err != nil {
		return nil, err
	}
	params["protocol"] = SuperMapTableBatchProtocol
	params["query"] = ""
	params["hints"] = map[string]interface{}{}
	result, err := p.invoke(ctx, operatorTableReadOpen, params)
	if err != nil {
		return nil, err
	}
	sessionID, err := requiredResultString(result, "session_id")
	if err != nil {
		return nil, fmt.Errorf("describe SuperMap table facts: %w", err)
	}
	closeSession := func() error {
		_, closeErr := p.invoke(ctx, operatorTableReadClose, map[string]interface{}{"session_id": sessionID})
		return closeErr
	}

	var fields []datatype.FieldInfo
	if err := decodeResultValue(result.Result["fields"], &fields); err != nil {
		_ = closeSession()
		return nil, fmt.Errorf("decode SuperMap table fields: %w", err)
	}
	var spatialInfo datatype.SpatialInfo
	if err := decodeResultValue(result.Result["spatial"], &spatialInfo); err != nil {
		_ = closeSession()
		return nil, fmt.Errorf("decode SuperMap table spatial facts: %w", err)
	}
	var rowCount int64
	if err := decodeResultValue(result.Result["row_count"], &rowCount); err != nil {
		_ = closeSession()
		return nil, fmt.Errorf("decode SuperMap table row count: %w", err)
	}
	if err := closeSession(); err != nil {
		return nil, fmt.Errorf("close SuperMap table facts session: %w", err)
	}

	_, table, err := superMapTablePath(path)
	if err != nil {
		return nil, err
	}
	return &plugin.CatalogFacts{
		Path: path,
		Kind: plugin.CatalogKindTable,
		Table: &datatype.TableInfo{
			Name:     table,
			Kind:     plugin.CatalogKindTable,
			RowCount: &rowCount,
			Fields:   fields,
		},
		Spatial: spatialInfo.Clone(),
	}, nil
}

func (p *SDXPostgreSQLTableProvider) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) error {
	params, err := p.tableParams(connInfo, path)
	if err != nil {
		return err
	}
	_, err = p.invoke(ctx, operatorTableDelete, params)
	return err
}

func (p *SDXPostgreSQLTableProvider) PrepareTableWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableWriteOptions) error {
	params, err := p.tableParams(connInfo, path)
	if err != nil {
		return err
	}
	params["protocol"] = SuperMapTableBatchProtocol
	params["fields"] = opts.Fields
	params["spatial"] = opts.SpatialInfo
	_, err = p.invoke(ctx, operatorTableWritePrepare, params)
	return err
}

func (p *SDXPostgreSQLTableProvider) OpenTableReadSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableReadSessionOptions) (plugin.TableReadSession, error) {
	if opts.ResumeMarker != nil {
		return nil, fmt.Errorf("SuperMap table read session does not support resume markers")
	}
	params, err := p.tableParams(connInfo, path)
	if err != nil {
		return nil, err
	}
	params["protocol"] = SuperMapTableBatchProtocol
	params["query"] = strings.TrimSpace(opts.Query)
	params["hints"] = opts.Hints
	result, err := p.invoke(ctx, operatorTableReadOpen, params)
	if err != nil {
		return nil, err
	}
	sessionID, err := requiredResultString(result, "session_id")
	if err != nil {
		return nil, fmt.Errorf("open SuperMap table read session: %w", err)
	}
	return &sdxPostgreSQLReadSession{provider: p, sessionID: sessionID}, nil
}

func (p *SDXPostgreSQLTableProvider) OpenTableWriteSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableWriteSessionOptions) (plugin.TableWriteSession, error) {
	if opts.ResumeMarker != nil {
		return nil, fmt.Errorf("SuperMap table write session does not support resume markers")
	}
	params, err := p.tableParams(connInfo, path)
	if err != nil {
		return nil, err
	}
	params["protocol"] = SuperMapTableBatchProtocol
	params["fields"] = opts.Fields
	params["spatial"] = opts.SpatialInfo
	params["replace"] = opts.Replace
	result, err := p.invoke(ctx, operatorTableWriteOpen, params)
	if err != nil {
		return nil, err
	}
	sessionID, err := requiredResultString(result, "session_id")
	if err != nil {
		return nil, fmt.Errorf("open SuperMap table write session: %w", err)
	}
	return &sdxPostgreSQLWriteSession{provider: p, sessionID: sessionID}, nil
}

func (p *SDXPostgreSQLTableProvider) tableParams(connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (map[string]interface{}, error) {
	if err := p.ValidateConnectionInfo(connInfo); err != nil {
		return nil, err
	}
	schema, table, err := superMapTablePath(path)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"connection_info": cloneConnectionInfo(connInfo),
		"schema":          schema,
		"table":           table,
	}, nil
}

func (p *SDXPostgreSQLTableProvider) invoke(ctx context.Context, operator string, params map[string]interface{}) (*plugin.OperatorInvokeResult, error) {
	result, err := p.runtime.InvokeOperator(ctx, p.runtimeConn, operator, plugin.OperatorInvokeRequest{Params: params})
	if err != nil {
		return result, fmt.Errorf("invoke SuperMap operator %s: %w", operator, err)
	}
	if result == nil {
		return nil, fmt.Errorf("SuperMap operator %s returned no result", operator)
	}
	return result, nil
}

type sdxPostgreSQLReadSession struct {
	provider  *SDXPostgreSQLTableProvider
	sessionID string
	closed    bool
}

func (s *sdxPostgreSQLReadSession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	if s.closed {
		return &plugin.BatchData{}, nil
	}
	if limit <= 0 {
		return nil, fmt.Errorf("SuperMap table read batch limit must be positive")
	}
	result, err := s.provider.invoke(ctx, operatorTableReadBatch, map[string]interface{}{
		"protocol":   SuperMapTableBatchProtocol,
		"session_id": s.sessionID,
		"limit":      limit,
	})
	if err != nil {
		return nil, err
	}
	var batch plugin.BatchData
	if err := decodeResultValue(result.Result["batch"], &batch); err != nil {
		return nil, fmt.Errorf("decode SuperMap table batch: %w", err)
	}
	if err := decodeBatchBinaryFields(&batch); err != nil {
		return nil, fmt.Errorf("decode SuperMap table batch binary fields: %w", err)
	}
	return &batch, nil
}

func (s *sdxPostgreSQLReadSession) Close(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	_, err := s.provider.invoke(ctx, operatorTableReadClose, map[string]interface{}{"session_id": s.sessionID})
	return err
}

type sdxPostgreSQLWriteSession struct {
	provider  *SDXPostgreSQLTableProvider
	sessionID string
	closed    bool
}

func (s *sdxPostgreSQLWriteSession) WriteBatch(ctx context.Context, batch *plugin.BatchData) error {
	if s.closed {
		return fmt.Errorf("SuperMap table write session is closed")
	}
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	_, err := s.provider.invoke(ctx, operatorTableWriteBatch, map[string]interface{}{
		"protocol":   SuperMapTableBatchProtocol,
		"session_id": s.sessionID,
		"batch":      batch,
	})
	return err
}

func (s *sdxPostgreSQLWriteSession) Close(ctx context.Context) error {
	if s.closed {
		return nil
	}
	if _, err := s.provider.invoke(ctx, operatorTableWriteClose, map[string]interface{}{"session_id": s.sessionID}); err != nil {
		return err
	}
	s.closed = true
	return nil
}

func (s *sdxPostgreSQLWriteSession) Abort(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	_, err := s.provider.invoke(ctx, operatorTableWriteAbort, map[string]interface{}{"session_id": s.sessionID})
	return err
}

func superMapTablePath(path plugin.CatalogPath) (string, string, error) {
	parts := make([]string, 0, len(path.Segments))
	for _, segment := range path.Segments {
		if name := strings.TrimSpace(segment.Name); name != "" {
			parts = append(parts, name)
		}
	}
	if len(parts) < 2 {
		return "", "", fmt.Errorf("SuperMap table path requires schema and table")
	}
	schema := parts[len(parts)-2]
	if !strings.EqualFold(schema, "sdx") {
		return "", "", fmt.Errorf("SuperMap SDX+ for PostgreSQL table schema must be sdx")
	}
	return schema, parts[len(parts)-1], nil
}

func isSuperMapSDXTablePath(path plugin.CatalogPath) bool {
	parts := make([]string, 0, len(path.Segments))
	for _, segment := range path.Segments {
		if name := strings.TrimSpace(segment.Name); name != "" {
			parts = append(parts, name)
		}
	}
	return len(parts) >= 2 && strings.EqualFold(parts[len(parts)-2], "sdx")
}

func requiredResultString(result *plugin.OperatorInvokeResult, key string) (string, error) {
	if result == nil {
		return "", fmt.Errorf("operator result is required")
	}
	value, _ := result.Result[key].(string)
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("operator result %q is required", key)
	}
	return value, nil
}

func decodeResultValue(value interface{}, target interface{}) error {
	if value == nil {
		return fmt.Errorf("result value is required")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func decodeBatchBinaryFields(batch *plugin.BatchData) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	for _, field := range batch.Fields {
		if field.Type != datatype.FieldTypeBytes && field.Type != datatype.FieldTypeGeometry {
			continue
		}
		for rowIndex, row := range batch.Rows {
			value, exists := row[field.Name]
			if !exists || value == nil {
				continue
			}
			encoded, ok := value.(string)
			if !ok {
				return fmt.Errorf("row %d field %q must be base64 text, got %T", rowIndex, field.Name, value)
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return fmt.Errorf("row %d field %q: %w", rowIndex, field.Name, err)
			}
			row[field.Name] = decoded
		}
	}
	return nil
}

func cloneConnectionInfo(value plugin.ConnectionInfo) plugin.ConnectionInfo {
	cloned := make(plugin.ConnectionInfo, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

var (
	_ plugin.CatalogModelProvider      = (*SDXPostgreSQLTableProvider)(nil)
	_ plugin.CatalogProvider           = (*SDXPostgreSQLTableProvider)(nil)
	_ plugin.CatalogFactsProvider      = (*SDXPostgreSQLTableProvider)(nil)
	_ plugin.TableReadSessionProvider  = (*SDXPostgreSQLTableProvider)(nil)
	_ plugin.TableWritePreparer        = (*SDXPostgreSQLTableProvider)(nil)
	_ plugin.TableWriteSessionProvider = (*SDXPostgreSQLTableProvider)(nil)
	_ plugin.ResourceDeleteProvider    = (*SDXPostgreSQLTableProvider)(nil)
	_ plugin.TableReadSession          = (*sdxPostgreSQLReadSession)(nil)
	_ plugin.TableWriteSession         = (*sdxPostgreSQLWriteSession)(nil)
)
