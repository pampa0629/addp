package dameng

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

func (p *Plugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath, _ plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	if plugin.IsEngineCatalogRootPath(parent) {
		schema, err := p.currentSchema(ctx, connInfo)
		if err != nil {
			return nil, err
		}
		count, err := p.tableCount(ctx, connInfo, schema)
		if err != nil {
			return nil, err
		}
		return []plugin.EngineCatalogEntry{plugin.TabularNamespaceCatalogEntry(parent, plugin.EngineCatalogTermSchema, schema, count)}, nil
	}
	segments := plugin.EngineCatalogPathWithoutRoot(parent).Segments
	if len(segments) != 1 || segments[0].Term != plugin.EngineCatalogTermSchema {
		return nil, fmt.Errorf("DM8 catalog children require server root or schema path")
	}
	return p.listTableEntries(ctx, connInfo, parent, segments[0].Name)
}

func (p *Plugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	if plugin.IsEngineCatalogRootPath(path) {
		return &plugin.EngineCatalogEntry{Path: path, Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer, Role: plugin.EngineCatalogRoleBranch}, nil
	}
	segments := plugin.EngineCatalogPathWithoutRoot(path).Segments
	if len(segments) == 1 {
		schema, err := p.currentSchema(ctx, connInfo)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(schema, segments[0].Name) {
			return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorNotFound, fmt.Errorf("DM8 schema %q is not visible", segments[0].Name))
		}
		count, err := p.tableCount(ctx, connInfo, schema)
		if err != nil {
			return nil, err
		}
		entry := plugin.TabularNamespaceCatalogEntry(plugin.EngineCatalogRootPath(p.EngineCatalogModel(), path.EngineID), plugin.EngineCatalogTermSchema, schema, count)
		entry.Path = path
		return &entry, nil
	}
	if len(segments) == 2 {
		facts, err := p.DescribeEngineCatalogFacts(ctx, connInfo, path, plugin.EngineCatalogFactsOptions{})
		if err != nil {
			return nil, err
		}
		return &plugin.EngineCatalogEntry{Name: segments[1].Name, Path: path, Term: plugin.EngineCatalogTermTable, Kind: facts.Kind, Role: plugin.EngineCatalogRoleLeaf, Table: facts.Table, UpdatedAt: facts.UpdatedAt}, nil
	}
	return nil, fmt.Errorf("invalid DM8 catalog path")
}

func (p *Plugin) DescribeEngineCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	schema, table, err := tablePathParts(path)
	if err != nil {
		return nil, err
	}
	info, err := p.tableInfo(ctx, connInfo, schema, table)
	if err != nil {
		return nil, err
	}
	fields, err := p.listColumns(ctx, connInfo, schema, table)
	if err != nil {
		return nil, err
	}
	info.Fields = fields
	for _, field := range fields {
		if field.PrimaryKey {
			info.PrimaryKey = append(info.PrimaryKey, field.Name)
		}
	}
	if opts.IncludeStatistics {
		db, err := p.openDB(connInfo)
		if err != nil {
			return nil, err
		}
		defer db.Close()
		var count int64
		if err := db.QueryRowContext(ctx, commonquery.ForDialect(p.SQLDialect()).CountTableSQL(schema, table, "")).Scan(&count); err != nil {
			return nil, fmt.Errorf("count DM8 table rows: %w", err)
		}
		info.RowCount = &count
	}
	facts := &plugin.EngineCatalogFacts{Path: path, Kind: info.Kind, Table: info.Clone(), UpdatedAt: info.UpdatedAt}
	if opts.IncludeConstraints {
		facts.Constraints, err = p.listConstraints(ctx, connInfo, schema, table)
		if err != nil {
			return nil, err
		}
	}
	return facts, nil
}

func (p *Plugin) currentSchema(ctx context.Context, connInfo plugin.ConnectionInfo) (string, error) {
	db, err := p.openDB(connInfo)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var schema string
	if err := db.QueryRowContext(ctx, "SELECT USER FROM DUAL").Scan(&schema); err != nil {
		return "", fmt.Errorf("query DM8 current schema: %w", err)
	}
	return strings.TrimSpace(schema), nil
}

func (p *Plugin) requireCurrentSchema(ctx context.Context, connInfo plugin.ConnectionInfo, schema string) error {
	current, err := p.currentSchema(ctx, connInfo)
	if err != nil {
		return err
	}
	if !strings.EqualFold(current, strings.TrimSpace(schema)) {
		return fmt.Errorf("DM8 provider only exposes the authenticated schema %q", current)
	}
	return nil
}

func (p *Plugin) tableCount(ctx context.Context, connInfo plugin.ConnectionInfo, schema string) (int, error) {
	if err := p.requireCurrentSchema(ctx, connInfo, schema); err != nil {
		return 0, err
	}
	db, err := p.openDB(connInfo)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM USER_OBJECTS WHERE OBJECT_TYPE IN ('TABLE', 'VIEW')").Scan(&count); err != nil {
		return 0, fmt.Errorf("count DM8 catalog tables: %w", err)
	}
	return count, nil
}

type tableRow struct {
	name      string
	kind      string
	comment   sql.NullString
	numRows   sql.NullInt64
	createdAt sql.NullTime
	updatedAt sql.NullTime
}

func (p *Plugin) listTables(ctx context.Context, connInfo plugin.ConnectionInfo, schema string) ([]tableRow, error) {
	if err := p.requireCurrentSchema(ctx, connInfo, schema); err != nil {
		return nil, err
	}
	db, err := p.openDB(connInfo)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT objects.object_name,
		       CASE objects.object_type WHEN 'TABLE' THEN 'table' ELSE 'view' END,
		       comments_view.comments, tables_view.num_rows, objects.created, objects.last_ddl_time
		  FROM user_objects objects
		  LEFT JOIN user_tab_comments comments_view ON comments_view.table_name = objects.object_name
		  LEFT JOIN user_tables tables_view ON tables_view.table_name = objects.object_name
		 WHERE objects.object_type IN ('TABLE', 'VIEW')
		 ORDER BY objects.object_name`)
	if err != nil {
		return nil, fmt.Errorf("list DM8 tables: %w", err)
	}
	defer rows.Close()
	result := make([]tableRow, 0)
	for rows.Next() {
		var row tableRow
		if err := rows.Scan(&row.name, &row.kind, &row.comment, &row.numRows, &row.createdAt, &row.updatedAt); err != nil {
			return nil, fmt.Errorf("scan DM8 table: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate DM8 tables: %w", err)
	}
	return result, nil
}

func (p *Plugin) listTableEntries(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.EngineCatalogPath, schema string) ([]plugin.EngineCatalogEntry, error) {
	tables, err := p.listTables(ctx, connInfo, schema)
	if err != nil {
		return nil, err
	}
	entries := make([]plugin.EngineCatalogEntry, 0, len(tables))
	for _, row := range tables {
		info := &datatype.TableInfo{Name: row.name, Kind: row.kind, Comment: row.comment.String}
		if row.numRows.Valid {
			value := row.numRows.Int64
			info.EstimatedRowCount = &value
		}
		var updatedAt *time.Time
		if row.updatedAt.Valid {
			value := row.updatedAt.Time
			updatedAt = &value
		}
		entries = append(entries, plugin.EngineCatalogEntry{Name: row.name, Path: plugin.TabularItemPath(parent.EngineID, plugin.EngineCatalogTermSchema, schema, row.name), Term: plugin.EngineCatalogTermTable, Kind: row.kind, Role: plugin.EngineCatalogRoleLeaf, Table: info, UpdatedAt: updatedAt})
	}
	return entries, nil
}

func (p *Plugin) tableInfo(ctx context.Context, connInfo plugin.ConnectionInfo, schema, table string) (*datatype.TableInfo, error) {
	tables, err := p.listTables(ctx, connInfo, schema)
	if err != nil {
		return nil, err
	}
	for _, row := range tables {
		if strings.EqualFold(row.name, table) {
			info := &datatype.TableInfo{Name: row.name, Kind: row.kind, Comment: row.comment.String}
			if row.numRows.Valid {
				value := row.numRows.Int64
				info.EstimatedRowCount = &value
			}
			if row.createdAt.Valid {
				value := row.createdAt.Time
				info.CreatedAt = &value
			}
			if row.updatedAt.Valid {
				value := row.updatedAt.Time
				info.UpdatedAt = &value
			}
			return info, nil
		}
	}
	return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorNotFound, fmt.Errorf("DM8 table %q not found in schema %q", table, schema))
}

func (p *Plugin) listColumns(ctx context.Context, connInfo plugin.ConnectionInfo, schema, table string) ([]datatype.FieldInfo, error) {
	if err := p.requireCurrentSchema(ctx, connInfo, schema); err != nil {
		return nil, err
	}
	db, err := p.openDB(connInfo)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT columns_view.column_name, columns_view.data_type, columns_view.data_length,
		       columns_view.data_precision, columns_view.data_scale, columns_view.nullable,
		       columns_view.column_id, columns_view.data_default, comments_view.comments,
		       CASE WHEN primary_keys.column_name IS NULL THEN 0 ELSE 1 END
		  FROM user_tab_columns columns_view
		  LEFT JOIN user_col_comments comments_view
		    ON comments_view.table_name = columns_view.table_name
		   AND comments_view.column_name = columns_view.column_name
		  LEFT JOIN (
		       SELECT constraint_columns.table_name, constraint_columns.column_name
		         FROM user_constraints constraints_view
		         JOIN user_cons_columns constraint_columns
		           ON constraint_columns.constraint_name = constraints_view.constraint_name
		        WHERE constraints_view.constraint_type = 'P'
		  ) primary_keys
		    ON primary_keys.table_name = columns_view.table_name
		   AND primary_keys.column_name = columns_view.column_name
		 WHERE columns_view.table_name = ?
		 ORDER BY columns_view.column_id`, strings.ToUpper(table))
	if err != nil {
		return nil, fmt.Errorf("list DM8 columns: %w", err)
	}
	defer rows.Close()
	fields := make([]datatype.FieldInfo, 0)
	for rows.Next() {
		var name, nativeType, nullable string
		var size int
		var precision, scale sql.NullInt64
		var ordinal, primary int
		var defaultExpr, comment sql.NullString
		if err := rows.Scan(&name, &nativeType, &size, &precision, &scale, &nullable, &ordinal, &defaultExpr, &comment, &primary); err != nil {
			return nil, fmt.Errorf("scan DM8 column: %w", err)
		}
		field := datatype.FieldInfo{Name: name, NativeType: nativeType, Type: commonFieldType(nativeType), Nullable: strings.EqualFold(nullable, "Y"), PrimaryKey: primary == 1, Size: size, OrdinalPosition: ordinal, DefaultExpression: strings.TrimSpace(defaultExpr.String), Comment: comment.String}
		if precision.Valid {
			field.Precision = int(precision.Int64)
		}
		if scale.Valid {
			field.Scale = int(scale.Int64)
		}
		fields = append(fields, field)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate DM8 columns: %w", err)
	}
	if len(fields) == 0 {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorNotFound, fmt.Errorf("DM8 table %q has no visible columns", table))
	}
	return plugin.NormalizeFieldInfos(fields), nil
}

func commonFieldType(native string) datatype.FieldType {
	switch strings.ToUpper(strings.TrimSpace(native)) {
	case "CHAR", "CHARACTER", "NCHAR", "VARCHAR", "VARCHAR2", "NVARCHAR", "NVARCHAR2", "TEXT", "CLOB":
		return datatype.FieldTypeString
	case "BIT", "BOOLEAN":
		return datatype.FieldTypeBool
	case "BINARY", "VARBINARY", "BLOB", "IMAGE":
		return datatype.FieldTypeBytes
	case "TINYINT", "SMALLINT", "INTEGER", "INT":
		return datatype.FieldTypeInt
	case "BIGINT":
		return datatype.FieldTypeBigInt
	case "REAL", "FLOAT":
		return datatype.FieldTypeFloat
	case "DOUBLE", "DOUBLE PRECISION":
		return datatype.FieldTypeDouble
	case "DECIMAL", "NUMERIC", "NUMBER":
		return datatype.FieldTypeDecimal
	case "DATE":
		return datatype.FieldTypeDate
	case "TIME", "TIME WITH TIME ZONE", "TIME WITH LOCAL TIME ZONE":
		return datatype.FieldTypeTime
	case "DATETIME", "TIMESTAMP", "TIMESTAMP WITH TIME ZONE", "TIMESTAMP WITH LOCAL TIME ZONE":
		return datatype.FieldTypeTimestamp
	default:
		return datatype.FieldTypeUnknown
	}
}

func (p *Plugin) listConstraints(ctx context.Context, connInfo plugin.ConnectionInfo, schema, table string) ([]plugin.ConstraintFacts, error) {
	if err := p.requireCurrentSchema(ctx, connInfo, schema); err != nil {
		return nil, err
	}
	db, err := p.openDB(connInfo)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT constraints_view.constraint_name, constraints_view.constraint_type, constraint_columns.column_name
		  FROM user_constraints constraints_view
		  JOIN user_cons_columns constraint_columns
		    ON constraint_columns.constraint_name = constraints_view.constraint_name
		 WHERE constraints_view.table_name = ?
		   AND constraints_view.constraint_type IN ('P', 'U')
		 ORDER BY constraints_view.constraint_name, constraint_columns.position`, strings.ToUpper(table))
	if err != nil {
		return nil, fmt.Errorf("list DM8 constraints: %w", err)
	}
	defer rows.Close()
	byName := map[string]*plugin.ConstraintFacts{}
	order := make([]string, 0)
	for rows.Next() {
		var name, constraintType, field string
		if err := rows.Scan(&name, &constraintType, &field); err != nil {
			return nil, fmt.Errorf("scan DM8 constraint: %w", err)
		}
		fact := byName[name]
		if fact == nil {
			kind := plugin.ConstraintTypeUnique
			if constraintType == "P" {
				kind = plugin.ConstraintTypePrimaryKey
			}
			fact = &plugin.ConstraintFacts{Name: name, ConstraintType: kind}
			byName[name] = fact
			order = append(order, name)
		}
		fact.Fields = append(fact.Fields, field)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate DM8 constraints: %w", err)
	}
	result := make([]plugin.ConstraintFacts, 0, len(order))
	for _, name := range order {
		result = append(result, *byName[name])
	}
	return result, nil
}
