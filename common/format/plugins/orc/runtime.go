package orc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	"github.com/addp/common/resume"
)

const Protocol = "orc.table-batch/v1"
const MaxBatchRows = 2000

func (p *Plugin) RequiredTableReadOperators() []string {
	return []string{"orc.inspect", "orc.read_batch"}
}
func (p *Plugin) BindTableProvider(runtime engineplugin.WorkflowRuntimeProvider, conn engineplugin.ConnectionInfo, plan workflowaccess.SourcePlan) (format.BoundTableProvider, error) {
	if runtime == nil {
		return nil, fmt.Errorf("ORC workflow runtime is required")
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if plan.Source.Kind != workflowaccess.KindFile || plan.Source.Format != "orc" {
		return nil, fmt.Errorf("ORC requires a single file source plan")
	}
	// Copy private binding values; callers must not replace source or credentials.
	data, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	var bound workflowaccess.SourcePlan
	if err = json.Unmarshal(data, &bound); err != nil {
		return nil, err
	}
	connection := engineplugin.ConnectionInfo{}
	for k, v := range conn {
		connection[k] = v
	}
	return &provider{runtime: runtime, conn: connection, plan: bound}, nil
}

type provider struct {
	runtime engineplugin.WorkflowRuntimeProvider
	conn    engineplugin.ConnectionInfo
	plan    workflowaccess.SourcePlan
}

func (*provider) Format() format.FormatType { return format.FormatORC }
func (p *provider) invoke(ctx context.Context, name string, params map[string]interface{}) (map[string]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	params["access_plan"] = p.plan.JSONMap()
	params["protocol"] = Protocol
	result, err := p.runtime.InvokeOperator(ctx, p.conn, name, engineplugin.OperatorInvokeRequest{Params: params})
	if err != nil {
		if result != nil && result.ErrorCode == "ORC_SOURCE_CHANGED" {
			return nil, format.ErrTableSourceChanged
		}
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("ORC runtime returned no result")
	}
	payload, ok := result.Result["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("ORC runtime returned invalid result")
	}
	if payload["protocol"] != Protocol {
		return nil, fmt.Errorf("ORC batch protocol mismatch")
	}
	return payload, nil
}
func expectedVersion(options *format.ParseOptions) string {
	if options == nil || options.ExtraParams == nil {
		return ""
	}
	value, _ := options.ExtraParams["source_version"].(string)
	return value
}
func (p *provider) DescribeTable(ctx context.Context, options *format.ParseOptions) (*format.TableDescribeResult, error) {
	payload, err := p.invoke(ctx, "orc.inspect", map[string]interface{}{"source_version": expectedVersion(options)})
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(payload["describe"])
	if err != nil {
		return nil, err
	}
	var result format.TableDescribeResult
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result.Table == nil || result.Table.RowCount == nil || *result.Table.RowCount < 0 || len(result.Table.Fields) == 0 || len(result.Table.Fields) > 256 {
		return nil, fmt.Errorf("ORC runtime returned incomplete table facts")
	}
	budget := 1
	seen := map[string]bool{}
	for _, field := range result.Table.Fields {
		if seen[field.Name] {
			return nil, fmt.Errorf("ORC schema repeats field")
		}
		seen[field.Name] = true
		if err := validateField(field, 1, &budget, false); err != nil {
			return nil, err
		}
	}
	if version, _ := result.FormatInfo["source_version"].(string); version == "" {
		return nil, fmt.Errorf("ORC runtime omitted source version")
	}
	if expected := expectedVersion(options); expected != "" && result.FormatInfo["source_version"] != expected {
		return nil, format.ErrTableSourceChanged
	}

	if options != nil {
		return format.ApplyFieldSelectionToTableDescribeResult(&result, options.FieldSelection)
	}
	return &result, nil
}
func (p *provider) OpenTableReader(ctx context.Context, options *format.ParseOptions) (format.TableReader, error) {
	return p.open(ctx, options, false)
}
func (p *provider) open(ctx context.Context, options *format.ParseOptions, preview bool) (*reader, error) {
	if options != nil {
		if err := resume.RejectUnsupported(options.ResumeMarker, "orc.table_reader"); err != nil {
			return nil, err
		}
	}
	fullOptions := format.DefaultParseOptions()
	if options != nil {
		*fullOptions = *options
		fullOptions.FieldSelection = nil
	}
	info, err := p.DescribeTable(ctx, fullOptions)
	if err != nil {
		return nil, err
	}
	if !preview {
		for _, field := range info.Table.Fields {
			if field.Type == datatype.FieldTypeJSON || field.Type == datatype.FieldTypeArray {
				return nil, fmt.Errorf("ORC nested fields support preview only; batch transfer is not supported")
			}
		}
	}
	if options != nil {
		info, err = format.ApplyFieldSelectionToTableDescribeResult(info, options.FieldSelection)
		if err != nil {
			return nil, err
		}
	}
	version := info.FormatInfo["source_version"].(string)
	return &reader{provider: p, fields: info.Table.Fields, version: version, count: *info.Table.RowCount}, nil
}
func (p *provider) SampleTable(ctx context.Context, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	if offset < 0 || limit <= 0 || limit > MaxBatchRows {
		return nil, fmt.Errorf("ORC window requires offset >= 0 and limit in 1..%d", MaxBatchRows)
	}
	r, err := p.open(ctx, options, true)
	if err != nil {
		return nil, err
	}
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
		return nil, fmt.Errorf("ORC batch limit must be positive")
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
	payload, err := r.provider.invoke(ctx, "orc.read_batch", map[string]interface{}{"source_version": r.version, "offset": r.offset, "limit": limit, "columns": names})
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
	expected := int64(limit)
	if r.count-r.offset < expected {
		expected = r.count - r.offset
	}
	if int64(len(rows)) != expected {
		return nil, fmt.Errorf("ORC batch does not match declared row count")
	}
	for _, row := range rows {
		if len(row) != len(r.fields) {
			return nil, fmt.Errorf("ORC row fields differ from schema")
		}
		for _, f := range r.fields {
			v, ok := row[f.Name]
			if !ok {
				return nil, fmt.Errorf("ORC row omitted field")
			}
			decoded, err := decodeValue(v, f, 0)
			if err != nil {
				return nil, err
			}
			row[f.Name] = decoded
		}
	}
	r.offset += int64(len(rows))
	return rows, nil
}

var _ format.RuntimeTableProviderFactory = (*Plugin)(nil)
var _ format.BoundTableProvider = (*provider)(nil)

func decodeValue(v interface{}, f datatype.FieldInfo, depth int) (interface{}, error) {
	if depth > 16 {
		return nil, fmt.Errorf("ORC value depth exceeds budget")
	}
	if v == nil {
		return nil, nil
	}
	switch f.Type {
	case datatype.FieldTypeJSON:
		value, ok := v.(map[string]interface{})
		if !ok || len(value) != len(f.Fields) {
			return nil, fmt.Errorf("ORC object differs from schema")
		}
		object := make(map[string]interface{}, len(value))
		for _, child := range f.Fields {
			raw, present := value[child.Name]
			if !present {
				return nil, fmt.Errorf("ORC object omitted member")
			}
			decoded, err := decodeValue(raw, child, depth+1)
			if err != nil {
				return nil, err
			}
			object[child.Name] = decoded
		}
		return object, nil
	case datatype.FieldTypeArray:
		values, ok := v.([]interface{})
		if !ok || f.Element == nil {
			return nil, fmt.Errorf("ORC array differs from schema")
		}
		array := make([]interface{}, len(values))
		for i, item := range values {
			decoded, err := decodeValue(item, *f.Element, depth+1)
			if err != nil {
				return nil, err
			}
			array[i] = decoded
		}
		return array, nil
	}
	switch f.Type {
	case datatype.FieldTypeBigInt:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("ORC bigint must be exact text")
		}
		n, e := strconv.ParseInt(s, 10, 64)
		if e != nil {
			return nil, e
		}
		return n, nil
	case datatype.FieldTypeBytes:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("ORC bytes must be base64")
		}
		b, e := base64.StdEncoding.Strict().DecodeString(s)
		if e != nil {
			return nil, e
		}
		return b, nil
	case datatype.FieldTypeDecimal:
		if _, ok := v.(string); !ok {
			return nil, fmt.Errorf("ORC decimal must be exact text")
		}
	case datatype.FieldTypeInt:
		n, ok := v.(float64)
		if !ok || math.Trunc(n) != n || n < math.MinInt32 || n > math.MaxInt32 {
			return nil, fmt.Errorf("ORC int is invalid")
		}
		return int64(n), nil
	case datatype.FieldTypeBool:
		if _, ok := v.(bool); !ok {
			return nil, fmt.Errorf("ORC bool is invalid")
		}
	case datatype.FieldTypeString:
		if _, ok := v.(string); !ok {
			return nil, fmt.Errorf("ORC string is invalid")
		}
	case datatype.FieldTypeFloat, datatype.FieldTypeDouble:
		if value, ok := v.(float64); !ok || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("ORC float is invalid")
		}
	case datatype.FieldTypeTimestamp, datatype.FieldTypeDate:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("ORC temporal value must be exact text")
		}
		layout := "2006-01-02T15:04:05.999999999"
		if f.Type == datatype.FieldTypeDate {
			layout = "2006-01-02"
		} else if f.NativeType == "timestamp with local time zone" {
			layout = time.RFC3339Nano
		}
		if _, e := time.Parse(layout, s); e != nil {
			return nil, e
		}
	}
	return v, nil
}

func validateField(f datatype.FieldInfo, depth int, budget *int, element bool) error {
	(*budget)++
	if depth > 16 || *budget > 1024 || (!element && f.Name == "") || (element && f.Name != "") || !f.Nullable {
		return fmt.Errorf("ORC schema is incomplete or exceeds budget")
	}
	switch f.Type {
	case datatype.FieldTypeJSON:
		if len(f.Fields) == 0 || f.Element != nil {
			return fmt.Errorf("ORC object schema is incomplete")
		}
		seen := map[string]bool{}
		for _, child := range f.Fields {
			if seen[child.Name] {
				return fmt.Errorf("ORC object repeats member")
			}
			seen[child.Name] = true
			if err := validateField(child, depth+1, budget, false); err != nil {
				return err
			}
		}
	case datatype.FieldTypeArray:
		if f.Element == nil || f.ElementType != f.Element.Type || len(f.Fields) != 0 {
			return fmt.Errorf("ORC array schema is incomplete")
		}
		return validateField(*f.Element, depth+1, budget, true)
	case datatype.FieldTypeBool, datatype.FieldTypeInt, datatype.FieldTypeBigInt,
		datatype.FieldTypeFloat, datatype.FieldTypeDouble, datatype.FieldTypeString,
		datatype.FieldTypeBytes, datatype.FieldTypeDecimal, datatype.FieldTypeDate, datatype.FieldTypeTimestamp:
		if len(f.Fields) != 0 || f.Element != nil {
			return fmt.Errorf("ORC scalar declares nested schema")
		}
	default:
		return fmt.Errorf("ORC field type is unsupported")
	}
	return nil
}
