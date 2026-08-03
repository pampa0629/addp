package dbbridge

import "testing"

func TestSupportsReadOnlySQLExecution(t *testing.T) {
	tests := []struct {
		engineType string
		want       bool
	}{
		{engineType: "postgresql", want: true},
		{engineType: "PostgreSQL", want: true},
		{engineType: "mysql", want: true},
		{engineType: "doris", want: true},
		{engineType: "clickhouse", want: false},
		{engineType: "spark", want: true},
		{engineType: "mongodb", want: false},
	}
	for _, test := range tests {
		t.Run(test.engineType, func(t *testing.T) {
			if got := SupportsReadOnlySQLExecution(test.engineType); got != test.want {
				t.Fatalf("SupportsReadOnlySQLExecution(%q) = %v, want %v", test.engineType, got, test.want)
			}
		})
	}
}
