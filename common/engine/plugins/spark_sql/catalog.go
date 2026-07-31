package spark_sql

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

const sparkCatalogOperationTimeout = 30 * time.Second

func (p *SparkSQLPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, _ plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	ctx, cancel := sparkCatalogContext(ctx)
	defer cancel()

	segments, root, err := sparkCatalogBusinessSegments(parent)
	if err != nil {
		return nil, err
	}
	if root {
		result, err := p.runQuery(ctx, connInfo, "SHOW DATABASES")
		if err != nil {
			return nil, fmt.Errorf("list Spark databases: %w", err)
		}
		entries := make([]plugin.CatalogEntry, 0, len(result.Rows))
		for _, row := range result.Rows {
			name := sparkRowString(row, "namespace", "databaseName", "database")
			if name == "" || isSparkSystemDatabase(name) {
				continue
			}
			entries = append(entries, plugin.CatalogEntry{
				Name: name,
				Path: plugin.TabularNamespacePath(parent.EngineID, plugin.CatalogTermDatabase, name),
				Term: plugin.CatalogTermDatabase,
				Kind: plugin.CatalogKindNamespace,
				Role: plugin.CatalogRoleBranch,
			})
		}
		return entries, nil
	}
	if len(segments) != 1 {
		return nil, fmt.Errorf("Spark catalog children require a database path")
	}

	database := segments[0].Name
	result, err := p.runQuery(ctx, connInfo, "SHOW TABLES IN "+quoteSparkIdentifier(database))
	if err != nil {
		return nil, fmt.Errorf("list Spark tables in %q: %w", database, err)
	}
	entries := make([]plugin.CatalogEntry, 0, len(result.Rows))
	for _, row := range result.Rows {
		name := sparkRowString(row, "tableName", "table_name", "table")
		if name == "" {
			continue
		}
		table := sparkSQLTableInfo(name)
		entries = append(entries, plugin.CatalogEntry{
			Name:  name,
			Path:  plugin.TabularItemPath(parent.EngineID, plugin.CatalogTermDatabase, database, name),
			Term:  plugin.CatalogTermTable,
			Kind:  plugin.CatalogKindTable,
			Role:  plugin.CatalogRoleLeaf,
			Table: plugin.CatalogEntryTableSummary(&table),
		})
	}
	return entries, nil
}

func (p *SparkSQLPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	segments, root, err := sparkCatalogBusinessSegments(path)
	if err != nil {
		return nil, err
	}
	if root {
		return &plugin.CatalogEntry{Name: "", Path: path, Term: plugin.CatalogTermServer, Kind: plugin.CatalogTermServer, Role: plugin.CatalogRoleBranch}, nil
	}
	if len(segments) == 1 {
		return &plugin.CatalogEntry{Name: segments[0].Name, Path: path, Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, Role: plugin.CatalogRoleBranch}, nil
	}
	if len(segments) != 2 {
		return nil, fmt.Errorf("Spark catalog item path requires database and table segments")
	}
	facts, err := p.DescribeCatalogFacts(ctx, connInfo, path, plugin.CatalogFactsOptions{})
	if err != nil {
		return nil, err
	}
	return &plugin.CatalogEntry{
		Name: segments[1].Name, Path: path, Term: plugin.CatalogTermTable,
		Kind: plugin.CatalogKindTable, Role: plugin.CatalogRoleLeaf,
		Table: plugin.CatalogEntryTableInfo(facts),
	}, nil
}

func (p *SparkSQLPlugin) DescribeCatalogFacts(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	segments, root, err := sparkCatalogBusinessSegments(path)
	if err != nil {
		return nil, err
	}
	if root || len(segments) != 2 {
		return nil, fmt.Errorf("Spark catalog facts require database and table segments")
	}

	ctx, cancel := sparkCatalogContext(ctx)
	defer cancel()
	database, tableName := segments[0].Name, segments[1].Name
	qualified := quoteSparkIdentifier(database) + "." + quoteSparkIdentifier(tableName)
	result, err := p.runQuery(ctx, connInfo, "DESCRIBE TABLE "+qualified)
	if err != nil {
		return nil, fmt.Errorf("describe Spark table %q: %w", path.StringPath(), err)
	}
	fields := make([]datatype.FieldInfo, 0, len(result.Rows))
	for _, row := range result.Rows {
		name := sparkRowString(row, "col_name", "colName", "column_name")
		nativeType := sparkRowString(row, "data_type", "dataType", "type")
		if name == "" || nativeType == "" || strings.HasPrefix(name, "#") {
			continue
		}
		fields = append(fields, datatype.FieldInfo{
			Name:       name,
			Type:       sparkCommonFieldType(nativeType),
			NativeType: nativeType,
			Nullable:   true,
			Comment:    sparkRowString(row, "comment"),
		})
	}
	fields = plugin.NormalizeFieldInfos(fields)
	table := sparkSQLTableInfo(tableName)
	table.Fields = fields
	table.Native = map[string]interface{}{"namespace": database}
	if opts.IncludeStatistics {
		countResult, countErr := p.runQuery(ctx, connInfo, "SELECT COUNT(*) AS row_count FROM "+qualified)
		if countErr == nil && len(countResult.Rows) > 0 {
			if count, ok := sparkRowInt64(countResult.Rows[0], "row_count", "count(1)", "count(*)"); ok {
				table.RowCount = &count
			}
		}
	}
	return &plugin.CatalogFacts{Path: path, Kind: plugin.CatalogKindTable, Table: table.Clone()}, nil
}

func sparkCatalogContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= sparkCatalogOperationTimeout {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, sparkCatalogOperationTimeout)
}

func sparkCatalogBusinessSegments(path plugin.CatalogPath) ([]plugin.CatalogSegment, bool, error) {
	if len(path.Segments) == 0 || !plugin.IsCatalogRootSegment(path.Segments[0]) || path.Segments[0].Term != plugin.CatalogTermServer {
		return nil, false, fmt.Errorf("Spark catalog path requires an explicit server root segment")
	}
	if len(path.Segments) == 1 {
		return nil, true, nil
	}
	segments := path.Segments[1:]
	if segments[0].Term != plugin.CatalogTermDatabase || segments[0].Name == "" {
		return nil, false, fmt.Errorf("Spark catalog path requires a database segment")
	}
	if len(segments) > 2 || (len(segments) == 2 && (segments[1].Term != plugin.CatalogTermTable || segments[1].Name == "")) {
		return nil, false, fmt.Errorf("Spark catalog path supports database and table segments")
	}
	return segments, false, nil
}

func sparkSQLTableInfo(tableName string) datatype.TableInfo {
	return datatype.TableInfo{Name: tableName, Kind: plugin.CatalogKindTable}
}

func sparkCommonFieldType(nativeType string) datatype.FieldType {
	normalized := strings.ToLower(strings.TrimSpace(nativeType))
	baseType := normalized
	if index := strings.IndexAny(baseType, "(<"); index > 0 {
		baseType = baseType[:index]
	}
	switch baseType {
	case "char", "varchar", "string":
		return datatype.FieldTypeString
	case "boolean", "bool":
		return datatype.FieldTypeBool
	case "binary":
		return datatype.FieldTypeBytes
	case "byte", "tinyint", "short", "smallint", "int", "integer":
		return datatype.FieldTypeInt
	case "long", "bigint":
		return datatype.FieldTypeBigInt
	case "float":
		return datatype.FieldTypeFloat
	case "double":
		return datatype.FieldTypeDouble
	case "decimal", "numeric":
		return datatype.FieldTypeDecimal
	case "date":
		return datatype.FieldTypeDate
	case "timestamp", "timestamp_ltz", "timestamp_ntz":
		return datatype.FieldTypeTimestamp
	case "array":
		return datatype.FieldTypeArray
	case "map", "struct":
		return datatype.FieldTypeJSON
	default:
		return datatype.FieldTypeUnknown
	}
}

func sparkRowString(row map[string]interface{}, candidates ...string) string {
	for _, candidate := range candidates {
		for key, value := range row {
			if strings.EqualFold(key, candidate) {
				switch typed := value.(type) {
				case []byte:
					return strings.TrimSpace(string(typed))
				default:
					return strings.TrimSpace(fmt.Sprint(value))
				}
			}
		}
	}
	return ""
}

func sparkRowInt64(row map[string]interface{}, candidates ...string) (int64, bool) {
	value := sparkRowString(row, candidates...)
	parsed, err := strconv.ParseInt(value, 10, 64)
	return parsed, err == nil
}

func isSparkSystemDatabase(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "information_schema", "sys":
		return true
	default:
		return false
	}
}
