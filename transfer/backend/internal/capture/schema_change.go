package capture

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	mysqltypes "github.com/addp/common/format/mappers/mysql"
	"github.com/addp/transfer/internal/models"
)

type postgreSQLSchemaFieldFact struct {
	nativeType        string
	temporalPrecision sql.NullInt64
	nullable          bool
}

type mySQLSchemaFieldFact struct {
	columnType        string
	dataType          string
	temporalPrecision sql.NullInt64
	defaultValue      sql.NullString
	nullable          bool
}

// InspectAdditiveFields 重新读取当前源表，只返回本次阻塞消息明确报告的新增字段事实。
// 其他尚未到达 committed offset 的源表新增字段不得提前进入当前 revision。
func (r *DatabasePlanResolver) InspectAdditiveFields(ctx context.Context, task *models.TransferTask, sourceFields []string) ([]models.SchemaChangeField, error) {
	requested, err := normalizeAdditiveSourceFields(sourceFields)
	if err != nil {
		return nil, err
	}
	plan, err := r.resolveBindings(task)
	if err != nil {
		return nil, err
	}
	configured := make(map[string]struct{}, len(plan.SourceFields))
	for _, source := range plan.SourceFields {
		configured[source] = struct{}{}
	}
	for _, source := range requested {
		if _, exists := configured[source]; exists {
			return nil, fmt.Errorf("schema change source field %q is already mapped", source)
		}
	}
	switch plan.SourceType {
	case models.CaptureSourcePostgreSQL:
		return inspectPostgreSQLAdditiveFields(ctx, plan, requested)
	case models.CaptureSourceMySQL:
		return inspectMySQLAdditiveFields(ctx, plan, requested)
	default:
		return nil, fmt.Errorf("unsupported database CDC source type %q", plan.SourceType)
	}
}

func normalizeAdditiveSourceFields(fields []string) ([]string, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("schema change requires unexpected source fields")
	}
	result := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		name := strings.TrimSpace(field)
		if name == "" {
			return nil, fmt.Errorf("schema change source field is required")
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("schema change source field %q is duplicated", name)
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result, nil
}

func inspectPostgreSQLAdditiveFields(ctx context.Context, plan *CapturePlan, requested []string) ([]models.SchemaChangeField, error) {
	db, err := openPostgreSQL(plan.SourceConnInfo)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT a.attname, format_type(a.atttypid, a.atttypmod), ic.datetime_precision, NOT a.attnotnull
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN information_schema.columns ic
		  ON ic.table_schema = n.nspname AND ic.table_name = c.relname AND ic.column_name = a.attname
		WHERE n.nspname = $1 AND c.relname = $2
		  AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, plan.SourceSchema, plan.SourceTable)
	if err != nil {
		return nil, fmt.Errorf("query PostgreSQL CDC source fields for schema change: %w", err)
	}
	defer rows.Close()
	facts := map[string]postgreSQLSchemaFieldFact{}
	for rows.Next() {
		var name string
		var fact postgreSQLSchemaFieldFact
		if err := rows.Scan(&name, &fact.nativeType, &fact.temporalPrecision, &fact.nullable); err != nil {
			return nil, err
		}
		facts[name] = fact
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, name := range plan.SourceFields {
		fact, exists := facts[name]
		if !exists {
			return nil, fmt.Errorf("PostgreSQL CDC existing source field %q is missing", name)
		}
		if err := validatePostgreSQLCDCSourceFieldType(name, fact.nativeType, fact.temporalPrecision, plan.SourceFieldTypes[name]); err != nil {
			return nil, err
		}
		if fact.nullable != plan.SourceFieldNullables[name] {
			return nil, fmt.Errorf("PostgreSQL CDC existing source field %q nullable changed", name)
		}
	}
	if err := validatePostgreSQLSourcePrimaryKey(ctx, plan); err != nil {
		return nil, err
	}
	result := make([]models.SchemaChangeField, 0, len(requested))
	for _, name := range requested {
		fact, exists := facts[name]
		if !exists {
			return nil, fmt.Errorf("PostgreSQL CDC additive source field %q no longer exists", name)
		}
		fieldType := postgresqlCDCCommonFieldType(fact.nativeType)
		if fieldType == datatype.FieldTypeGeometry {
			return nil, fmt.Errorf("PostgreSQL CDC additive geometry field %q is not supported", name)
		}
		if !fact.nullable {
			return nil, fmt.Errorf("PostgreSQL CDC additive source field %q must be nullable", name)
		}
		if err := validatePostgreSQLCDCSourceFieldType(name, fact.nativeType, fact.temporalPrecision, fieldType); err != nil {
			return nil, err
		}
		result = append(result, models.SchemaChangeField{Source: name, Target: name, TargetType: string(fieldType), Nullable: true})
	}
	return result, nil
}

func inspectMySQLAdditiveFields(ctx context.Context, plan *CapturePlan, requested []string) ([]models.SchemaChangeField, error) {
	db, err := openMySQL(plan.SourceConnInfo)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT column_name, column_type, data_type, datetime_precision, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position`, plan.SourceDatabase, plan.SourceTable)
	if err != nil {
		return nil, fmt.Errorf("query MySQL CDC source fields for schema change: %w", err)
	}
	defer rows.Close()
	facts := map[string]mySQLSchemaFieldFact{}
	for rows.Next() {
		var name, nullableText string
		var fact mySQLSchemaFieldFact
		if err := rows.Scan(&name, &fact.columnType, &fact.dataType, &fact.temporalPrecision, &nullableText, &fact.defaultValue); err != nil {
			return nil, err
		}
		fact.nullable = strings.EqualFold(nullableText, "YES")
		facts[name] = fact
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, name := range plan.SourceFields {
		fact, exists := facts[name]
		if !exists {
			return nil, fmt.Errorf("MySQL CDC existing source field %q is missing", name)
		}
		if err := validateMySQLCDCSourceFieldType(name, fact.columnType, fact.dataType, fact.temporalPrecision, fact.defaultValue, plan.SourceFieldTypes[name]); err != nil {
			return nil, err
		}
		if fact.nullable != plan.SourceFieldNullables[name] {
			return nil, fmt.Errorf("MySQL CDC existing source field %q nullable changed", name)
		}
	}
	if err := validateMySQLSourcePrimaryKey(ctx, plan); err != nil {
		return nil, err
	}
	result := make([]models.SchemaChangeField, 0, len(requested))
	for _, name := range requested {
		fact, exists := facts[name]
		if !exists {
			return nil, fmt.Errorf("MySQL CDC additive source field %q no longer exists", name)
		}
		if !fact.nullable {
			return nil, fmt.Errorf("MySQL CDC additive source field %q must be nullable", name)
		}
		fieldType := (&mysqltypes.TypeMapper{}).ToCommon(strings.ToLower(strings.TrimSpace(fact.dataType)))
		if err := validateMySQLCDCSourceFieldType(name, fact.columnType, fact.dataType, fact.temporalPrecision, fact.defaultValue, fieldType); err != nil {
			return nil, err
		}
		result = append(result, models.SchemaChangeField{Source: name, Target: name, TargetType: string(fieldType), Nullable: true})
	}
	return result, nil
}
