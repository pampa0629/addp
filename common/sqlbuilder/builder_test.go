package sqlbuilder

import (
	"fmt"
	"testing"
)

func TestQuoteIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"小写列名", "id", `"id"`},
		{"混合大小写", "SmID", `"SmID"`},
		{"全大写", "OBJECTID", `"OBJECTID"`},
		{"包含双引号", `my"column`, `"my""column"`},
		{"空字符串", "", `""`},
		{"下划线", "user_id", `"user_id"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := QuoteIdentifier(tt.input)
			if result != tt.expected {
				t.Errorf("QuoteIdentifier(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestQuoteIdentifiers(t *testing.T) {
	input := []string{"SmID", "Name", "SmGeometry"}
	expected := []string{`"SmID"`, `"Name"`, `"SmGeometry"`}
	result := QuoteIdentifiers(input)

	if len(result) != len(expected) {
		t.Fatalf("QuoteIdentifiers() returned %d items, want %d", len(result), len(expected))
	}

	for i, exp := range expected {
		if result[i] != exp {
			t.Errorf("QuoteIdentifiers()[%d] = %q, want %q", i, result[i], exp)
		}
	}
}

func TestQualifiedTableName(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		table    string
		expected string
	}{
		{"有 schema", "public", "MyTable", `"public"."MyTable"`},
		{"无 schema", "", "MyTable", `"MyTable"`},
		{"混合大小写 schema", "MySchema", "MyTable", `"MySchema"."MyTable"`},
		{"SuperMap 表", "public", "dltb", `"public"."dltb"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := QualifiedTableName(tt.schema, tt.table)
			if result != tt.expected {
				t.Errorf("QualifiedTableName(%q, %q) = %q, want %q", tt.schema, tt.table, result, tt.expected)
			}
		})
	}
}

func TestGeometryTransform(t *testing.T) {
	tests := []struct {
		name       string
		geomColumn string
		targetSRID int
		expected   string
	}{
		{"标准几何列", "geom", 3857, `ST_Transform("geom", 3857)`},
		{"SuperMap 几何列", "SmGeometry", 3857, `ST_Transform("SmGeometry", 3857)`},
		{"ArcGIS 几何列", "SHAPE", 4326, `ST_Transform("SHAPE", 4326)`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GeometryTransform(tt.geomColumn, tt.targetSRID)
			if result != tt.expected {
				t.Errorf("GeometryTransform(%q, %d) = %q, want %q", tt.geomColumn, tt.targetSRID, result, tt.expected)
			}
		})
	}
}

func TestSelectColumns(t *testing.T) {
	tests := []struct {
		name     string
		columns  []string
		expected string
	}{
		{"单列", []string{"id"}, `"id"`},
		{"多列", []string{"SmID", "Name", "SmGeometry"}, `"SmID", "Name", "SmGeometry"`},
		{"空列表", []string{}, ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SelectColumns(tt.columns)
			if result != tt.expected {
				t.Errorf("SelectColumns(%v) = %q, want %q", tt.columns, result, tt.expected)
			}
		})
	}
}

func TestWhereClause(t *testing.T) {
	tests := []struct {
		name       string
		conditions []string
		expected   string
	}{
		{"单条件", []string{"id > 10"}, " WHERE id > 10"},
		{"多条件", []string{"id > 10", "name IS NOT NULL"}, " WHERE id > 10 AND name IS NOT NULL"},
		{"空条件", []string{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := WhereClause(tt.conditions)
			if result != tt.expected {
				t.Errorf("WhereClause(%v) = %q, want %q", tt.conditions, result, tt.expected)
			}
		})
	}
}

func TestCreateTableSQL(t *testing.T) {
	tests := []struct {
		name        string
		schema      string
		table       string
		columnDefs  []string
		ifNotExists bool
		expected    string
	}{
		{
			"基础表创建",
			"public",
			"MyTable",
			[]string{"id SERIAL PRIMARY KEY", "name TEXT"},
			false,
			`CREATE TABLE "public"."MyTable" (id SERIAL PRIMARY KEY, name TEXT)`,
		},
		{
			"IF NOT EXISTS",
			"public",
			"MyTable",
			[]string{"id SERIAL"},
			true,
			`CREATE TABLE IF NOT EXISTS "public"."MyTable" (id SERIAL)`,
		},
		{
			"无 schema",
			"",
			"MyTable",
			[]string{"id SERIAL"},
			false,
			`CREATE TABLE "MyTable" (id SERIAL)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CreateTableSQL(tt.schema, tt.table, tt.columnDefs, tt.ifNotExists)
			if result != tt.expected {
				t.Errorf("CreateTableSQL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestCreateIndexSQL(t *testing.T) {
	tests := []struct {
		name         string
		indexName    string
		schema       string
		table        string
		columns      []string
		indexType    string
		concurrently bool
		expected     string
	}{
		{
			"GIST 空间索引",
			"idx_geom",
			"public",
			"MyTable",
			[]string{"SmGeometry"},
			"GIST",
			false,
			`CREATE INDEX "idx_geom" ON "public"."MyTable" USING GIST ("SmGeometry")`,
		},
		{
			"CONCURRENTLY 索引",
			"idx_name",
			"public",
			"MyTable",
			[]string{"name"},
			"BTREE",
			true,
			`CREATE INDEX CONCURRENTLY "idx_name" ON "public"."MyTable" USING BTREE ("name")`,
		},
		{
			"无索引类型",
			"idx_id",
			"",
			"MyTable",
			[]string{"id"},
			"",
			false,
			`CREATE INDEX "idx_id" ON "MyTable" ("id")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CreateIndexSQL(tt.indexName, tt.schema, tt.table, tt.columns, tt.indexType, tt.concurrently)
			if result != tt.expected {
				t.Errorf("CreateIndexSQL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestAnalyzeTableSQL(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		table    string
		expected string
	}{
		{"有 schema", "public", "MyTable", `ANALYZE "public"."MyTable"`},
		{"无 schema", "", "MyTable", `ANALYZE "MyTable"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AnalyzeTableSQL(tt.schema, tt.table)
			if result != tt.expected {
				t.Errorf("AnalyzeTableSQL(%q, %q) = %q, want %q", tt.schema, tt.table, result, tt.expected)
			}
		})
	}
}

func TestDropTableSQL(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		table    string
		ifExists bool
		cascade  bool
		expected string
	}{
		{"基础删除", "public", "MyTable", false, false, `DROP TABLE "public"."MyTable"`},
		{"IF EXISTS", "public", "MyTable", true, false, `DROP TABLE IF EXISTS "public"."MyTable"`},
		{"CASCADE", "public", "MyTable", false, true, `DROP TABLE "public"."MyTable" CASCADE`},
		{"IF EXISTS CASCADE", "public", "MyTable", true, true, `DROP TABLE IF EXISTS "public"."MyTable" CASCADE`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DropTableSQL(tt.schema, tt.table, tt.ifExists, tt.cascade)
			if result != tt.expected {
				t.Errorf("DropTableSQL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestInsertSQL(t *testing.T) {
	tests := []struct {
		name                     string
		schema                   string
		table                    string
		columns                  []string
		placeholders             int
		useNumberedPlaceholders  bool
		expected                 string
	}{
		{
			"PostgreSQL 占位符",
			"public",
			"MyTable",
			[]string{"id", "name"},
			2,
			true,
			`INSERT INTO "public"."MyTable" ("id", "name") VALUES ($1, $2)`,
		},
		{
			"MySQL 占位符",
			"",
			"MyTable",
			[]string{"id", "name"},
			2,
			false,
			`INSERT INTO "MyTable" ("id", "name") VALUES (?, ?)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := InsertSQL(tt.schema, tt.table, tt.columns, tt.placeholders, tt.useNumberedPlaceholders)
			if result != tt.expected {
				t.Errorf("InsertSQL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSelectSQL(t *testing.T) {
	tests := []struct {
		name            string
		columns         []string
		schema          string
		table           string
		whereConditions []string
		orderBy         string
		limit           int
		offset          int
		expected        string
	}{
		{
			"基础查询",
			[]string{"id", "name"},
			"public",
			"MyTable",
			nil,
			"",
			0,
			0,
			`SELECT "id", "name" FROM "public"."MyTable"`,
		},
		{
			"SELECT *",
			[]string{},
			"public",
			"MyTable",
			nil,
			"",
			0,
			0,
			`SELECT * FROM "public"."MyTable"`,
		},
		{
			"完整查询",
			[]string{"id", "name"},
			"public",
			"MyTable",
			[]string{"id > 10", "name IS NOT NULL"},
			"id ASC",
			10,
			5,
			`SELECT "id", "name" FROM "public"."MyTable" WHERE id > 10 AND name IS NOT NULL ORDER BY id ASC LIMIT 10 OFFSET 5`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SelectSQL(tt.columns, tt.schema, tt.table, tt.whereConditions, tt.orderBy, tt.limit, tt.offset)
			if result != tt.expected {
				t.Errorf("SelectSQL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// 集成测试：模拟真实场景
func TestRealWorldScenarios(t *testing.T) {
	t.Run("SuperMap 数据 MVT 物化视图创建", func(t *testing.T) {
		schema := "public"
		table := "dltb"
		mvName := "dltb_mv3857"
		primaryKey := "SmID"
		geomColumn := "SmGeometry"

		// 构建 CREATE MATERIALIZED VIEW 语句
		selectClause := QuoteIdentifier(primaryKey)
		geomTransform := GeometryTransform(geomColumn, 3857)
		fromClause := QualifiedTableName(schema, table)
		whereClause := fmt.Sprintf(`WHERE %s IS NOT NULL`, QuoteIdentifier(geomColumn))

		expected := `CREATE MATERIALIZED VIEW "public"."dltb_mv3857" AS SELECT "SmID", ST_Transform("SmGeometry", 3857) AS geom_3857 FROM "public"."dltb" WHERE "SmGeometry" IS NOT NULL`
		result := fmt.Sprintf(`CREATE MATERIALIZED VIEW %s AS SELECT %s, %s AS geom_3857 FROM %s %s`,
			QualifiedTableName(schema, mvName), selectClause, geomTransform, fromClause, whereClause)

		if result != expected {
			t.Errorf("MVT 物化视图创建 SQL 不匹配\nGot:  %q\nWant: %q", result, expected)
		}
	})

	t.Run("ArcGIS 数据空间索引创建", func(t *testing.T) {
		schema := "public"
		table := "parcels"
		indexName := "idx_parcels_shape"
		geomColumn := "SHAPE"

		result := CreateIndexSQL(indexName, schema, table, []string{geomColumn}, "GIST", true)
		expected := `CREATE INDEX CONCURRENTLY "idx_parcels_shape" ON "public"."parcels" USING GIST ("SHAPE")`

		if result != expected {
			t.Errorf("空间索引创建 SQL 不匹配\nGot:  %q\nWant: %q", result, expected)
		}
	})
}
