package query

import "testing"

func TestQuoteIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		engineType string
		identifier string
		want       string
	}{
		{name: "postgresql double quote", engineType: "postgresql", identifier: `city"name`, want: `"city""name"`},
		{name: "mysql backtick", engineType: "mysql", identifier: "city`name", want: "`city``name`"},
		{name: "clickhouse backtick", engineType: "clickhouse", identifier: "events", want: "`events`"},
		{name: "default double quote", engineType: "unknown", identifier: "Events", want: `"Events"`},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ForEngine(tt.engineType).QuoteIdentifier(tt.identifier); got != tt.want {
				t.Fatalf("QuoteIdentifier() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSelectTableSQL(t *testing.T) {
	t.Parallel()

	got := ForEngine("postgresql").SelectTableSQL(`"id", "name"`, "public", "Cities", `name IS NOT NULL`, `id DESC`, 10, 20)
	want := `SELECT "id", "name" FROM "public"."Cities" WHERE name IS NOT NULL ORDER BY id DESC LIMIT 10 OFFSET 20`
	if got != want {
		t.Fatalf("SelectTableSQL() = %q, want %q", got, want)
	}
}

func TestOracleSelectTableSQLUsesFetchPagination(t *testing.T) {
	t.Parallel()

	got := ForEngine("oracle").SelectTableSQL("*", "BUSINESS", "ORDERS", "", `"ID"`, 10, 20)
	want := `SELECT * FROM "BUSINESS"."ORDERS" ORDER BY "ID" OFFSET 20 ROWS FETCH NEXT 10 ROWS ONLY`
	if got != want {
		t.Fatalf("SelectTableSQL() = %q, want %q", got, want)
	}
}

func TestCountTableSQL(t *testing.T) {
	t.Parallel()

	got := ForEngine("mysql").CountTableSQL("analytics", "events", "kind = 'click'")
	want := "SELECT COUNT(*) AS total FROM `analytics`.`events` WHERE kind = 'click'"
	if got != want {
		t.Fatalf("CountTableSQL() = %q, want %q", got, want)
	}
}

func TestPaginateQuerySQL(t *testing.T) {
	t.Parallel()

	got := PaginateQuerySQL("SELECT * FROM t", 50, 100)
	want := "SELECT * FROM (SELECT * FROM t) AS addp_page LIMIT 50 OFFSET 100"
	if got != want {
		t.Fatalf("PaginateQuerySQL() = %q, want %q", got, want)
	}
}

func TestPaginateQuerySQLWrapsExistingLimit(t *testing.T) {
	t.Parallel()

	query := "SELECT *\nFROM Business_PostgreSQL.addp_transfer.apply_positions\nLIMIT 10"
	got := PaginateQuerySQL(query, 10, 0)
	want := "SELECT * FROM (SELECT *\nFROM Business_PostgreSQL.addp_transfer.apply_positions\nLIMIT 10) AS addp_page LIMIT 10"
	if got != want {
		t.Fatalf("PaginateQuerySQL() = %q, want %q", got, want)
	}
}

func TestOraclePaginateQuerySQLUsesOracleAliasAndFetch(t *testing.T) {
	t.Parallel()

	got := ForEngine("oracle").PaginateQuerySQL("SELECT * FROM ORDERS;", 50, 100)
	want := "SELECT * FROM (SELECT * FROM ORDERS) addp_page OFFSET 100 ROWS FETCH NEXT 50 ROWS ONLY"
	if got != want {
		t.Fatalf("PaginateQuerySQL() = %q, want %q", got, want)
	}
}

func TestDialectSubqueryAliasAndAppendPagination(t *testing.T) {
	t.Parallel()

	if got := ForEngine("postgresql").SubqueryAlias("source"); got != " AS source" {
		t.Fatalf("PostgreSQL alias = %q", got)
	}
	if got := ForEngine("oracle").SubqueryAlias("source"); got != " source" {
		t.Fatalf("Oracle alias = %q", got)
	}
	if got := ForEngine("oracle").AppendPaginationSQL("SELECT 1 FROM DUAL;", 5, 0); got != "SELECT 1 FROM DUAL FETCH FIRST 5 ROWS ONLY" {
		t.Fatalf("Oracle pagination = %q", got)
	}
}

func TestSelectAllSampleSQLWithoutLimitReturnsBaseQuery(t *testing.T) {
	t.Parallel()

	got := SelectAllSampleSQL("mysql", "business", "orders", 0)
	want := "SELECT *\nFROM `business`.`orders`"
	if got != want {
		t.Fatalf("SelectAllSampleSQL() = %q, want %q", got, want)
	}
}

func TestSelectAllSampleSQLUsesOracleFetch(t *testing.T) {
	t.Parallel()

	got := SelectAllSampleSQL("oracle", "BUSINESS", "ORDERS", 20)
	want := "SELECT *\nFROM \"BUSINESS\".\"ORDERS\"\nFETCH FIRST 20 ROWS ONLY"
	if got != want {
		t.Fatalf("SelectAllSampleSQL() = %q, want %q", got, want)
	}
}
