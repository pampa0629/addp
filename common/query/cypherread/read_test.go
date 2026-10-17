package cypherread

import (
	"strings"
	"testing"
)

func TestBoundedCypherUsesASTAndKeepsOriginalValues(t *testing.T) {
	for _, tc := range []struct {
		query, want string
		params      map[string]interface{}
	}{
		{"MATCH (n) RETURN n", "MATCH (n) RETURN n LIMIT 3", nil},
		{"RETURN 'LIMIT 500 中文' AS text; // tail", "RETURN 'LIMIT 500 中文' AS text LIMIT 3", nil},
		{"RETURN 1 AS n LIMIT 1", "RETURN 1 AS n LIMIT 1", nil},
		{"RETURN 1 AS n LIMIT 100", "RETURN 1 AS n LIMIT 3", nil},
		{"RETURN $row_limit AS n LIMIT $row_limit", "RETURN $row_limit AS n LIMIT 3", map[string]interface{}{"row_limit": int64(100)}},
		{"RETURN 1 AS n UNION RETURN 2 AS n", "RETURN 1 AS n LIMIT 3 UNION RETURN 2 AS n LIMIT 3", nil},
	} {
		p, err := ParseLocalRead(tc.query)
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.BoundedStatement(3, tc.params)
		if err != nil || got != tc.want {
			t.Fatalf("%q -> %q %v", tc.query, got, err)
		}
	}
	for _, q := range []string{"RETURN 1 LIMIT 1+1", "RETURN 1 LIMIT $size", "RETURN 1 LIMIT -1"} {
		p, err := ParseLocalRead(q)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = p.BoundedStatement(3, nil); err == nil {
			t.Fatalf("unsupported limit accepted: %s", q)
		}
	}
	if _, err := ParseLocalRead(strings.Repeat("x", 256<<10+1)); err == nil {
		t.Fatal("oversize query accepted")
	}
}
