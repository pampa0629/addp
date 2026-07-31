package spark_sql

import (
	"context"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestQuoteSparkIdentifier(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		want       string
	}{
		{name: "plain", identifier: "default", want: "`default`"},
		{name: "case sensitive", identifier: "SalesDB", want: "`SalesDB`"},
		{name: "escape backtick", identifier: "a`b", want: "`a``b`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quoteSparkIdentifier(tt.identifier); got != tt.want {
				t.Fatalf("quoteSparkIdentifier(%q) = %q, want %q", tt.identifier, got, tt.want)
			}
		})
	}
}

func TestSparkSQLTableInfoLeavesUnknownStatisticsEmpty(t *testing.T) {
	info := sparkSQLTableInfo("orders")
	if info.Name != "orders" || info.Kind != "table" {
		t.Fatalf("sparkSQLTableInfo() = %#v", info)
	}
	if info.RowCount != nil || info.SizeBytes != nil {
		t.Fatalf("Spark SQL list table stats = row:%#v size:%#v, want nil unknown stats", info.RowCount, info.SizeBytes)
	}
}

func TestSparkCommonFieldTypeMapsNativeTypes(t *testing.T) {
	tests := map[string]datatype.FieldType{
		"string":        datatype.FieldTypeString,
		"int":           datatype.FieldTypeInt,
		"bigint":        datatype.FieldTypeBigInt,
		"decimal(18,2)": datatype.FieldTypeDecimal,
		"array<string>": datatype.FieldTypeArray,
		"timestamp_ntz": datatype.FieldTypeTimestamp,
	}
	for nativeType, want := range tests {
		if got := sparkCommonFieldType(nativeType); got != want {
			t.Fatalf("sparkCommonFieldType(%q) = %q, want %q", nativeType, got, want)
		}
	}
}

func TestListChildrenUsesSparkThriftQueryForDatabases(t *testing.T) {
	t.Parallel()

	var queries []string
	spark := &SparkSQLPlugin{query: func(ctx context.Context, _ plugin.ConnectionInfo, query string) (*plugin.QueryResult, error) {
		queries = append(queries, query)
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("Spark catalog query context has no deadline")
		}
		return &plugin.QueryResult{Rows: []map[string]interface{}{
			{"namespace": "default"},
			{"namespace": "analytics"},
		}}, nil
	}}

	entries, err := spark.ListChildren(
		context.Background(),
		plugin.ConnectionInfo{"host": "spark", "port": 10000},
		plugin.CatalogRootPath(spark.CatalogModel(), 7),
		plugin.ListOptions{},
	)
	if err != nil {
		t.Fatalf("ListChildren() error = %v", err)
	}
	if !reflect.DeepEqual(queries, []string{"SHOW DATABASES"}) {
		t.Fatalf("queries = %#v", queries)
	}
	if len(entries) != 2 || entries[1].Name != "analytics" || entries[1].Path.StringPath() != "analytics" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestListChildrenUsesQualifiedSparkShowTables(t *testing.T) {
	t.Parallel()

	var query string
	spark := &SparkSQLPlugin{query: func(_ context.Context, _ plugin.ConnectionInfo, sql string) (*plugin.QueryResult, error) {
		query = sql
		return &plugin.QueryResult{Rows: []map[string]interface{}{
			{"namespace": "analytics", "tableName": "orders", "isTemporary": false},
		}}, nil
	}}

	entries, err := spark.ListChildren(
		context.Background(),
		plugin.ConnectionInfo{"host": "spark", "port": 10000},
		plugin.TabularNamespacePath(7, plugin.CatalogTermDatabase, "analytics"),
		plugin.ListOptions{},
	)
	if err != nil {
		t.Fatalf("ListChildren() error = %v", err)
	}
	if query != "SHOW TABLES IN `analytics`" {
		t.Fatalf("query = %q", query)
	}
	if len(entries) != 1 || entries[0].Name != "orders" || entries[0].Path.StringPath() != "analytics/orders" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestCreateConnectionPoolRejectsSparkThriftProtocol(t *testing.T) {
	t.Parallel()

	if _, err := (&SparkSQLPlugin{}).CreateConnectionPool(
		plugin.ConnectionInfo{"host": "spark", "port": 10000},
		nil,
	); err == nil {
		t.Fatal("CreateConnectionPool() error = nil, want unsupported protocol error")
	}
}
