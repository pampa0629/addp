package sqleffect

import "testing"

func TestRequireReadOnly(t *testing.T) {
	t.Parallel()

	for _, query := range []string{
		"SELECT * FROM orders",
		"WITH recent AS (SELECT * FROM orders) SELECT * FROM recent",
		"EXPLAIN SELECT * FROM orders",
	} {
		if err := RequireReadOnly(query); err != nil {
			t.Fatalf("RequireReadOnly(%q) error = %v", query, err)
		}
	}
	for _, query := range []string{
		"DELETE FROM orders",
		"SELECT * INTO archived_orders FROM orders",
		"SELECT * FROM orders; DELETE FROM orders",
	} {
		if err := RequireReadOnly(query); err == nil {
			t.Fatalf("RequireReadOnly(%q) error = nil, want rejection", query)
		}
	}
}
