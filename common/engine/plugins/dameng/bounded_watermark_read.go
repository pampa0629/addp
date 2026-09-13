package dameng

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

type boundedWatermarkSession struct {
	db           *sql.DB
	tx           *sql.Tx
	rows         *sql.Rows
	columns      []string
	fields       []datatype.FieldInfo
	upper        *plugin.WatermarkCursor
	cursorFields []string
	offset       int64
	exhausted    bool
	closed       bool
}

func (p *Plugin) OpenBoundedWatermarkRead(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BoundedWatermarkReadOptions) (plugin.BoundedWatermarkReadSession, error) {
	schema, table, err := tablePathParts(path)
	if err != nil {
		return nil, err
	}
	cursorNames, err := plugin.NormalizeWatermarkFields(opts.WatermarkField, opts.TieBreakers)
	if err != nil {
		return nil, err
	}
	fields, err := p.listColumns(ctx, connInfo, schema, table)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]datatype.FieldInfo, len(fields))
	cursorFields := make([]datatype.FieldInfo, 0, len(cursorNames))
	for _, field := range fields {
		byName[field.Name] = field
	}
	for _, name := range cursorNames {
		field, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("DM8 watermark cursor field %q does not exist", name)
		}
		if field.Nullable {
			return nil, fmt.Errorf("DM8 watermark cursor field %q must be NOT NULL", name)
		}
		if !watermarkFieldTypeSupported(field.Type) {
			return nil, fmt.Errorf("DM8 watermark cursor field %q has unsupported type %q", name, field.Type)
		}
		cursorFields = append(cursorFields, field)
	}
	identity := opts.TieBreakers
	if len(identity) == 0 {
		identity = cursorNames
	}
	if err := p.validateUniqueFields(ctx, connInfo, path, identity); err != nil {
		return nil, fmt.Errorf("DM8 watermark identity: %w", err)
	}
	if opts.Start != nil && len(opts.Start.Values) != len(cursorFields) {
		return nil, fmt.Errorf("DM8 watermark start cursor has %d values, want %d", len(opts.Start.Values), len(cursorFields))
	}
	db, err := p.openDB(connInfo)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (plugin.BoundedWatermarkReadSession, error) {
		_ = db.Close()
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fail(fmt.Errorf("begin DM8 watermark snapshot: %w", err))
	}
	failTx := func(err error) (plugin.BoundedWatermarkReadSession, error) {
		_ = tx.Rollback()
		_ = db.Close()
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "SET TRANSACTION READ ONLY"); err != nil {
		return failTx(fmt.Errorf("set DM8 watermark transaction read-only: %w", err))
	}
	dialect := commonquery.ForDialect(p.SQLDialect())
	qualified := dialect.QualifiedTable(schema, table)
	quotedCursor := quoteFields(dialect, cursorNames)
	upperQuery := "SELECT " + strings.Join(quotedCursor, ", ") + " FROM " + qualified + " ORDER BY " + orderFields(quotedCursor, "DESC") + " FETCH FIRST 1 ROWS ONLY"
	upperValues := make([]interface{}, len(cursorFields))
	upperPointers := make([]interface{}, len(cursorFields))
	for index := range upperValues {
		upperPointers[index] = &upperValues[index]
	}
	var upper *plugin.WatermarkCursor
	if err := tx.QueryRowContext(ctx, upperQuery).Scan(upperPointers...); err != nil {
		if err != sql.ErrNoRows {
			return failTx(fmt.Errorf("freeze DM8 watermark upper bound: %w", err))
		}
	} else {
		upper = &plugin.WatermarkCursor{Values: stringifyCursor(upperValues)}
	}
	statement := "SELECT * FROM " + qualified
	predicates := make([]string, 0, 2)
	args := make([]interface{}, 0)
	if upper == nil {
		predicates = append(predicates, "1 = 0")
	} else {
		if opts.Start != nil {
			predicate, values, err := cursorPredicate(dialect, cursorFields, opts.Start.Values, ">")
			if err != nil {
				return failTx(err)
			}
			predicates = append(predicates, predicate)
			args = append(args, values...)
		}
		predicate, values, err := cursorPredicate(dialect, cursorFields, upper.Values, "<=")
		if err != nil {
			return failTx(err)
		}
		predicates = append(predicates, predicate)
		args = append(args, values...)
	}
	statement += " WHERE " + strings.Join(predicates, " AND ") + " ORDER BY " + strings.Join(quotedCursor, ", ")
	rows, err := tx.QueryContext(ctx, statement, args...)
	if err != nil {
		return failTx(fmt.Errorf("open DM8 watermark cursor: %w", err))
	}
	columns, err := rows.Columns()
	if err != nil {
		rows.Close()
		return failTx(fmt.Errorf("read DM8 watermark columns: %w", err))
	}
	return &boundedWatermarkSession{db: db, tx: tx, rows: rows, columns: columns, fields: fields, upper: upper, cursorFields: cursorNames}, nil
}

func watermarkFieldTypeSupported(fieldType datatype.FieldType) bool {
	switch fieldType {
	case datatype.FieldTypeString, datatype.FieldTypeInt, datatype.FieldTypeBigInt, datatype.FieldTypeFloat, datatype.FieldTypeDouble, datatype.FieldTypeDecimal, datatype.FieldTypeDate, datatype.FieldTypeTime, datatype.FieldTypeTimestamp:
		return true
	default:
		return false
	}
}

func quoteFields(dialect commonquery.Dialect, fields []string) []string {
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		result = append(result, dialect.QuoteIdentifier(field))
	}
	return result
}

func orderFields(fields []string, direction string) string {
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		result = append(result, field+" "+direction)
	}
	return strings.Join(result, ", ")
}

func cursorPredicate(dialect commonquery.Dialect, fields []datatype.FieldInfo, values []string, operator string) (string, []interface{}, error) {
	if len(fields) != len(values) {
		return "", nil, fmt.Errorf("DM8 watermark cursor has %d values, want %d", len(values), len(fields))
	}
	terms := make([]string, 0, len(fields))
	args := make([]interface{}, 0, len(fields)*len(fields))
	for index := range fields {
		parts := make([]string, 0, index+1)
		for prefix := 0; prefix < index; prefix++ {
			parts = append(parts, dialect.QuoteIdentifier(fields[prefix].Name)+" = CAST(? AS "+cursorCastType(fields[prefix])+")")
			value, err := parseCursorValue(fields[prefix], values[prefix])
			if err != nil {
				return "", nil, err
			}
			args = append(args, value)
		}
		comparison := operator
		if operator == "<=" && index < len(fields)-1 {
			comparison = "<"
		}
		parts = append(parts, dialect.QuoteIdentifier(fields[index].Name)+" "+comparison+" CAST(? AS "+cursorCastType(fields[index])+")")
		value, err := parseCursorValue(fields[index], values[index])
		if err != nil {
			return "", nil, err
		}
		args = append(args, value)
		terms = append(terms, "("+strings.Join(parts, " AND ")+")")
	}
	return "(" + strings.Join(terms, " OR ") + ")", args, nil
}

func cursorCastType(field datatype.FieldInfo) string {
	if field.Type == datatype.FieldTypeDecimal && field.Precision > 0 {
		return fmt.Sprintf("DECIMAL(%d,%d)", field.Precision, field.Scale)
	}
	if field.Type == datatype.FieldTypeString && field.Size > 0 {
		return fmt.Sprintf("VARCHAR(%d)", field.Size)
	}
	if sqlType, err := dmSQLType(field); err == nil {
		return sqlType
	}
	return field.NativeType
}

func parseCursorValue(field datatype.FieldInfo, value string) (interface{}, error) {
	switch field.Type {
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt:
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse DM8 watermark integer %q: %w", value, err)
		}
		return parsed, nil
	case datatype.FieldTypeFloat, datatype.FieldTypeDouble:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("parse DM8 watermark number %q: %w", value, err)
		}
		return parsed, nil
	case datatype.FieldTypeDate, datatype.FieldTypeTime, datatype.FieldTypeTimestamp:
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return nil, fmt.Errorf("parse DM8 watermark time %q: %w", value, err)
		}
		return parsed, nil
	default:
		return value, nil
	}
}

func stringifyCursor(values []interface{}) []string {
	result := make([]string, len(values))
	for index, value := range values {
		switch typed := value.(type) {
		case time.Time:
			result[index] = typed.UTC().Format(time.RFC3339Nano)
		case []byte:
			result[index] = string(typed)
		default:
			result[index] = fmt.Sprint(typed)
		}
	}
	return result
}

func (s *boundedWatermarkSession) UpperBound() *plugin.WatermarkCursor {
	if s.upper == nil {
		return nil
	}
	return &plugin.WatermarkCursor{Values: append([]string(nil), s.upper.Values...)}
}

func (s *boundedWatermarkSession) TableInfo() (*datatype.TableInfo, *datatype.SpatialInfo) {
	return &datatype.TableInfo{Fields: append([]datatype.FieldInfo(nil), s.fields...)}, nil
}

func (s *boundedWatermarkSession) PositionForRow(row map[string]interface{}) (*plugin.WatermarkCursor, error) {
	values := make([]interface{}, 0, len(s.cursorFields))
	for _, field := range s.cursorFields {
		value, ok := row[field]
		if !ok || value == nil {
			return nil, fmt.Errorf("DM8 watermark row is missing cursor field %q", field)
		}
		values = append(values, value)
	}
	return &plugin.WatermarkCursor{Values: stringifyCursor(values)}, nil
}

func (s *boundedWatermarkSession) ReadBatch(_ context.Context, limit int) (*plugin.BatchData, error) {
	if s.closed {
		return nil, fmt.Errorf("DM8 watermark session is closed")
	}
	if limit <= 0 {
		limit = 1000
	}
	batch := &plugin.BatchData{Fields: append([]datatype.FieldInfo(nil), s.fields...), Offset: s.offset, Rows: make([]map[string]interface{}, 0, limit)}
	if s.exhausted {
		return batch, nil
	}
	for len(batch.Rows) < limit {
		if !s.rows.Next() {
			s.exhausted = true
			break
		}
		values := make([]interface{}, len(s.columns))
		pointers := make([]interface{}, len(s.columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := s.rows.Scan(pointers...); err != nil {
			return nil, fmt.Errorf("scan DM8 watermark row: %w", err)
		}
		row := make(map[string]interface{}, len(s.columns))
		for index, column := range s.columns {
			value := values[index]
			if bytes, ok := value.([]byte); ok {
				value = string(bytes)
			}
			row[column] = value
		}
		batch.Rows = append(batch.Rows, row)
	}
	if err := s.rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate DM8 watermark rows: %w", err)
	}
	s.offset += int64(len(batch.Rows))
	return batch, nil
}

func (s *boundedWatermarkSession) Close(context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	var result error
	if s.rows != nil {
		result = s.rows.Close()
	}
	if s.tx != nil {
		if err := s.tx.Rollback(); err != nil && err != sql.ErrTxDone && result == nil {
			result = err
		}
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil && result == nil {
			result = err
		}
	}
	return result
}
