package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/workbench/internal/models"
)

func validateComponentConfiguration(input models.ComponentConfiguration, descriptor *models.ConsumerDescriptor) error {
	if descriptor == nil || input.ServiceRef == nil || descriptor.SchemaVersion != models.ConsumerDescriptorSchemaVersion ||
		descriptor.Status != "active" || descriptor.Ref != *input.ServiceRef ||
		descriptor.Ref.ServiceType != "query" || descriptor.Ref.ServiceID <= 0 ||
		!strings.HasPrefix(descriptor.ContractFingerprint, "sha256:") || len(descriptor.ContractFingerprint) != 71 ||
		!validConsumerOperation(descriptor) {
		return fmt.Errorf("%w: invalid service descriptor", ErrInvalidComponentConfiguration)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 200 || len([]rune(input.Description)) > 2000 {
		return fmt.Errorf("%w: invalid name or description", ErrInvalidComponentConfiguration)
	}
	queryFields := make(map[string]models.ConsumerQueryField, len(descriptor.InputContract.Fields))
	namedParameters := make(map[string]models.ConsumerNamedParameter, len(descriptor.InputContract.NamedParameters))
	outputFields := make(map[string]models.ConsumerOutputField, len(descriptor.OutputContract.Fields))
	for _, field := range descriptor.InputContract.Fields {
		if field.Name == "" {
			return fmt.Errorf("%w: invalid input field", ErrInvalidComponentConfiguration)
		}
		queryFields[field.Name] = field
	}
	for _, parameter := range descriptor.InputContract.NamedParameters {
		if strings.TrimSpace(parameter.Name) == "" || !selectionScalarFieldType(parameter.Type) {
			return fmt.Errorf("%w: invalid service named parameter", ErrInvalidComponentConfiguration)
		}
		if _, duplicate := namedParameters[parameter.Name]; duplicate || (parameter.Required && parameter.Default != nil) || (!parameter.Required && parameter.Default == nil) {
			return fmt.Errorf("%w: invalid service named parameter", ErrInvalidComponentConfiguration)
		}
		if parameter.Default != nil && validateScalarValue(parameter.Default, parameter.Type) != nil {
			return fmt.Errorf("%w: invalid service named parameter default", ErrInvalidComponentConfiguration)
		}
		namedParameters[parameter.Name] = parameter
	}
	for _, field := range descriptor.OutputContract.Fields {
		outputFields[field.Name] = field
	}
	parameterKeys := make(map[string]models.ComponentParameterDefinition, len(input.ParameterDefinitions))
	for _, parameter := range input.ParameterDefinitions {
		key := strings.TrimSpace(parameter.Key)
		if key == "" || strings.TrimSpace(parameter.Label) == "" || !allowedControlType(parameter.ControlType) {
			return fmt.Errorf("%w: invalid parameter definition", ErrInvalidComponentConfiguration)
		}
		if _, exists := parameterKeys[key]; exists {
			return fmt.Errorf("%w: duplicate parameter key", ErrInvalidComponentConfiguration)
		}
		parameterKeys[key] = parameter
	}
	for key := range input.DefaultParameterValues {
		if _, exists := parameterKeys[key]; !exists {
			return fmt.Errorf("%w: unknown default parameter", ErrInvalidComponentConfiguration)
		}
	}
	if len(input.QueryTemplate.Select) == 0 || input.QueryTemplate.PageLimit <= 0 ||
		input.QueryTemplate.PageLimit > descriptor.InputContract.Page.MaxLimit ||
		!contains(descriptor.InputContract.Formats, input.QueryTemplate.Format) {
		return fmt.Errorf("%w: invalid query template", ErrInvalidComponentConfiguration)
	}
	selectedFields := make(map[string]struct{}, len(input.QueryTemplate.Select))
	for _, fieldName := range input.QueryTemplate.Select {
		field, exists := queryFields[fieldName]
		if !exists || !field.Selectable {
			return fmt.Errorf("%w: field is not selectable", ErrInvalidComponentConfiguration)
		}
		if _, duplicate := selectedFields[fieldName]; duplicate {
			return fmt.Errorf("%w: duplicate selected field", ErrInvalidComponentConfiguration)
		}
		selectedFields[fieldName] = struct{}{}
	}
	parameterBindings := make(map[string]string, len(input.QueryTemplate.ParameterFilters)+len(input.QueryTemplate.NamedParameterBindings))
	for _, binding := range input.QueryTemplate.ParameterFilters {
		if _, exists := parameterKeys[binding.ParameterKey]; !exists {
			return fmt.Errorf("%w: unknown parameter binding", ErrInvalidComponentConfiguration)
		}
		if _, duplicate := parameterBindings[binding.ParameterKey]; duplicate {
			return fmt.Errorf("%w: duplicate parameter binding", ErrInvalidComponentConfiguration)
		}
		field, exists := queryFields[binding.Field]
		if !exists || !field.Filterable || !contains(field.Operators, binding.Operator) {
			return fmt.Errorf("%w: invalid parameter filter", ErrInvalidComponentConfiguration)
		}
		parameterBindings[binding.ParameterKey] = "filter"
	}
	boundNamedParameters := make(map[string]struct{}, len(input.QueryTemplate.NamedParameterBindings))
	for _, binding := range input.QueryTemplate.NamedParameterBindings {
		definition, exists := parameterKeys[binding.ParameterKey]
		if !exists {
			return fmt.Errorf("%w: unknown named parameter binding", ErrInvalidComponentConfiguration)
		}
		serviceParameter, exists := namedParameters[binding.Name]
		if !exists || !controlSupportsNamedParameter(definition.ControlType, serviceParameter.Type) || (serviceParameter.Required && !definition.Required) {
			return fmt.Errorf("%w: invalid named parameter binding", ErrInvalidComponentConfiguration)
		}
		if _, duplicate := parameterBindings[binding.ParameterKey]; duplicate {
			return fmt.Errorf("%w: duplicate parameter binding", ErrInvalidComponentConfiguration)
		}
		if _, duplicate := boundNamedParameters[binding.Name]; duplicate {
			return fmt.Errorf("%w: duplicate service named parameter binding", ErrInvalidComponentConfiguration)
		}
		parameterBindings[binding.ParameterKey] = "named"
		boundNamedParameters[binding.Name] = struct{}{}
	}
	for name, parameter := range namedParameters {
		if parameter.Required {
			if _, bound := boundNamedParameters[name]; !bound {
				return fmt.Errorf("%w: required service named parameter is not bound", ErrInvalidComponentConfiguration)
			}
		}
	}
	if len(parameterBindings) != len(parameterKeys) {
		return fmt.Errorf("%w: every parameter requires one binding", ErrInvalidComponentConfiguration)
	}
	for key, raw := range input.DefaultParameterValues {
		if filter, exists := componentParameterFilterFromTemplate(input.QueryTemplate, key); exists {
			if err := validateRawFilterValue(raw, queryFields[filter.Field], filter.Operator, descriptor.InputContract.Filter.MaxInValues); err != nil {
				return fmt.Errorf("%w: invalid default parameter %s: %v", ErrInvalidComponentConfiguration, key, err)
			}
			continue
		}
		binding, exists := componentNamedParameterBindingFromTemplate(input.QueryTemplate, key)
		if !exists {
			return fmt.Errorf("%w: invalid default parameter %s", ErrInvalidComponentConfiguration, key)
		}
		if err := validateRawNamedParameterValue(raw, namedParameters[binding.Name].Type); err != nil {
			return fmt.Errorf("%w: invalid default parameter %s: %v", ErrInvalidComponentConfiguration, key, err)
		}
	}
	filterNodes := 0
	if err := validateFilter(input.QueryTemplate.FixedFilter, descriptor, queryFields, 1, &filterNodes); err != nil {
		return err
	}
	for _, order := range input.QueryTemplate.OrderBy {
		field, exists := queryFields[order.Field]
		if !exists || !field.Sortable || !contains(descriptor.InputContract.Order.Directions, order.Direction) {
			return fmt.Errorf("%w: invalid order field", ErrInvalidComponentConfiguration)
		}
	}
	return validateRenderer(input.RendererType, input.RendererConfig, descriptor, outputFields, selectedFields, input.QueryTemplate.OrderBy)
}

func controlSupportsNamedParameter(controlType string, fieldType datatype.FieldType) bool {
	switch fieldType {
	case datatype.FieldTypeBool:
		return controlType == "select" || controlType == "checkbox"
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt, datatype.FieldTypeFloat, datatype.FieldTypeDouble, datatype.FieldTypeDecimal:
		return controlType == "number"
	case datatype.FieldTypeDate:
		return controlType == "date"
	case datatype.FieldTypeTimestamp:
		return controlType == "datetime"
	default:
		return controlType == "text"
	}
}

func componentParameterFilterFromTemplate(template models.ComponentQueryTemplate, key string) (models.ComponentParameterFilter, bool) {
	for _, filter := range template.ParameterFilters {
		if filter.ParameterKey == key {
			return filter, true
		}
	}
	return models.ComponentParameterFilter{}, false
}

func componentNamedParameterBindingFromTemplate(template models.ComponentQueryTemplate, key string) (models.ComponentNamedParameterBinding, bool) {
	for _, binding := range template.NamedParameterBindings {
		if binding.ParameterKey == key {
			return binding, true
		}
	}
	return models.ComponentNamedParameterBinding{}, false
}

func validateRawNamedParameterValue(raw json.RawMessage, fieldType datatype.FieldType) error {
	return validateRawFilterValue(raw, models.ConsumerQueryField{Type: fieldType}, "eq", 1)
}

func validConsumerOperation(descriptor *models.ConsumerDescriptor) bool {
	if len(descriptor.Operations) != 1 {
		return false
	}
	operation := descriptor.Operations[0]
	path := strings.TrimSpace(operation.Path)
	return operation.Key == "query" && operation.Method == "POST" &&
		operation.InputKind == "structured_query" && operation.OutputKind == descriptor.OutputContract.Kind &&
		strings.HasPrefix(path, "/api/query/") && strings.HasSuffix(path, "/query") &&
		!strings.Contains(path, "//") && !strings.ContainsAny(path, "?#")
}

func validateFilter(filter *models.QueryFilter, descriptor *models.ConsumerDescriptor, fields map[string]models.ConsumerQueryField, depth int, nodes *int) error {
	if filter == nil {
		return nil
	}
	*nodes++
	if depth > descriptor.InputContract.Filter.MaxDepth || *nodes > descriptor.InputContract.Filter.MaxNodes {
		return fmt.Errorf("%w: filter exceeds descriptor limits", ErrInvalidComponentConfiguration)
	}
	leaf := strings.TrimSpace(filter.Field) != "" || strings.TrimSpace(filter.Op) != ""
	branches := 0
	if len(filter.And) > 0 {
		branches++
	}
	if len(filter.Or) > 0 {
		branches++
	}
	if filter.Not != nil {
		branches++
	}
	if (leaf && branches > 0) || (!leaf && branches != 1) {
		return fmt.Errorf("%w: invalid filter shape", ErrInvalidComponentConfiguration)
	}
	if leaf {
		field, exists := fields[filter.Field]
		if !exists || !field.Filterable || !contains(field.Operators, filter.Op) {
			return fmt.Errorf("%w: invalid filter field", ErrInvalidComponentConfiguration)
		}
		if err := validateFilterValue(filter.Value, field, filter.Op, descriptor.InputContract.Filter.MaxInValues); err != nil {
			return fmt.Errorf("%w: invalid filter value: %v", ErrInvalidComponentConfiguration, err)
		}
		return nil
	}
	for index := range filter.And {
		if err := validateFilter(&filter.And[index], descriptor, fields, depth+1, nodes); err != nil {
			return err
		}
	}
	for index := range filter.Or {
		if err := validateFilter(&filter.Or[index], descriptor, fields, depth+1, nodes); err != nil {
			return err
		}
	}
	return validateFilter(filter.Not, descriptor, fields, depth+1, nodes)
}

func validateRenderer(rendererType string, raw json.RawMessage, descriptor *models.ConsumerDescriptor, fields map[string]models.ConsumerOutputField, selectedFields map[string]struct{}, orderBy []models.QueryOrder) error {
	switch rendererType {
	case models.RendererTypeTable:
		var config models.TableRendererConfig
		if err := decodeStrict(raw, &config); err != nil || len(config.Columns) == 0 {
			return fmt.Errorf("%w: invalid table renderer", ErrInvalidComponentConfiguration)
		}
		for _, field := range config.Columns {
			if _, ok := fields[field]; !ok {
				return fmt.Errorf("%w: unknown table column", ErrInvalidComponentConfiguration)
			}
			if _, selected := selectedFields[field]; !selected {
				return fmt.Errorf("%w: table column is not selected", ErrInvalidComponentConfiguration)
			}
		}
		if err := validateFieldPresentations(config.FieldPresentations, config.Columns, fields, true); err != nil {
			return err
		}
	case models.RendererTypeChart:
		var config models.ChartRendererConfig
		if err := decodeStrict(raw, &config); err != nil || !contains([]string{"bar", "line", "pie"}, config.ChartType) || len(config.Measures) == 0 || len(config.Measures) > 5 || (config.ChartType == "pie" && len(config.Measures) != 1) {
			return fmt.Errorf("%w: invalid chart renderer", ErrInvalidComponentConfiguration)
		}
		if _, ok := fields[config.Dimension]; !ok {
			return fmt.Errorf("%w: unknown chart dimension", ErrInvalidComponentConfiguration)
		}
		if _, selected := selectedFields[config.Dimension]; !selected {
			return fmt.Errorf("%w: chart dimension is not selected", ErrInvalidComponentConfiguration)
		}
		seenMeasures := make(map[string]struct{}, len(config.Measures))
		for _, field := range config.Measures {
			outputField, ok := fields[field]
			if !ok || !datatype.IsNumericFieldType(outputField.Type) {
				return fmt.Errorf("%w: unknown chart measure", ErrInvalidComponentConfiguration)
			}
			if _, duplicate := seenMeasures[field]; duplicate {
				return fmt.Errorf("%w: duplicate chart measure", ErrInvalidComponentConfiguration)
			}
			seenMeasures[field] = struct{}{}
			if _, selected := selectedFields[field]; !selected {
				return fmt.Errorf("%w: chart measure is not selected", ErrInvalidComponentConfiguration)
			}
		}
		if config.ChartType == "line" && !orderContainsField(orderBy, config.Dimension) {
			return fmt.Errorf("%w: line chart dimension must be ordered", ErrInvalidComponentConfiguration)
		}
		if err := validateFieldPresentations(config.FieldPresentations, append([]string{config.Dimension}, config.Measures...), fields, false); err != nil {
			return err
		}
	case models.RendererTypeMap:
		var config models.MapRendererConfig
		if err := decodeStrict(raw, &config); err != nil || descriptor.OutputContract.Spatial == nil || config.GeometryField != descriptor.OutputContract.Spatial.PrimaryGeometryField {
			return fmt.Errorf("%w: invalid map renderer", ErrInvalidComponentConfiguration)
		}
		if _, selected := selectedFields[config.GeometryField]; !selected {
			return fmt.Errorf("%w: map geometry is not selected", ErrInvalidComponentConfiguration)
		}
		if config.LabelField != "" {
			if _, ok := fields[config.LabelField]; !ok {
				return fmt.Errorf("%w: unknown map label field", ErrInvalidComponentConfiguration)
			}
			if _, selected := selectedFields[config.LabelField]; !selected {
				return fmt.Errorf("%w: map label field is not selected", ErrInvalidComponentConfiguration)
			}
		}
		for _, field := range config.TooltipFields {
			if _, ok := fields[field]; !ok {
				return fmt.Errorf("%w: unknown map tooltip field", ErrInvalidComponentConfiguration)
			}
			if _, selected := selectedFields[field]; !selected {
				return fmt.Errorf("%w: map tooltip field is not selected", ErrInvalidComponentConfiguration)
			}
		}
		if config.Style != nil {
			style := config.Style
			if !contains([]string{"uniform", "categorical", "continuous"}, style.Mode) || !contains([]string{"primary", "success", "warning", "danger"}, style.Palette) || len([]rune(style.LegendTitle)) > 100 {
				return fmt.Errorf("%w: invalid map style", ErrInvalidComponentConfiguration)
			}
			if style.Mode == "uniform" {
				if style.Field != "" {
					return fmt.Errorf("%w: uniform map style cannot use a field", ErrInvalidComponentConfiguration)
				}
			} else {
				field, ok := fields[style.Field]
				if !ok || !isMapThematicFieldType(field.Type) || (style.Mode == "continuous" && !datatype.IsNumericFieldType(field.Type)) {
					return fmt.Errorf("%w: invalid map style field", ErrInvalidComponentConfiguration)
				}
				if _, selected := selectedFields[style.Field]; !selected {
					return fmt.Errorf("%w: map style field is not selected", ErrInvalidComponentConfiguration)
				}
			}
		}
		presentationFields := append([]string{}, config.TooltipFields...)
		if config.LabelField != "" {
			presentationFields = append(presentationFields, config.LabelField)
		}
		if config.Style != nil && config.Style.Field != "" {
			presentationFields = append(presentationFields, config.Style.Field)
		}
		if err := validateFieldPresentations(config.FieldPresentations, presentationFields, fields, false); err != nil {
			return err
		}
	case models.RendererTypeValue:
		var config models.ValueRendererConfig
		if err := decodeStrict(raw, &config); err != nil || len(config.Items) == 0 || len(config.Items) > 4 {
			return fmt.Errorf("%w: invalid value renderer", ErrInvalidComponentConfiguration)
		}
		seenFields := make(map[string]struct{}, len(config.Items))
		for _, item := range config.Items {
			field, ok := fields[item.Field]
			if !ok || !datatype.IsNumericFieldType(field.Type) || strings.TrimSpace(item.Label) == "" || len([]rune(item.Label)) > 100 || len([]rune(item.Unit)) > 30 || item.Precision < 0 || item.Precision > 8 {
				return fmt.Errorf("%w: invalid value item", ErrInvalidComponentConfiguration)
			}
			if _, duplicate := seenFields[item.Field]; duplicate {
				return fmt.Errorf("%w: duplicate value field", ErrInvalidComponentConfiguration)
			}
			seenFields[item.Field] = struct{}{}
			if _, selected := selectedFields[item.Field]; !selected {
				return fmt.Errorf("%w: value field is not selected", ErrInvalidComponentConfiguration)
			}
		}
	default:
		return fmt.Errorf("%w: unknown renderer", ErrInvalidComponentConfiguration)
	}
	return nil
}

func validateFieldPresentations(presentations []models.FieldPresentation, allowed []string, fields map[string]models.ConsumerOutputField, allowWidth bool) error {
	allowedFields := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		if field != "" {
			allowedFields[field] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(presentations))
	for _, presentation := range presentations {
		field, exists := fields[presentation.Field]
		_, isAllowed := allowedFields[presentation.Field]
		_, duplicate := seen[presentation.Field]
		if !exists || !isAllowed || duplicate || strings.TrimSpace(presentation.Label) == "" || len([]rune(presentation.Label)) > 100 || len([]rune(presentation.Unit)) > 30 {
			return fmt.Errorf("%w: invalid field presentation", ErrInvalidComponentConfiguration)
		}
		seen[presentation.Field] = struct{}{}
		if presentation.Precision != nil && (*presentation.Precision < 0 || *presentation.Precision > 8 || !datatype.IsNumericFieldType(field.Type)) {
			return fmt.Errorf("%w: invalid field presentation precision", ErrInvalidComponentConfiguration)
		}
		if presentation.Unit != "" && !datatype.IsNumericFieldType(field.Type) {
			return fmt.Errorf("%w: invalid field presentation unit", ErrInvalidComponentConfiguration)
		}
		if !validTemporalPresentation(field.Type, presentation.TemporalFormat) {
			return fmt.Errorf("%w: invalid field presentation temporal format", ErrInvalidComponentConfiguration)
		}
		if presentation.Width != nil && (!allowWidth || *presentation.Width < 80 || *presentation.Width > 600) {
			return fmt.Errorf("%w: invalid field presentation width", ErrInvalidComponentConfiguration)
		}
	}
	return nil
}

func validTemporalPresentation(fieldType datatype.FieldType, format string) bool {
	if format == "" {
		return true
	}
	switch fieldType {
	case datatype.FieldTypeDate:
		return format == "date"
	case datatype.FieldTypeTime:
		return format == "time"
	case datatype.FieldTypeTimestamp:
		return format == "date" || format == "datetime"
	default:
		return false
	}
}

func orderContainsField(orderBy []models.QueryOrder, field string) bool {
	for _, order := range orderBy {
		if order.Field == field {
			return true
		}
	}
	return false
}

func isMapThematicFieldType(fieldType datatype.FieldType) bool {
	switch fieldType {
	case datatype.FieldTypeString, datatype.FieldTypeBool, datatype.FieldTypeInt, datatype.FieldTypeBigInt,
		datatype.FieldTypeFloat, datatype.FieldTypeDouble, datatype.FieldTypeDecimal, datatype.FieldTypeDate,
		datatype.FieldTypeTime, datatype.FieldTypeTimestamp, datatype.FieldTypeUUID:
		return true
	default:
		return false
	}
}

func validateRawFilterValue(raw json.RawMessage, field models.ConsumerQueryField, operator string, maxInValues int) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("multiple JSON values")
	}
	return validateFilterValue(value, field, operator, maxInValues)
}

func validateFilterValue(value interface{}, field models.ConsumerQueryField, operator string, maxInValues int) error {
	switch operator {
	case "is_null", "is_not_null":
		if value != nil {
			if enabled, ok := value.(bool); !ok || !enabled {
				return fmt.Errorf("unary parameter default must be true")
			}
		}
		return nil
	case "in":
		values, ok := value.([]interface{})
		if !ok || len(values) == 0 || len(values) > maxInValues {
			return fmt.Errorf("in requires 1 to %d values", maxInValues)
		}
		for _, item := range values {
			if err := validateScalarValue(item, field.Type); err != nil {
				return err
			}
		}
		return nil
	case "bbox_intersects":
		values, ok := value.([]interface{})
		if !ok || len(values) != 4 {
			return fmt.Errorf("bbox requires four coordinates")
		}
		coordinates := make([]float64, 4)
		for index, item := range values {
			number, ok := floatValue(item)
			if !ok {
				return fmt.Errorf("bbox coordinates must be numeric")
			}
			coordinates[index] = number
		}
		if coordinates[0] > coordinates[2] || coordinates[1] > coordinates[3] {
			return fmt.Errorf("bbox bounds are invalid")
		}
		return nil
	default:
		if value == nil {
			return fmt.Errorf("operator %s requires a value", operator)
		}
		return validateScalarValue(value, field.Type)
	}
}

func validateScalarValue(value interface{}, fieldType datatype.FieldType) error {
	if datatype.IsNumericFieldType(fieldType) {
		number, ok := floatValue(value)
		if !ok {
			return fmt.Errorf("field %s requires a numeric value", fieldType)
		}
		if (fieldType == datatype.FieldTypeInt || fieldType == datatype.FieldTypeBigInt) && math.Trunc(number) != number {
			return fmt.Errorf("field %s requires an integer value", fieldType)
		}
		return nil
	}
	switch fieldType {
	case datatype.FieldTypeString, datatype.FieldTypeDate, datatype.FieldTypeTime, datatype.FieldTypeTimestamp, datatype.FieldTypeUUID:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("field %s requires a string value", fieldType)
		}
	case datatype.FieldTypeBool:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("bool field requires a boolean value")
		}
	default:
		return fmt.Errorf("field type %s does not support parameter values", fieldType)
	}
	return nil
}

func floatValue(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil && !math.IsInf(parsed, 0) && !math.IsNaN(parsed)
	case float64:
		return typed, !math.IsInf(typed, 0) && !math.IsNaN(typed)
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func decodeStrict(raw json.RawMessage, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}

func allowedControlType(value string) bool {
	return contains([]string{"text", "number", "checkbox", "date", "datetime", "select", "multiselect", "bbox"}, value)
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
