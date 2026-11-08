package feather

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	"github.com/addp/common/resume"
)

const Protocol = "feather.table-batch/v1"
const MaxBatchRows = 2000

type provider struct {
	runtime engineplugin.WorkflowRuntimeProvider
	conn    engineplugin.ConnectionInfo
	plan    workflowaccess.SourcePlan
}

func (*Plugin) RequiredTableReadOperators() []string {
	return []string{"feather.inspect", "feather.read_batch"}
}
func (*Plugin) BindTableProvider(runtime engineplugin.WorkflowRuntimeProvider, conn engineplugin.ConnectionInfo, plan workflowaccess.SourcePlan) (format.BoundTableProvider, error) {
	if runtime == nil {
		return nil, fmt.Errorf("Feather workflow runtime is required")
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if plan.Source.Kind != workflowaccess.KindFile || plan.Source.Format != "feather" {
		return nil, fmt.Errorf("Feather requires a single file source plan")
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	var bound workflowaccess.SourcePlan
	if err := json.Unmarshal(encoded, &bound); err != nil {
		return nil, err
	}
	connection := engineplugin.ConnectionInfo{}
	for key, value := range conn {
		connection[key] = value
	}
	return &provider{runtime: runtime, conn: connection, plan: bound}, nil
}
func (*provider) Format() format.FormatType { return format.FormatFeather }
func (p *provider) invoke(ctx context.Context, name string, params map[string]interface{}) (map[string]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	params["protocol"], params["access_plan"] = Protocol, p.plan.JSONMap()
	result, err := p.runtime.InvokeOperator(ctx, p.conn, name, engineplugin.OperatorInvokeRequest{Params: params})
	if result != nil && result.ErrorCode == "FEATHER_SOURCE_CHANGED" {
		return nil, format.ErrTableSourceChanged
	}
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("Feather runtime returned no result")
	}
	payload, ok := result.Result["result"].(map[string]interface{})
	if !ok || payload["protocol"] != Protocol {
		return nil, fmt.Errorf("Feather protocol mismatch")
	}
	return payload, nil
}
func expectedVersion(options *format.ParseOptions) string {
	if options == nil {
		return ""
	}
	value, _ := options.ExtraParams["source_version"].(string)
	return value
}
func (p *provider) DescribeTable(ctx context.Context, options *format.ParseOptions) (*format.TableDescribeResult, error) {
	payload, err := p.invoke(ctx, "feather.inspect", map[string]interface{}{"source_version": expectedVersion(options)})
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(payload["describe"])
	if err != nil {
		return nil, err
	}
	var result format.TableDescribeResult
	if err = json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	if result.Table == nil || result.Table.RowCount == nil || *result.Table.RowCount < 0 || len(result.Table.Fields) == 0 || len(result.Table.Fields) > 256 {
		return nil, fmt.Errorf("Feather runtime omitted bounded table facts")
	}
	seen := map[string]bool{}
	nativeTypes := map[string]datatype.FieldType{"bool": datatype.FieldTypeBool, "int8": datatype.FieldTypeInt, "int16": datatype.FieldTypeInt, "int32": datatype.FieldTypeInt, "int64": datatype.FieldTypeBigInt, "float": datatype.FieldTypeFloat, "double": datatype.FieldTypeDouble, "string": datatype.FieldTypeString, "binary": datatype.FieldTypeBytes}
	for _, field := range result.Table.Fields {
		kind, ok := nativeTypes[field.NativeType]
		if !ok || kind != field.Type || field.Name == "" || seen[field.Name] || len(field.Fields) > 0 || field.Element != nil {
			return nil, fmt.Errorf("Feather runtime schema is outside the scalar profile")
		}
		seen[field.Name] = true
	}
	version, _ := result.FormatInfo["source_version"].(string)
	if version == "" {
		return nil, fmt.Errorf("Feather source version is missing")
	}
	if expected := expectedVersion(options); expected != "" && version != expected {
		return nil, format.ErrTableSourceChanged
	}
	if options != nil {
		return format.ApplyFieldSelectionToTableDescribeResult(&result, options.FieldSelection)
	}
	return &result, nil
}
func (p *provider) OpenTableReader(ctx context.Context, options *format.ParseOptions) (format.TableReader, error) {
	if options != nil {
		if err := resume.RejectUnsupported(options.ResumeMarker, "feather.table_reader"); err != nil {
			return nil, err
		}
	}
	info, err := p.DescribeTable(ctx, options)
	if err != nil {
		return nil, err
	}
	return &reader{provider: p, fields: info.Table.Fields, version: info.FormatInfo["source_version"].(string), count: *info.Table.RowCount}, nil
}
func (p *provider) SampleTable(ctx context.Context, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	if offset < 0 || limit <= 0 || limit > MaxBatchRows {
		return nil, fmt.Errorf("Feather window requires offset >= 0 and limit in 1..%d", MaxBatchRows)
	}
	opened, err := p.OpenTableReader(ctx, options)
	if err != nil {
		return nil, err
	}
	r := opened.(*reader)
	defer r.Close(ctx)
	r.offset = offset
	return r.ReadRows(ctx, int(limit))
}

type reader struct {
	provider      *provider
	fields        []datatype.FieldInfo
	version       string
	count, offset int64
	closed        bool
}

func (r *reader) Fields() []datatype.FieldInfo { return datatype.CloneFieldInfos(r.fields) }
func (r *reader) Close(context.Context) error  { r.closed = true; return nil }
func (r *reader) ReadRows(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, fmt.Errorf("Feather batch limit must be positive")
	}
	if r.closed || r.offset >= r.count {
		return []map[string]interface{}{}, nil
	}
	if limit > MaxBatchRows {
		limit = MaxBatchRows
	}
	names := make([]string, len(r.fields))
	for i, f := range r.fields {
		names[i] = f.Name
	}
	payload, err := r.provider.invoke(ctx, "feather.read_batch", map[string]interface{}{"source_version": r.version, "offset": r.offset, "limit": limit, "columns": names})
	if err != nil {
		return nil, err
	}
	if payload["source_version"] != r.version {
		return nil, format.ErrTableSourceChanged
	}
	raw, err := json.Marshal(payload["rows"])
	if err != nil {
		return nil, err
	}
	var rows []map[string]interface{}
	if err = json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	if int64(len(rows)) != min(int64(limit), r.count-r.offset) {
		return nil, fmt.Errorf("Feather rows differ from declared count")
	}
	for _, row := range rows {
		if len(row) != len(r.fields) {
			return nil, fmt.Errorf("Feather row fields differ from schema")
		}
		for _, field := range r.fields {
			value, present := row[field.Name]
			if !present {
				return nil, fmt.Errorf("Feather row omitted field")
			}
			value, err = decodeValue(value, field)
			if err != nil {
				return nil, err
			}
			row[field.Name] = value
		}
	}
	r.offset += int64(len(rows))
	return rows, nil
}
func decodeValue(value interface{}, field datatype.FieldInfo) (interface{}, error) {
	if value == nil {
		if !field.Nullable {
			return nil, fmt.Errorf("Feather NULL contradicts schema")
		}
		return nil, nil
	}
	switch field.Type {
	case datatype.FieldTypeBigInt:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("Feather bigint requires exact text")
		}
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("Feather bigint is invalid")
		}
		return n, nil
	case datatype.FieldTypeInt:
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < math.MinInt32 || number > math.MaxInt32 {
			return nil, fmt.Errorf("Feather int is invalid")
		}
		return int64(number), nil
	case datatype.FieldTypeBytes:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("Feather bytes require base64")
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(text)
		if err != nil {
			return nil, fmt.Errorf("Feather binary is invalid")
		}
		return raw, nil
	case datatype.FieldTypeBool:
		if _, ok := value.(bool); !ok {
			return nil, fmt.Errorf("Feather bool is invalid")
		}
	case datatype.FieldTypeString:
		if _, ok := value.(string); !ok {
			return nil, fmt.Errorf("Feather text is invalid")
		}
	case datatype.FieldTypeFloat, datatype.FieldTypeDouble:
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("Feather float is invalid")
		}
	default:
		return nil, fmt.Errorf("Feather field type is unsupported")
	}
	return value, nil
}

var _ format.RuntimeTableProviderFactory = (*Plugin)(nil)
var _ format.BoundTableProvider = (*provider)(nil)
