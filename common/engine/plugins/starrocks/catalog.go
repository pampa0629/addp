package starrocks

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"gorm.io/gorm"
)

func isSystemNamespace(name string) bool {
	switch strings.ToLower(name) {
	case "information_schema", "sys", "_statistics_":
		return true
	}
	return false
}
func (p *Plugin) callbacks() plugin.TabularCatalogCallbacks {
	return plugin.TabularCatalogCallbacks{NamespaceTerm: plugin.EngineCatalogTermDatabase, ListNamespaces: p.listNamespaces, ListTables: p.listTables, GetTable: p.getTable, ListColumns: p.listColumns, RowCount: p.rowCount, IsSystemNamespaceFunc: isSystemNamespace}
}
func (p *Plugin) ListChildren(ctx context.Context, info plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return plugin.ListTabularCatalogChildren(ctx, p.callbacks(), &plugin.Engine{ID: parent.EngineID, EngineType: p.Type(), ConnectionInfo: info}, parent, opts)
}
func (p *Plugin) ResolvePath(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return plugin.ResolveTabularCatalogPath(ctx, p.callbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: info}, path)
}
func (p *Plugin) DescribeEngineCatalogFacts(ctx context.Context, info plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	facts, err := plugin.DescribeTabularCatalogFacts(ctx, p.callbacks(), &plugin.Engine{ID: path.EngineID, EngineType: p.Type(), ConnectionInfo: info}, path, opts)
	if err != nil || facts == nil || facts.Table == nil {
		return facts, err
	}
	keys := map[string]bool{}
	for _, key := range facts.Table.PrimaryKey {
		keys[key] = true
	}
	for i := range facts.Table.Fields {
		facts.Table.Fields[i].PrimaryKey = keys[facts.Table.Fields[i].Name]
		delete(keys, facts.Table.Fields[i].Name)
	}
	if len(keys) != 0 {
		return nil, fmt.Errorf("StarRocks primary key refers to unknown fields")
	}
	return facts, nil
}
func (*Plugin) listNamespaces(ctx context.Context, db *gorm.DB, root plugin.EngineCatalogPath) ([]plugin.EngineCatalogEntry, error) {
	var rows []struct {
		Name      string
		LeafCount int
	}
	err := db.WithContext(ctx).Raw(`SELECT s.schema_name AS name,COUNT(t.table_name) AS leaf_count FROM information_schema.schemata s LEFT JOIN information_schema.tables t ON t.table_schema=s.schema_name AND t.table_type='BASE TABLE' AND t.engine='StarRocks' GROUP BY s.schema_name ORDER BY s.schema_name`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	entries := make([]plugin.EngineCatalogEntry, 0, len(rows))
	for _, row := range rows {
		if !isSystemNamespace(row.Name) {
			entries = append(entries, plugin.TabularNamespaceCatalogEntry(root, plugin.EngineCatalogTermDatabase, row.Name, row.LeafCount))
		}
	}
	return entries, nil
}
func (p *Plugin) listTables(ctx context.Context, db *gorm.DB, database string) ([]datatype.TableInfo, error) {
	if isSystemNamespace(database) {
		return nil, nil
	}
	var rows []struct{ Name string }
	err := db.WithContext(ctx).Raw(`SELECT table_name AS name FROM information_schema.tables WHERE table_schema=? AND table_type='BASE TABLE' AND engine='StarRocks' ORDER BY table_name`, database).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	tables := make([]datatype.TableInfo, 0, len(rows))
	for _, row := range rows {
		tables = append(tables, datatype.TableInfo{Name: row.Name, Kind: "table", Native: map[string]interface{}{"engine": "StarRocks"}})
	}
	return tables, nil
}
func (*Plugin) getTable(ctx context.Context, db *gorm.DB, database, table string) (*datatype.TableInfo, error) {
	var matches []struct{ Name string }
	err := db.WithContext(ctx).Raw(`SELECT table_name AS name FROM information_schema.tables WHERE table_schema=? AND table_name=? AND table_type='BASE TABLE' AND engine='StarRocks'`, database, table).Scan(&matches).Error
	if err != nil || len(matches) == 0 {
		return nil, err
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("StarRocks exact table lookup is ambiguous")
	}
	rows, err := db.WithContext(ctx).Raw("SHOW CREATE TABLE " + commonquery.ForDialect(commonquery.DialectMySQL).QualifiedTable(database, table)).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var name, ddl string
	if !rows.Next() {
		return nil, fmt.Errorf("StarRocks table definition missing")
	}
	if err := rows.Scan(&name, &ddl); err != nil {
		return nil, err
	}
	if name != table || rows.Next() {
		return nil, fmt.Errorf("StarRocks table definition identity mismatch")
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	keys, err := primaryKey(ddl)
	if err != nil {
		return nil, err
	}
	return &datatype.TableInfo{Name: name, Kind: "table", PrimaryKey: keys, Native: map[string]interface{}{"engine": "StarRocks"}}, nil
}

var decimalDeclaration = regexp.MustCompile(`(?i)^decimal\((\d+),\s*(\d+)\)$`)

func (*Plugin) listColumns(ctx context.Context, db *gorm.DB, database, table string) ([]datatype.FieldInfo, error) {
	rows, err := db.WithContext(ctx).Raw("DESC " + commonquery.ForDialect(commonquery.DialectMySQL).QualifiedTable(database, table)).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fields := []datatype.FieldInfo{}
	for rows.Next() {
		var name, native, nullable, sortKey string
		var defaultValue, extra sql.NullString
		if err := rows.Scan(&name, &native, &nullable, &sortKey, &defaultValue, &extra); err != nil {
			return nil, err
		}
		field := datatype.FieldInfo{Name: name, NativeType: native, Type: nativeFieldType(native), Nullable: strings.EqualFold(nullable, "YES"), OrdinalPosition: len(fields) + 1}
		if decimal := decimalDeclaration.FindStringSubmatch(strings.TrimSpace(native)); decimal != nil {
			field.Precision, _ = strconv.Atoi(decimal[1])
			field.Scale, _ = strconv.Atoi(decimal[2])
		}
		fields = append(fields, field)
	}
	return fields, rows.Err()
}
func (*Plugin) rowCount(ctx context.Context, db *gorm.DB, database, table string) (int64, error) {
	var count int64
	err := db.WithContext(ctx).Raw("SELECT COUNT(*) FROM " + commonquery.ForDialect(commonquery.DialectMySQL).QualifiedTable(database, table)).Scan(&count).Error
	return count, err
}

var primaryKeyLine = regexp.MustCompile("(?m)^\\s*PRIMARY KEY\\s*\\((.*)\\)\\s*$")

func primaryKey(ddl string) ([]string, error) {
	matches := primaryKeyLine.FindAllStringSubmatch(ddl, -1)
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("ambiguous StarRocks primary key")
	}
	input := strings.TrimSpace(matches[0][1])
	keys := []string{}
	seen := map[string]bool{}
	for input != "" {
		if input[0] != '`' {
			return nil, fmt.Errorf("unquoted StarRocks primary key identifier")
		}
		input = input[1:]
		var key strings.Builder
		closed := false
		for len(input) > 0 {
			b := input[0]
			input = input[1:]
			if b == '`' {
				if len(input) > 0 && input[0] == '`' {
					key.WriteByte('`')
					input = input[1:]
					continue
				}
				closed = true
				break
			}
			key.WriteByte(b)
		}
		if !closed || key.Len() == 0 || seen[key.String()] {
			return nil, fmt.Errorf("invalid StarRocks primary key identifier")
		}
		seen[key.String()] = true
		keys = append(keys, key.String())
		input = strings.TrimSpace(input)
		if input == "" {
			break
		}
		if input[0] != ',' {
			return nil, fmt.Errorf("invalid StarRocks primary key separator")
		}
		input = strings.TrimSpace(input[1:])
		if input == "" {
			return nil, fmt.Errorf("empty StarRocks primary key identifier")
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("empty StarRocks primary key")
	}
	return keys, nil
}
