package spark_sql

import "testing"

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
