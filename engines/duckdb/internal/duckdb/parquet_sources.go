package duckdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Freeze a directory once per preparation. Only this list is given to the
// reader and to DuckDB's allowed_paths. glob lists names, without binding a
// parquet scan or opening any business records.
func parquetMembers(ctx context.Context, conn *sql.Conn, location string) ([]string, error) {
	if strings.ContainsAny(location, "*?[") {
		return nil, fmt.Errorf("Parquet location contains a pattern character")
	}
	if strings.HasSuffix(strings.ToLower(location), ".parquet") {
		return []string{location}, nil
	}
	prefix := strings.TrimRight(location, "/") + "/"
	rows, err := conn.QueryContext(ctx, "SELECT file FROM glob(CAST(? AS VARCHAR)) ORDER BY file", prefix+"*.parquet")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []string
	for rows.Next() {
		var file string
		if err := rows.Scan(&file); err != nil {
			return nil, err
		}
		leaf, ok := strings.CutPrefix(file, prefix)
		if !ok || leaf == "" || strings.ContainsAny(leaf, "/*?[") || !strings.HasSuffix(leaf, ".parquet") {
			return nil, fmt.Errorf("invalid member of Parquet directory")
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("Parquet directory contains no matching files")
	}
	return files, nil
}

type parsedSelect struct {
	Error      bool             `json:"error"`
	Statements []map[string]any `json:"statements"`
}

func parseSelect(ctx context.Context, conn *sql.Conn, query string) (*parsedSelect, error) {
	var raw string
	if err := conn.QueryRowContext(ctx, "SELECT json_serialize_sql(CAST(? AS VARCHAR))::VARCHAR", query).Scan(&raw); err != nil {
		return nil, err
	}
	var ast parsedSelect
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&ast); err != nil {
		return nil, err
	}
	if ast.Error || len(ast.Statements) != 1 {
		return nil, fmt.Errorf("DuckDB could not parse one SELECT statement")
	}
	return &ast, nil
}

func visitBaseTables(value any, visit func(map[string]any) error) error {
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			if err := visitBaseTables(child, visit); err != nil {
				return err
			}
		}
	case map[string]any:
		if node["type"] == "BASE_TABLE" {
			return visit(node)
		}
		for _, child := range node {
			if err := visitBaseTables(child, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func prepareObjectTables(ctx context.Context, conn *sql.Conn, query string, tables map[string]map[string]string) (string, []string, error) {
	if len(tables) == 0 {
		return query, nil, nil
	}
	ast, err := parseSelect(ctx, conn, query)
	if err != nil {
		return "", nil, err
	}
	members := make(map[string][]string)
	var allowed []string
	err = visitBaseTables(ast.Statements[0], func(node map[string]any) error {
		engine, _ := node["catalog_name"].(string)
		schema, _ := node["schema_name"].(string)
		table, _ := node["table_name"].(string)
		if engine == "" {
			engine, schema = schema, ""
		}
		byName := tables[engine]
		location := byName[table]
		if schema != "" && byName[schema+"."+table] != "" {
			location = byName[schema+"."+table]
		}
		if location == "" {
			return nil
		}
		if !strings.HasPrefix(location, "s3://") && !strings.HasPrefix(location, "https://") && !strings.HasPrefix(location, "http://") {
			if strings.Contains(location, "://") {
				return fmt.Errorf("unsupported Parquet location")
			}
			location = "s3://" + location
		}
		files, found := members[location]
		if !found {
			files, err = parquetMembers(ctx, conn, location)
			if err != nil {
				return err
			}
			members[location] = files
			allowed = append(allowed, files...)
		}
		// Have the pinned native parser construct the table function node.
		// Keep query aliases, samples and column aliases at their original site.
		reader, err := parseSelect(ctx, conn, "SELECT * FROM read_parquet("+duckDBStringList(files)+")")
		if err != nil {
			return err
		}
		selectNode, _ := reader.Statements[0]["node"].(map[string]any)
		call, _ := selectNode["from_table"].(map[string]any)
		if call["type"] != "TABLE_FUNCTION" || node["at_clause"] != nil {
			return fmt.Errorf("unsupported Parquet table reference")
		}
		call["alias"], call["sample"], call["column_name_alias"] = node["alias"], node["sample"], node["column_name_alias"]
		if call["alias"] == "" {
			call["alias"] = table
		}
		clear(node)
		for key, value := range call {
			node[key] = value
		}
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	if len(allowed) == 0 {
		return query, nil, nil
	}
	raw, err := json.Marshal(ast)
	if err != nil {
		return "", nil, err
	}
	var rewritten string
	if err := conn.QueryRowContext(ctx, "SELECT json_deserialize_sql(CAST(? AS JSON))", string(raw)).Scan(&rewritten); err != nil {
		return "", nil, err
	}
	slices.Sort(allowed)
	return rewritten, slices.Compact(allowed), nil
}
