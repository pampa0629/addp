package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/addp/common/datatype"
	commonquery "github.com/addp/common/query"
	"github.com/addp/service/internal/models"
)

var ErrInvalidStructuredQuery = errors.New("invalid structured query")

type queryProtocol string

const (
	queryProtocolREST queryProtocol = "rest"
	queryProtocolOGC  queryProtocol = "ogc_features"
)

type compiledQueryPlan struct {
	SQL            string
	Args           []interface{}
	Limit          int
	SelectedFields []string
	HiddenFields   []string
	OrderBy        []models.QueryOrder
	QueryHash      string
	ServiceVersion string
}

func compileQueryPlan(
	service *models.QueryService,
	request *models.QueryExecutionRequest,
	protocol queryProtocol,
	engineType string,
	baseSQL string,
	baseArgs []interface{},
	parameters map[string]interface{},
	codec *queryTokenCodec,
) (*compiledQueryPlan, error) {
	if service == nil || request == nil || codec == nil {
		return nil, fmt.Errorf("%w: query service request is incomplete", ErrInvalidStructuredQuery)
	}
	table := service.GetTableInfo()
	if table == nil || len(table.Fields) == 0 {
		return nil, fmt.Errorf("%w: published output contract has no fields", ErrInvalidStructuredQuery)
	}
	version := serviceDependencyVersion(service)
	if version == "" {
		return nil, fmt.Errorf("%w: published service version is missing", ErrInvalidStructuredQuery)
	}
	stableKey := service.GetStableKey()
	if _, err := validateStableKey(stableKey, table); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidStructuredQuery, err)
	}

	fields := make(map[string]datatype.FieldInfo, len(table.Fields))
	for _, field := range table.Fields {
		if strings.TrimSpace(field.Name) == "" {
			return nil, fmt.Errorf("%w: output contract contains an empty field", ErrInvalidStructuredQuery)
		}
		if _, duplicate := fields[field.Name]; duplicate {
			return nil, fmt.Errorf("%w: output contract contains duplicate field %s", ErrInvalidStructuredQuery, field.Name)
		}
		fields[field.Name] = field
	}
	selected, err := selectedQueryFields(service, request.Select, fields, table.FieldNames())
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(strings.TrimSpace(request.Format), "geojson") {
		geometryColumn := service.GetGeometryColumn()
		if geometryColumn == "" {
			return nil, fmt.Errorf("%w: geojson format requires a primary geometry field", ErrInvalidStructuredQuery)
		}
		if !containsQueryField(selected, geometryColumn) {
			selected = append(selected, geometryColumn)
		}
	}
	orderBy, err := effectiveQueryOrder(request.OrderBy, stableKey, fields)
	if err != nil {
		return nil, err
	}
	limit := request.Page.Limit
	if limit < 0 || limit > 10000 {
		return nil, fmt.Errorf("%w: page.limit must be between 1 and 10000", ErrInvalidStructuredQuery)
	}
	if limit == 0 {
		limit = 50
	}
	maxFeatures := service.MaxFeatures
	if maxFeatures <= 0 {
		maxFeatures = 1000
	}
	if limit > maxFeatures {
		limit = maxFeatures
	}

	queryHash, err := structuredQueryHash(parameters, selected, request.Filter, orderBy)
	if err != nil {
		return nil, err
	}
	var cursorValues []interface{}
	if strings.TrimSpace(request.Page.Cursor) != "" {
		payload, decodeErr := codec.decodeCursor(request.Page.Cursor)
		if decodeErr != nil || payload.ServiceID != service.ID || payload.ServiceVersion != version ||
			payload.QueryHash != queryHash || !reflect.DeepEqual(payload.OrderBy, orderBy) || len(payload.Values) != len(orderBy) {
			return nil, ErrInvalidQueryCursor
		}
		cursorValues = payload.Values
	}

	dialect := commonquery.ForEngine(engineType)
	hidden := hiddenOrderFields(selected, orderBy)
	selectFields := append(append([]string(nil), selected...), hidden...)
	selectSQL := make([]string, 0, len(selectFields))
	for _, name := range selectFields {
		field := fields[name]
		qualified := "addp_source." + dialect.QuoteIdentifier(name)
		if supportsGeoJSONProjection(engineType) && datatype.IsSpatialFieldType(field.Type) {
			selectSQL = append(selectSQL, fmt.Sprintf("ST_AsGeoJSON(%s) AS %s", qualified, dialect.QuoteIdentifier(name)))
			continue
		}
		selectSQL = append(selectSQL, qualified)
	}

	args := append([]interface{}(nil), baseArgs...)
	whereParts := make([]string, 0, 2)
	if request.Filter != nil {
		filterSQL, filterErr := compileFilter(request.Filter, service, protocol, engineType, fields, &args)
		if filterErr != nil {
			return nil, filterErr
		}
		whereParts = append(whereParts, filterSQL)
	}
	if len(cursorValues) > 0 {
		cursorSQL, cursorErr := compileCursorPredicate(orderBy, cursorValues, fields, dialect, &args)
		if cursorErr != nil {
			return nil, cursorErr
		}
		whereParts = append(whereParts, cursorSQL)
	}

	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(baseSQL), ";"))
	if inner == "" {
		return nil, fmt.Errorf("%w: source query is empty", ErrInvalidStructuredQuery)
	}
	var builder strings.Builder
	builder.WriteString("SELECT ")
	builder.WriteString(strings.Join(selectSQL, ", "))
	builder.WriteString(" FROM (")
	builder.WriteString(inner)
	builder.WriteString(")")
	builder.WriteString(dialect.SubqueryAlias("addp_source"))
	if len(whereParts) > 0 {
		builder.WriteString(" WHERE ")
		builder.WriteString(strings.Join(whereParts, " AND "))
	}
	builder.WriteString(" ORDER BY ")
	orderParts := make([]string, len(orderBy))
	for index, order := range orderBy {
		orderParts[index] = "addp_source." + dialect.QuoteIdentifier(order.Field) + " " + strings.ToUpper(order.Direction)
	}
	builder.WriteString(strings.Join(orderParts, ", "))
	plannedSQL := dialect.AppendPaginationSQL(builder.String(), limit+1, 0)

	return &compiledQueryPlan{
		SQL: plannedSQL, Args: args, Limit: limit,
		SelectedFields: selected, HiddenFields: hidden, OrderBy: orderBy,
		QueryHash: queryHash, ServiceVersion: version,
	}, nil
}

func containsQueryField(fields []string, required string) bool {
	for _, field := range fields {
		if field == required {
			return true
		}
	}
	return false
}

func selectedQueryFields(service *models.QueryService, requested []string, fields map[string]datatype.FieldInfo, all []string) ([]string, error) {
	selected := requested
	if len(selected) == 0 {
		selected = service.GetDefaultFields()
	}
	if len(selected) == 0 {
		selected = all
	}
	result := make([]string, 0, len(selected))
	seen := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		name = strings.TrimSpace(name)
		if _, exists := fields[name]; !exists {
			return nil, fmt.Errorf("%w: selected field %s is not in the output contract", ErrInvalidStructuredQuery, name)
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("%w: selected field %s is duplicated", ErrInvalidStructuredQuery, name)
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result, nil
}

func effectiveQueryOrder(requested []models.QueryOrder, stableKey []string, fields map[string]datatype.FieldInfo) ([]models.QueryOrder, error) {
	result := make([]models.QueryOrder, 0, len(requested)+len(stableKey))
	seen := make(map[string]struct{}, len(requested)+len(stableKey))
	for _, order := range requested {
		order.Field = strings.TrimSpace(order.Field)
		order.Direction = strings.ToLower(strings.TrimSpace(order.Direction))
		field, exists := fields[order.Field]
		if !exists || (order.Direction != "asc" && order.Direction != "desc") {
			return nil, fmt.Errorf("%w: invalid order_by field or direction", ErrInvalidStructuredQuery)
		}
		if field.Nullable || !isStableOrderFieldType(field.Type) {
			return nil, fmt.Errorf("%w: order_by field %s is not a non-null scalar", ErrInvalidStructuredQuery, order.Field)
		}
		if _, duplicate := seen[order.Field]; duplicate {
			return nil, fmt.Errorf("%w: order_by field %s is duplicated", ErrInvalidStructuredQuery, order.Field)
		}
		seen[order.Field] = struct{}{}
		result = append(result, order)
	}
	for _, field := range stableKey {
		if _, exists := seen[field]; exists {
			continue
		}
		result = append(result, models.QueryOrder{Field: field, Direction: "asc"})
	}
	return result, nil
}

func isStableOrderFieldType(fieldType datatype.FieldType) bool {
	switch fieldType {
	case datatype.FieldTypeString, datatype.FieldTypeBool,
		datatype.FieldTypeInt, datatype.FieldTypeBigInt, datatype.FieldTypeFloat,
		datatype.FieldTypeDouble, datatype.FieldTypeDecimal,
		datatype.FieldTypeDate, datatype.FieldTypeTime, datatype.FieldTypeTimestamp,
		datatype.FieldTypeUUID:
		return true
	default:
		return false
	}
}

func hiddenOrderFields(selected []string, orderBy []models.QueryOrder) []string {
	visible := make(map[string]struct{}, len(selected))
	for _, field := range selected {
		visible[field] = struct{}{}
	}
	hidden := make([]string, 0)
	for _, order := range orderBy {
		if _, exists := visible[order.Field]; !exists {
			hidden = append(hidden, order.Field)
			visible[order.Field] = struct{}{}
		}
	}
	return hidden
}

func compileFilter(filter *models.QueryFilter, service *models.QueryService, protocol queryProtocol, engineType string, fields map[string]datatype.FieldInfo, args *[]interface{}) (string, error) {
	nodes := 0
	return compileFilterNode(filter, service, protocol, engineType, fields, args, 0, &nodes)
}

func compileFilterNode(filter *models.QueryFilter, service *models.QueryService, protocol queryProtocol, engineType string, fields map[string]datatype.FieldInfo, args *[]interface{}, depth int, nodes *int) (string, error) {
	if filter == nil {
		return "", fmt.Errorf("%w: filter is nil", ErrInvalidStructuredQuery)
	}
	*nodes++
	if depth > 16 || *nodes > 256 {
		return "", fmt.Errorf("%w: filter tree is too complex", ErrInvalidStructuredQuery)
	}
	nodeCount := 0
	if strings.TrimSpace(filter.Field) != "" || strings.TrimSpace(filter.Op) != "" {
		nodeCount++
	}
	if len(filter.And) > 0 {
		nodeCount++
	}
	if len(filter.Or) > 0 {
		nodeCount++
	}
	if filter.Not != nil {
		nodeCount++
	}
	if nodeCount != 1 {
		return "", fmt.Errorf("%w: each filter node must contain exactly one expression", ErrInvalidStructuredQuery)
	}
	if len(filter.And) > 0 || len(filter.Or) > 0 {
		children := filter.And
		joiner := " AND "
		if len(filter.Or) > 0 {
			children = filter.Or
			joiner = " OR "
		}
		parts := make([]string, len(children))
		for index := range children {
			part, err := compileFilterNode(&children[index], service, protocol, engineType, fields, args, depth+1, nodes)
			if err != nil {
				return "", err
			}
			parts[index] = part
		}
		return "(" + strings.Join(parts, joiner) + ")", nil
	}
	if filter.Not != nil {
		part, err := compileFilterNode(filter.Not, service, protocol, engineType, fields, args, depth+1, nodes)
		if err != nil {
			return "", err
		}
		return "(NOT " + part + ")", nil
	}

	field, exists := fields[strings.TrimSpace(filter.Field)]
	if !exists || !filterFieldAllowed(service, protocol, field.Name, strings.ToLower(strings.TrimSpace(filter.Op))) {
		return "", fmt.Errorf("%w: field %s is not filterable", ErrInvalidStructuredQuery, filter.Field)
	}
	dialect := commonquery.ForEngine(engineType)
	column := "addp_source." + dialect.QuoteIdentifier(field.Name)
	op := strings.ToLower(strings.TrimSpace(filter.Op))
	switch op {
	case "eq", "ne", "lt", "lte", "gt", "gte":
		if filter.Value == nil {
			return "", fmt.Errorf("%w: operator %s requires a value", ErrInvalidStructuredQuery, op)
		}
		if !filterComparisonAllowed(field.Type, op) {
			return "", fmt.Errorf("%w: operator %s is not valid for field %s", ErrInvalidStructuredQuery, op, field.Name)
		}
		value, err := normalizeBoundValue(filter.Value, field.Type)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrInvalidStructuredQuery, err)
		}
		operators := map[string]string{"eq": "=", "ne": "<>", "lt": "<", "lte": "<=", "gt": ">", "gte": ">="}
		*args = append(*args, value)
		return "(" + column + " " + operators[op] + " ?)", nil
	case "in":
		values, ok := interfaceSlice(filter.Value)
		if !ok || len(values) == 0 || len(values) > 1000 {
			return "", fmt.Errorf("%w: in requires 1 to 1000 values", ErrInvalidStructuredQuery)
		}
		placeholders := make([]string, len(values))
		for index, value := range values {
			if value == nil {
				return "", fmt.Errorf("%w: in values must not contain null", ErrInvalidStructuredQuery)
			}
			normalized, err := normalizeBoundValue(value, field.Type)
			if err != nil || !isStableOrderFieldType(field.Type) {
				return "", fmt.Errorf("%w: invalid in value for field %s", ErrInvalidStructuredQuery, field.Name)
			}
			placeholders[index] = "?"
			*args = append(*args, normalized)
		}
		return "(" + column + " IN (" + strings.Join(placeholders, ", ") + "))", nil
	case "is_null":
		return "(" + column + " IS NULL)", nil
	case "is_not_null":
		return "(" + column + " IS NOT NULL)", nil
	case "bbox_intersects":
		return compileBBoxFilter(filter.Value, service, protocol, engineType, field, column, args)
	default:
		return "", fmt.Errorf("%w: unsupported filter operator %s", ErrInvalidStructuredQuery, op)
	}
}

func filterComparisonAllowed(fieldType datatype.FieldType, operator string) bool {
	if !isStableOrderFieldType(fieldType) {
		return false
	}
	if (operator == "lt" || operator == "lte" || operator == "gt" || operator == "gte") && fieldType == datatype.FieldTypeBool {
		return false
	}
	return true
}

func normalizeBoundValue(value interface{}, fieldType datatype.FieldType) (interface{}, error) {
	if datatype.IsNumericFieldType(fieldType) {
		switch typed := value.(type) {
		case json.Number:
			if fieldType == datatype.FieldTypeInt || fieldType == datatype.FieldTypeBigInt {
				return typed.Int64()
			}
			number, err := typed.Float64()
			if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
				return nil, fmt.Errorf("field %s requires a finite numeric value", fieldType)
			}
			return number, nil
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32:
			return value, nil
		case float32:
			number := float64(typed)
			if math.IsInf(number, 0) || math.IsNaN(number) || ((fieldType == datatype.FieldTypeInt || fieldType == datatype.FieldTypeBigInt) && math.Trunc(number) != number) {
				return nil, fmt.Errorf("field %s requires a valid numeric value", fieldType)
			}
			return typed, nil
		case float64:
			if math.IsInf(typed, 0) || math.IsNaN(typed) || ((fieldType == datatype.FieldTypeInt || fieldType == datatype.FieldTypeBigInt) && math.Trunc(typed) != typed) {
				return nil, fmt.Errorf("field %s requires a valid numeric value", fieldType)
			}
			return typed, nil
		default:
			return nil, fmt.Errorf("field %s requires a numeric value", fieldType)
		}
	}
	switch fieldType {
	case datatype.FieldTypeString, datatype.FieldTypeDate, datatype.FieldTypeTime, datatype.FieldTypeTimestamp, datatype.FieldTypeUUID:
		if _, ok := value.(string); !ok {
			return nil, fmt.Errorf("field %s requires a string value", fieldType)
		}
	case datatype.FieldTypeBool:
		if _, ok := value.(bool); !ok {
			return nil, errors.New("bool field requires a boolean value")
		}
	default:
		return nil, fmt.Errorf("field type %s does not support bound comparisons", fieldType)
	}
	return value, nil
}

func filterFieldAllowed(service *models.QueryService, protocol queryProtocol, field, operator string) bool {
	if protocol == queryProtocolOGC {
		if operator == "bbox_intersects" && field == service.GetGeometryColumn() {
			return true
		}
		for _, stableField := range service.GetStableKey() {
			if field == stableField && operator == "eq" {
				return true
			}
		}
	}
	for _, allowed := range service.GetFilterableFields() {
		if field == allowed {
			return true
		}
	}
	return false
}

func compileBBoxFilter(value interface{}, service *models.QueryService, protocol queryProtocol, engineType string, field datatype.FieldInfo, column string, args *[]interface{}) (string, error) {
	if !datatype.IsSpatialFieldType(field.Type) || field.Name != service.GetGeometryColumn() {
		return "", fmt.Errorf("%w: bbox_intersects requires the primary geometry field", ErrInvalidStructuredQuery)
	}
	values, ok := interfaceSlice(value)
	if !ok || len(values) != 4 {
		return "", fmt.Errorf("%w: bbox_intersects requires four coordinates", ErrInvalidStructuredQuery)
	}
	coordinates := make([]float64, 4)
	for index, value := range values {
		number, ok := numericValue(value)
		if !ok {
			return "", fmt.Errorf("%w: bbox_intersects coordinates must be numeric", ErrInvalidStructuredQuery)
		}
		coordinates[index] = number
	}
	if coordinates[0] > coordinates[2] || coordinates[1] > coordinates[3] {
		return "", fmt.Errorf("%w: bbox_intersects bounds are invalid", ErrInvalidStructuredQuery)
	}
	srid := service.GetSRID()
	switch strings.ToLower(strings.TrimSpace(engineType)) {
	case "postgresql":
		*args = append(*args, coordinates[0], coordinates[1], coordinates[2], coordinates[3])
		if protocol == queryProtocolOGC && srid > 0 && srid != 4326 {
			*args = append(*args, srid)
			return fmt.Sprintf("(ST_Intersects(%s, ST_Transform(ST_MakeEnvelope(?, ?, ?, ?, 4326), ?)))", column), nil
		}
		if srid <= 0 {
			srid = 4326
		}
		*args = append(*args, srid)
		return fmt.Sprintf("(ST_Intersects(%s, ST_MakeEnvelope(?, ?, ?, ?, ?)))", column), nil
	case "duckdb":
		*args = append(*args, coordinates[0], coordinates[1], coordinates[2], coordinates[3])
		envelope := "ST_MakeEnvelope(?, ?, ?, ?)"
		if protocol == queryProtocolOGC && srid > 0 && srid != 4326 {
			envelope = fmt.Sprintf("ST_Transform(%s, 'EPSG:4326', 'EPSG:%d', always_xy := true)", envelope, srid)
		}
		return fmt.Sprintf("(ST_Intersects(%s, %s))", column, envelope), nil
	case "mysql":
		*args = append(*args, coordinates[0], coordinates[1], coordinates[2], coordinates[3])
		envelopeSRID := 4326
		if protocol != queryProtocolOGC && srid > 0 {
			envelopeSRID = srid
		}
		*args = append(*args, envelopeSRID)
		envelope := "ST_Envelope(ST_GeomFromText(CONCAT('LINESTRING(', ?, ' ', ?, ',', ?, ' ', ?, ')'), ?))"
		if protocol == queryProtocolOGC && srid > 0 && srid != 4326 {
			envelope = fmt.Sprintf("ST_Transform(%s, %d)", envelope, srid)
		}
		return fmt.Sprintf("(ST_Intersects(%s, %s))", column, envelope), nil
	default:
		return "", fmt.Errorf("%w: bbox_intersects is not supported by the selected runtime", ErrInvalidStructuredQuery)
	}
}

func supportsGeoJSONProjection(engineType string) bool {
	switch strings.ToLower(strings.TrimSpace(engineType)) {
	case "postgresql", "mysql", "duckdb":
		return true
	default:
		return false
	}
}

func compileCursorPredicate(orderBy []models.QueryOrder, values []interface{}, fields map[string]datatype.FieldInfo, dialect commonquery.Dialect, args *[]interface{}) (string, error) {
	parts := make([]string, len(orderBy))
	for index, order := range orderBy {
		field := fields[order.Field]
		value, err := normalizeCursorValue(values[index], field.Type)
		if err != nil || value == nil {
			return "", ErrInvalidQueryCursor
		}
		conditions := make([]string, 0, index+1)
		for previous := 0; previous < index; previous++ {
			previousField := fields[orderBy[previous].Field]
			previousValue, normalizeErr := normalizeCursorValue(values[previous], previousField.Type)
			if normalizeErr != nil || previousValue == nil {
				return "", ErrInvalidQueryCursor
			}
			conditions = append(conditions, "addp_source."+dialect.QuoteIdentifier(orderBy[previous].Field)+" = ?")
			*args = append(*args, previousValue)
		}
		operator := ">"
		if order.Direction == "desc" {
			operator = "<"
		}
		conditions = append(conditions, "addp_source."+dialect.QuoteIdentifier(order.Field)+" "+operator+" ?")
		*args = append(*args, value)
		parts[index] = "(" + strings.Join(conditions, " AND ") + ")"
	}
	return "(" + strings.Join(parts, " OR ") + ")", nil
}

func structuredQueryHash(parameters map[string]interface{}, selected []string, filter *models.QueryFilter, orderBy []models.QueryOrder) (string, error) {
	payload := struct {
		Parameters map[string]interface{} `json:"parameters,omitempty"`
		Select     []string               `json:"select"`
		Filter     *models.QueryFilter    `json:"filter,omitempty"`
		OrderBy    []models.QueryOrder    `json:"order_by"`
	}{Parameters: parameters, Select: selected, Filter: filter, OrderBy: orderBy}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func serviceDependencyVersion(service *models.QueryService) string {
	if snapshot := service.SourceSnapshot(); snapshot != nil {
		if snapshot.DependencyHash == "" || len(service.GetStableKey()) == 0 {
			return ""
		}
		return hashString(snapshot.DependencyHash + "\x00" + strings.Join(service.GetStableKey(), "\x00"))
	}
	return ""
}

func interfaceSlice(value interface{}) ([]interface{}, bool) {
	switch typed := value.(type) {
	case []interface{}:
		return typed, true
	case []string:
		result := make([]interface{}, len(typed))
		for index := range typed {
			result[index] = typed[index]
		}
		return result, true
	default:
		return nil, false
	}
}

func numericValue(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func normalizeCursorValue(value interface{}, fieldType datatype.FieldType) (interface{}, error) {
	number, isNumber := value.(json.Number)
	if !isNumber {
		return value, nil
	}
	if datatype.IsNumericFieldType(fieldType) {
		if fieldType == datatype.FieldTypeInt || fieldType == datatype.FieldTypeBigInt {
			return number.Int64()
		}
		return number.Float64()
	}
	return number.String(), nil
}
