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
	if result.Table == nil || result.Table.RowCount == nil || *result.Table.RowCount < 0 || len(result.Table.Fields) == 0 {
		return nil, fmt.Errorf("ORC runtime returned incomplete table facts")
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
	if options != nil {
		if err := resume.RejectUnsupported(options.ResumeMarker, "orc.table_reader"); err != nil {
			return nil, err
		}
	}
	info, err := p.DescribeTable(ctx, options)
	if err != nil {
		return nil, err
	}
	version := info.FormatInfo["source_version"].(string)
	return &reader{provider: p, fields: info.Table.Fields, version: version, count: *info.Table.RowCount}, nil
}
func (p *provider) SampleTable(ctx context.Context, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	if offset < 0 || limit <= 0 || limit > MaxBatchRows {
		return nil, fmt.Errorf("ORC window requires offset >= 0 and limit in 1..%d", MaxBatchRows)
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

func (r *reader) Fields() []datatype.FieldInfo { return append([]datatype.FieldInfo(nil), r.fields...) }
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
			if v == nil {
				continue
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
				row[f.Name] = n
			case datatype.FieldTypeBytes:
				s, ok := v.(string)
				if !ok {
					return nil, fmt.Errorf("ORC bytes must be base64")
				}
				b, e := base64.StdEncoding.Strict().DecodeString(s)
				if e != nil {
					return nil, e
				}
				row[f.Name] = b
			case datatype.FieldTypeDecimal:
				if _, ok := v.(string); !ok {
					return nil, fmt.Errorf("ORC decimal must be exact text")
				}
			case datatype.FieldTypeInt:
				n, ok := v.(float64)
				if !ok || math.Trunc(n) != n || n < math.MinInt32 || n > math.MaxInt32 {
					return nil, fmt.Errorf("ORC int is invalid")
				}
				row[f.Name] = int64(n)
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
		}
	}
	r.offset += int64(len(rows))
	return rows, nil
}

var _ format.RuntimeTableProviderFactory = (*Plugin)(nil)
var _ format.BoundTableProvider = (*provider)(nil)
