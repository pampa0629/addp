package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/query/plan"
	"github.com/addp/service/internal/models"
)

type analyticalCompiledRequest struct {
	result     *compiledQueryPlan
	query      plugin.QueryRequest
	parameters []models.ExecutionQueryParameter
}

func compileAnalyticalQuery(service *models.QueryService, request *models.QueryExecutionRequest, protocol queryProtocol, engine *commonmodels.Engine, codec *queryTokenCodec) (*compiledQueryPlan, plugin.QueryRequest, error) {
	compiled, err := compileAnalyticalRequest(service, request, protocol, engine, codec)
	if err != nil {
		return nil, plugin.QueryRequest{}, err
	}
	return compiled.result, compiled.query, nil
}

func compileAnalyticalRequest(service *models.QueryService, request *models.QueryExecutionRequest, protocol queryProtocol, engine *commonmodels.Engine, codec *queryTokenCodec) (*analyticalCompiledRequest, error) {
	fail := func(err error) (*analyticalCompiledRequest, error) {
		return nil, fmt.Errorf("%w: %v", ErrInvalidStructuredQuery, err)
	}
	if err := validateAnalyticalPublication(service); err != nil {
		return fail(err)
	}
	instance, compiler, err := analyticalEngine(engine)
	if err != nil {
		return fail(err)
	}
	frozen := service.MetricPlan().ExecutionPlan
	if err := frozen.Verify(instance, compiler); err != nil {
		return fail(err)
	}
	result, fields, after, err := prepareQueryResult(service, request, request.Parameters, codec)
	if err != nil {
		return fail(err)
	}
	resolved := plan.ResultRequest{Select: result.SelectedFields, Limit: result.Limit}
	for i, order := range result.OrderBy {
		resolved.OrderBy = append(resolved.OrderBy, plan.SortKey{Name: order.Field, Direction: order.Direction})
		if len(after) > 0 {
			v, err := analyticalLiteral(fields[order.Field].Type, after[i])
			if err != nil {
				return fail(err)
			}
			resolved.After = append(resolved.After, v)
		}
	}
	if request.Filter != nil {
		nodes := 0
		resolved.Filter, err = analyticalResultFilter(request.Filter, service, protocol, fields, 0, &nodes)
		if err != nil {
			return fail(err)
		}
	}
	extended, err := plan.ApplyResultRequest(frozen.Plan, resolved)
	if err != nil {
		return fail(err)
	}
	values := extended.Values
	if len(request.Parameters) != len(frozen.Plan.Parameters) {
		return fail(fmt.Errorf("metric parameter count mismatch"))
	}
	for _, parameter := range frozen.Plan.Parameters {
		raw, ok := request.Parameters[parameter.Name]
		if !ok {
			return fail(fmt.Errorf("missing metric parameter"))
		}
		value, err := analyticalLiteral(parameter.Type, raw)
		if err != nil {
			return fail(err)
		}
		values[parameter.Name] = value
	}
	compiled, err := compiler.Compile(plugin.CompileRequest{Plan: extended.Plan, Sources: frozen.Sources, Instance: instance})
	if err != nil {
		return fail(err)
	}
	query, err := compiled.QueryRequest(values, 60*time.Second)
	if err != nil {
		return fail(err)
	}
	result.SelectedFields, result.HiddenFields = extended.SelectedFields, extended.HiddenFields
	parameters := make([]models.ExecutionQueryParameter, 0, len(extended.Plan.Parameters))
	for _, parameter := range extended.Plan.Parameters {
		literal, err := values[parameter.Name].Canonical()
		if err != nil {
			return fail(err)
		}
		item := models.ExecutionQueryParameter{Name: parameter.Name, Type: string(parameter.Type)}
		if !literal.Null {
			text := literal.Text
			item.Value = &text
		}
		parameters = append(parameters, item)
	}
	return &analyticalCompiledRequest{result: result, query: query, parameters: parameters}, nil
}

// Preserve exact decimal/integer tokens. A floating-point value cannot establish
// the caller's decimal intent; HTTP and cursor decoders both use json.Number.
func analyticalLiteral(typ datatype.FieldType, value interface{}) (plan.Literal, error) {
	literal := plan.Literal{Type: typ}
	switch v := value.(type) {
	case string:
		literal.Text = v
	case json.Number:
		if typ != datatype.FieldTypeInt && typ != datatype.FieldTypeBigInt && typ != datatype.FieldTypeDecimal {
			return literal, ErrInvalidStructuredQuery
		}
		literal.Text = string(v)
	case bool:
		if typ != datatype.FieldTypeBool {
			return literal, ErrInvalidStructuredQuery
		}
		literal.Text = strconv.FormatBool(v)
	case int:
		literal.Text = strconv.Itoa(v)
	case int32:
		literal.Text = strconv.FormatInt(int64(v), 10)
	case int64:
		literal.Text = strconv.FormatInt(v, 10)
	default:
		return literal, ErrInvalidStructuredQuery
	}
	if _, ok := value.(string); !ok && (typ == datatype.FieldTypeString || typ == datatype.FieldTypeDate) {
		return literal, ErrInvalidStructuredQuery
	}
	return literal.Canonical()
}

func analyticalResultFilter(filter *models.QueryFilter, service *models.QueryService, protocol queryProtocol, fields map[string]datatype.FieldInfo, depth int, nodes *int) (*plan.ResultFilter, error) {
	if filter == nil || depth > 16 {
		return nil, ErrInvalidStructuredQuery
	}
	*nodes++
	if *nodes > 256 {
		return nil, ErrInvalidStructuredQuery
	}
	kinds := 0
	if filter.Field != "" || filter.Op != "" {
		kinds++
	}
	if len(filter.And) > 0 {
		kinds++
	}
	if len(filter.Or) > 0 {
		kinds++
	}
	if filter.Not != nil {
		kinds++
	}
	if kinds != 1 {
		return nil, ErrInvalidStructuredQuery
	}
	out := &plan.ResultFilter{}
	if len(filter.And) > 0 || len(filter.Or) > 0 || filter.Not != nil {
		if filter.Value != nil {
			return nil, ErrInvalidStructuredQuery
		}
		children := filter.And
		out.Op = "and"
		if len(filter.Or) > 0 {
			children = filter.Or
			out.Op = "or"
		}
		if filter.Not != nil {
			children = []models.QueryFilter{*filter.Not}
			out.Op = "not"
		}
		for i := range children {
			child, err := analyticalResultFilter(&children[i], service, protocol, fields, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			out.Children = append(out.Children, *child)
		}
		return out, nil
	}
	name, op := strings.TrimSpace(filter.Field), strings.ToLower(strings.TrimSpace(filter.Op))
	field, ok := fields[name]
	if !ok || !filterFieldAllowed(service, protocol, name, op) {
		return nil, ErrInvalidStructuredQuery
	}
	out.Field, out.Op = name, op
	switch op {
	case "is_null", "is_not_null":
		if filter.Value != nil {
			return nil, ErrInvalidStructuredQuery
		}
		if op == "is_not_null" {
			out.Op = "not_null"
		}
		return out, nil
	case "eq", "ne", "lt", "lte", "gt", "gte", "contains":
		if !filterComparisonAllowed(field.Type, op) {
			return nil, ErrInvalidStructuredQuery
		}
		if op == "lte" {
			out.Op = "le"
		}
		if op == "gte" {
			out.Op = "ge"
		}
		v, err := analyticalLiteral(field.Type, filter.Value)
		if err != nil {
			return nil, err
		}
		out.Values = []plan.Literal{v}
	case "in":
		values, ok := interfaceSlice(filter.Value)
		if !ok || len(values) == 0 || len(values) > 1000 {
			return nil, ErrInvalidStructuredQuery
		}
		for _, raw := range values {
			v, err := analyticalLiteral(field.Type, raw)
			if err != nil {
				return nil, err
			}
			out.Values = append(out.Values, v)
		}
	default:
		return nil, ErrInvalidStructuredQuery
	}
	return out, nil
}
