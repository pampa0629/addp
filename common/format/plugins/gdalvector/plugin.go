package gdalvector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	"github.com/addp/common/resume"
)

const (
	BatchProtocol             = "gdal.vector-batch/v1"
	ContainerInspectionSchema = "gdal.vector-dataset.inspect/v1"
)

const (
	operatorInspect    = "vector_dataset.inspect"
	operatorReadOpen   = "vector_dataset.read_open"
	operatorReadBatch  = "vector_dataset.read_batch"
	operatorReadClose  = "vector_dataset.read_close"
	operatorWriteOpen  = "vector_dataset.write_open"
	operatorWriteBatch = "vector_dataset.write_batch"
	operatorWriteClose = "vector_dataset.write_close"
	operatorWriteAbort = "vector_dataset.write_abort"
)

var (
	inspectOperators = []string{operatorInspect}
	readOperators    = []string{operatorReadOpen, operatorReadBatch, operatorReadClose}
	writeOperators   = []string{operatorWriteOpen, operatorWriteBatch, operatorWriteClose, operatorWriteAbort}
)

type basePlugin struct {
	formatType format.FormatType
	descriptor format.FormatDescriptor
}

type ReadOnlyPlugin struct{ *basePlugin }

type ReadWritePlugin struct{ *ReadOnlyPlugin }

func NewReadOnlyPlugin(descriptor format.FormatDescriptor) *ReadOnlyPlugin {
	return &ReadOnlyPlugin{basePlugin: &basePlugin{formatType: descriptor.Format, descriptor: descriptor}}
}

func NewReadWritePlugin(descriptor format.FormatDescriptor) *ReadWritePlugin {
	return &ReadWritePlugin{ReadOnlyPlugin: NewReadOnlyPlugin(descriptor)}
}

func (p *basePlugin) Format() format.FormatType { return p.formatType }

func (p *basePlugin) Descriptor() format.FormatDescriptor { return p.descriptor }

func (p *basePlugin) SpatialEncodingCapability() format.SpatialEncodingCapability {
	return format.SpatialEncodingCapability{
		GeometryReadEncodings:  []format.GeometryEncoding{format.GeometryEncodingEWKB},
		GeometryWriteEncodings: []format.GeometryEncoding{format.GeometryEncodingEWKB},
		DefaultReadEncoding:    format.GeometryEncodingEWKB,
		DefaultWriteEncoding:   format.GeometryEncodingEWKB,
		NativeReadEncoding:     format.GeometryEncodingEWKB,
		NativeWriteEncoding:    format.GeometryEncodingEWKB,
	}
}

func (p *ReadOnlyPlugin) RequiredScopeTableReadOperators() []string {
	return append([]string(nil), readOperators...)
}

func (p *ReadOnlyPlugin) RequiredContainerInfoOperators() []string {
	return append([]string(nil), inspectOperators...)
}

func (p *ReadOnlyPlugin) BindContainerInfoProvider(runtime engineplugin.WorkflowRuntimeProvider, runtimeConn engineplugin.ConnectionInfo, plan workflowaccess.SourcePlan) (format.BoundContainerInfoProvider, error) {
	if err := validateRuntime(runtime, runtimeConn); err != nil {
		return nil, err
	}
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("validate GDAL vector source plan: %w", err)
	}
	if !strings.EqualFold(plan.Source.Format, string(p.Format())) {
		return nil, fmt.Errorf("GDAL vector source format %q does not match plugin %q", plan.Source.Format, p.Format())
	}
	return &runtimeProvider{formatType: p.Format(), runtime: runtime, runtimeConn: cloneConnectionInfo(runtimeConn), sourcePlan: &plan}, nil
}

func (p *ReadOnlyPlugin) BindScopeTableReader(runtime engineplugin.WorkflowRuntimeProvider, runtimeConn engineplugin.ConnectionInfo, plan workflowaccess.SourcePlan) (format.ScopeTableReaderProvider, error) {
	if err := validateRuntime(runtime, runtimeConn); err != nil {
		return nil, err
	}
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("validate GDAL vector source plan: %w", err)
	}
	if !strings.EqualFold(plan.Source.Format, string(p.Format())) {
		return nil, fmt.Errorf("GDAL vector source format %q does not match plugin %q", plan.Source.Format, p.Format())
	}
	return &runtimeProvider{formatType: p.Format(), runtime: runtime, runtimeConn: cloneConnectionInfo(runtimeConn), sourcePlan: &plan}, nil
}

func (p *ReadWritePlugin) RequiredScopeTableWriteOperators() []string {
	return append([]string(nil), writeOperators...)
}

func (p *ReadWritePlugin) BindScopeTableWriter(runtime engineplugin.WorkflowRuntimeProvider, runtimeConn engineplugin.ConnectionInfo, plan workflowaccess.TargetPlan) (format.ScopeTableWriterProvider, error) {
	if err := validateRuntime(runtime, runtimeConn); err != nil {
		return nil, err
	}
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("validate GDAL vector target plan: %w", err)
	}
	if !strings.EqualFold(plan.Target.Format, string(p.Format())) {
		return nil, fmt.Errorf("GDAL vector target format %q does not match plugin %q", plan.Target.Format, p.Format())
	}
	return &runtimeProvider{formatType: p.Format(), runtime: runtime, runtimeConn: cloneConnectionInfo(runtimeConn), targetPlan: &plan}, nil
}

type runtimeProvider struct {
	formatType  format.FormatType
	runtime     engineplugin.WorkflowRuntimeProvider
	runtimeConn engineplugin.ConnectionInfo
	sourcePlan  *workflowaccess.SourcePlan
	targetPlan  *workflowaccess.TargetPlan
}

func (p *runtimeProvider) Format() format.FormatType { return p.formatType }

func (p *runtimeProvider) DescribeContainer(ctx context.Context, options *format.ParseOptions) (*format.ContainerDescribeResult, error) {
	if p.sourcePlan == nil {
		return nil, fmt.Errorf("GDAL vector source plan is not bound")
	}
	params := map[string]interface{}{
		"access_plan": p.sourcePlan.JSONMap(),
		"child_limit": containerChildLimit(options),
	}
	result, err := p.invoke(ctx, operatorInspect, params)
	if err != nil {
		return nil, err
	}
	if schema, _ := result.Result["schema_version"].(string); schema != ContainerInspectionSchema {
		return nil, fmt.Errorf("unsupported GDAL vector inspection schema %q", schema)
	}
	if resultFormat, _ := result.Result["format"].(string); !strings.EqualFold(resultFormat, string(p.Format())) {
		return nil, fmt.Errorf("GDAL vector inspection format %q does not match plugin %q", resultFormat, p.Format())
	}
	var container datatype.ContainerInfo
	if err := decodeResultValue(result.Result["container"], &container); err != nil {
		return nil, fmt.Errorf("decode GDAL vector container info: %w", err)
	}
	if container.ChildCount < len(container.Children) {
		return nil, fmt.Errorf("GDAL vector container child_count %d is smaller than returned children %d", container.ChildCount, len(container.Children))
	}
	seen := make(map[string]struct{}, len(container.Children))
	for index, child := range container.Children {
		name := strings.TrimSpace(child.Name)
		if name == "" {
			return nil, fmt.Errorf("GDAL vector container child %d has no name", index)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("GDAL vector container contains duplicate child %q", name)
		}
		seen[name] = struct{}{}
		if child.DataType != datatype.Table {
			return nil, fmt.Errorf("GDAL vector container child %q has unsupported data_type %q", name, child.DataType)
		}
		if strings.TrimSpace(stringOption(child.Native, "table")) == "" {
			return nil, fmt.Errorf("GDAL vector container child %q has no native table", name)
		}
	}
	formatInfo := map[string]interface{}{}
	if value := result.Result["format_info"]; value != nil {
		if err := decodeResultValue(value, &formatInfo); err != nil {
			return nil, fmt.Errorf("decode GDAL vector format info: %w", err)
		}
	}
	return &format.ContainerDescribeResult{Container: container.Clone(), FormatInfo: formatInfo}, nil
}

func (p *runtimeProvider) OpenTableScopeReader(ctx context.Context, _ contentio.Reader, _ contentio.Ref, options *format.ParseOptions) (format.TableReader, error) {
	if p.sourcePlan == nil {
		return nil, fmt.Errorf("GDAL vector source plan is not bound")
	}
	if options != nil {
		if err := resume.RejectUnsupported(options.ResumeMarker, "gdal.vector.scope_table_reader"); err != nil {
			return nil, err
		}
		if options.GeometryEncoding != "" && options.GeometryEncoding != format.GeometryEncodingWKT && options.GeometryEncoding != format.GeometryEncodingEWKB {
			return nil, fmt.Errorf("GDAL vector reader requires EWKB geometry encoding")
		}
	}
	layer := selectedLayer(options)
	if layer == "" {
		return nil, fmt.Errorf("GDAL vector reader requires selected container child layer")
	}
	params := map[string]interface{}{
		"protocol":    BatchProtocol,
		"access_plan": p.sourcePlan.JSONMap(),
		"layer":       layer,
	}
	result, err := p.invoke(ctx, operatorReadOpen, params)
	if err != nil {
		return nil, err
	}
	var fields []datatype.FieldInfo
	if err := decodeResultValue(result.Result["fields"], &fields); err != nil {
		return nil, fmt.Errorf("decode GDAL vector fields: %w", err)
	}
	var spatial datatype.SpatialInfo
	if value := result.Result["spatial"]; value != nil {
		if err := decodeResultValue(value, &spatial); err != nil {
			return nil, fmt.Errorf("decode GDAL vector spatial info: %w", err)
		}
	}
	return &tableReader{provider: p, params: params, fields: fields, spatial: spatial.Clone()}, nil
}

func (p *runtimeProvider) OpenTableScopeWriter(ctx context.Context, _ contentio.Writer, _ contentio.Ref, tableInfo *datatype.TableInfo, options *format.WriteOptions) (format.TableWriter, error) {
	if p.targetPlan == nil {
		return nil, fmt.Errorf("GDAL vector target plan is not bound")
	}
	if options != nil {
		if err := resume.RejectUnsupported(options.ResumeMarker, "gdal.vector.scope_table_writer"); err != nil {
			return nil, err
		}
	}
	if tableInfo == nil || len(tableInfo.Fields) == 0 {
		return nil, fmt.Errorf("GDAL vector writer requires table fields")
	}
	layer := tableInfo.Name
	if options != nil {
		if selected := stringOption(options.ExtraParams, "layer"); selected != "" {
			layer = selected
		}
	}
	if strings.TrimSpace(layer) == "" {
		return nil, fmt.Errorf("GDAL vector writer requires target layer name")
	}
	params := map[string]interface{}{
		"protocol":    BatchProtocol,
		"access_plan": p.targetPlan.JSONMap(),
		"layer":       layer,
		"fields":      tableInfo.Fields,
		"spatial":     optionsSpatial(options),
	}
	if _, err := p.invoke(ctx, operatorWriteOpen, params); err != nil {
		return nil, err
	}
	return &tableWriter{provider: p, params: params}, nil
}

func (p *runtimeProvider) invoke(ctx context.Context, operator string, params map[string]interface{}) (*engineplugin.OperatorInvokeResult, error) {
	result, err := p.runtime.InvokeOperator(ctx, p.runtimeConn, operator, engineplugin.OperatorInvokeRequest{Params: params})
	if err != nil {
		return result, fmt.Errorf("invoke GDAL vector operator %s: %w", operator, err)
	}
	if result == nil {
		return nil, fmt.Errorf("GDAL vector operator %s returned no result", operator)
	}
	payload, ok := result.Result["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("GDAL vector operator %s returned a non-object result", operator)
	}
	result.Result = payload
	return result, nil
}

type tableReader struct {
	provider *runtimeProvider
	params   map[string]interface{}
	fields   []datatype.FieldInfo
	spatial  *datatype.SpatialInfo
	offset   int64
	closed   bool
}

func (r *tableReader) Fields() []datatype.FieldInfo {
	return append([]datatype.FieldInfo(nil), r.fields...)
}

func (r *tableReader) SpatialInfo() *datatype.SpatialInfo { return r.spatial.Clone() }

func (r *tableReader) ReadRows(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if r.closed {
		return []map[string]interface{}{}, nil
	}
	if limit <= 0 {
		return nil, fmt.Errorf("GDAL vector read limit must be positive")
	}
	params := cloneMap(r.params)
	params["offset"] = r.offset
	params["limit"] = limit
	result, err := r.provider.invoke(ctx, operatorReadBatch, params)
	if err != nil {
		return nil, err
	}
	var rows []map[string]interface{}
	if err := decodeResultValue(result.Result["rows"], &rows); err != nil {
		return nil, fmt.Errorf("decode GDAL vector rows: %w", err)
	}
	if err := decodeBinaryFields(rows, r.fields); err != nil {
		return nil, err
	}
	r.offset += int64(len(rows))
	return rows, nil
}

func (r *tableReader) Close(ctx context.Context) error {
	if r.closed {
		return nil
	}
	r.closed = true
	_, err := r.provider.invoke(ctx, operatorReadClose, r.params)
	return err
}

type tableWriter struct {
	provider *runtimeProvider
	params   map[string]interface{}
	offset   int64
	closed   bool
}

func (w *tableWriter) WriteRows(ctx context.Context, rows []map[string]interface{}) error {
	if w.closed {
		return fmt.Errorf("GDAL vector writer is closed")
	}
	if len(rows) == 0 {
		return nil
	}
	params := cloneMap(w.params)
	params["offset"] = w.offset
	params["rows"] = rows
	if _, err := w.provider.invoke(ctx, operatorWriteBatch, params); err != nil {
		return err
	}
	w.offset += int64(len(rows))
	return nil
}

func (w *tableWriter) Close(ctx context.Context) error {
	if w.closed {
		return nil
	}
	params := cloneMap(w.params)
	params["expected_row_count"] = w.offset
	if _, err := w.provider.invoke(ctx, operatorWriteClose, params); err != nil {
		return err
	}
	w.closed = true
	return nil
}

func (w *tableWriter) Abort(ctx context.Context) error {
	if w.closed {
		return nil
	}
	w.closed = true
	_, err := w.provider.invoke(ctx, operatorWriteAbort, w.params)
	return err
}

func validateRuntime(runtime engineplugin.WorkflowRuntimeProvider, conn engineplugin.ConnectionInfo) error {
	if runtime == nil {
		return fmt.Errorf("workflow runtime provider is required")
	}
	if err := runtime.ValidateConnectionInfo(conn); err != nil {
		return fmt.Errorf("validate workflow runtime connection: %w", err)
	}
	return nil
}

func selectedLayer(options *format.ParseOptions) string {
	if options == nil {
		return ""
	}
	if layer := stringOption(options.ExtraParams, "layer"); layer != "" {
		return layer
	}
	if layer := stringOption(options.ExtraParams, format.ChildTableParam); layer != "" {
		return layer
	}
	if layer := stringOption(options.ExtraParams, format.ChildNameParam); layer != "" {
		return layer
	}
	return strings.TrimSpace(options.SheetName)
}

func containerChildLimit(options *format.ParseOptions) int {
	const defaultLimit = 100
	if options == nil || options.ExtraParams == nil {
		return defaultLimit
	}
	switch value := options.ExtraParams[format.ContainerChildLimitParam].(type) {
	case int:
		if value > 0 {
			return value
		}
	case int64:
		if value > 0 {
			return int(value)
		}
	case float64:
		if value > 0 {
			return int(value)
		}
	}
	return defaultLimit
}

func optionsSpatial(options *format.WriteOptions) *datatype.SpatialInfo {
	if options == nil {
		return nil
	}
	return options.SpatialInfo.Clone()
}

func stringOption(values map[string]interface{}, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
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

func decodeBinaryFields(rows []map[string]interface{}, fields []datatype.FieldInfo) error {
	for _, field := range fields {
		if field.Type != datatype.FieldTypeBytes && field.Type != datatype.FieldTypeGeometry {
			continue
		}
		for rowIndex, row := range rows {
			value, ok := row[field.Name]
			if !ok || value == nil {
				continue
			}
			encoded, ok := value.(string)
			if !ok {
				return fmt.Errorf("GDAL vector row %d field %q must be base64 text, got %T", rowIndex, field.Name, value)
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return fmt.Errorf("decode GDAL vector row %d field %q: %w", rowIndex, field.Name, err)
			}
			row[field.Name] = decoded
		}
	}
	return nil
}

func cloneConnectionInfo(value engineplugin.ConnectionInfo) engineplugin.ConnectionInfo {
	cloned := make(engineplugin.ConnectionInfo, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

func cloneMap(value map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(value)+2)
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

var (
	_ format.RuntimeContainerInfoProviderFactory = (*ReadOnlyPlugin)(nil)
	_ format.RuntimeContainerInfoProviderFactory = (*ReadWritePlugin)(nil)
	_ format.RuntimeScopeTableReaderFactory      = (*ReadOnlyPlugin)(nil)
	_ format.RuntimeScopeTableReaderFactory      = (*ReadWritePlugin)(nil)
	_ format.RuntimeScopeTableWriterFactory      = (*ReadWritePlugin)(nil)
	_ format.ScopeTableReaderProvider            = (*runtimeProvider)(nil)
	_ format.ScopeTableWriterProvider            = (*runtimeProvider)(nil)
	_ format.BoundContainerInfoProvider          = (*runtimeProvider)(nil)
	_ format.TableSpatialInfoProvider            = (*tableReader)(nil)
	_ format.TableReader                         = (*tableReader)(nil)
	_ format.TableWriter                         = (*tableWriter)(nil)
	_ format.AbortableTableWriter                = (*tableWriter)(nil)
)
