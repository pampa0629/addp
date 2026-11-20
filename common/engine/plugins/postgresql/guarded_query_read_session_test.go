package postgresql

import (
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestGuardedPostgresQueryAdmission(t *testing.T) {
	for _, test := range []struct {
		query    string
		sources  int
		accepted bool
	}{
		{"SELECT 1", 0, true},
		{"SELECT CAST($1 AS TEXT) AS bound_text FROM public.source WHERE id >= $2 ORDER BY id", 1, true},
		{"SELECT p.id FROM public.persons p WHERE p.id=$1 ORDER BY p.id LIMIT 2", 1, true},
		{"WITH x AS (SELECT id FROM public.a) SELECT x.id FROM x JOIN public.b b ON x.id=b.id", 2, true},
		{"SELECT * FROM public.a UNION ALL SELECT * FROM public.a", 1, true},
		{"SELECT public.hidden_read()", 0, false},
		{"SELECT * FROM persons", 0, false},
		{"SELECT * FROM public.persons; SELECT 1", 0, false},
		{"SELECT id::public.custom_type FROM public.persons", 0, false},
		{"SELECT * FROM public.persons TABLESAMPLE SYSTEM (10)", 0, false},
		{"DELETE FROM public.persons RETURNING id", 0, false},
	} {
		t.Run(test.query, func(t *testing.T) {
			plan, err := prepareGuardedPostgresQuery(t.Context(), plugin.QueryRequest{EngineID: 9, Language: "sql", Query: test.query, Options: plugin.QueryOptions{ReadOnly: true}}, nil)
			query, set := plan.sql, plan.readSet
			if !test.accepted {
				if !errors.Is(err, plugin.ErrQueryReadSetUnresolved) || set != nil {
					t.Fatalf("unproven query admitted: %q %+v %v", query, set, err)
				}
				return
			}
			if err != nil || set == nil || len(set.Paths) != test.sources {
				t.Fatalf("sources=%+v err=%v", set, err)
			}
			if test.sources > 0 && !strings.Contains(query, "ONLY public.") {
				t.Fatal("ordinary source not bound with ONLY", query)
			}
			if strings.Contains(test.query, "p.id=$1") && !strings.Contains(query, "OPERATOR(pg_catalog.=)") {
				t.Fatal("operator resolution was not bound", query)
			}
		})
	}
}
