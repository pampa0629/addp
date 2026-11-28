package oracle

import (
	"errors"
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestOracleQueryReadSet(t *testing.T) {
	const engineID = 5
	path := func(schema, name string) plugin.EngineCatalogPath {
		return plugin.TabularItemPath(engineID, plugin.EngineCatalogTermSchema, schema, name)
	}
	for _, tc := range []struct {
		name, sql string
		want      []plugin.EngineCatalogPath
		deny      bool
	}{
		{"quoted_names", `SELECT id FROM "Report.Owner"."Event.Rows"`, []plugin.EngineCatalogPath{path("Report.Owner", "Event.Rows")}, false},
		{"unqualified_joins", `WITH Choice AS (SELECT id FROM Local_Tab) SELECT a.id FROM Choice a JOIN OTHER.Local_Tab b ON a.id=b.id`,
			[]plugin.EngineCatalogPath{path("READER", "LOCAL_TAB"), path("OTHER", "LOCAL_TAB")}, false},
		{"sequence_is_not_a_table_read", `SELECT demo_seq.NEXTVAL FROM SYS.DUAL`, nil, true},
		{"view_function_dependency_unproven", `SELECT application_package.summary() FROM report.orders`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := inspectOracleReadSQL(tc.sql, "READER", engineID, false)
			if tc.deny {
				if !errors.Is(err, plugin.ErrQueryReadSetUnresolved) || got != nil {
					t.Fatalf("unsupported source=%+v error=%v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want, err := plugin.NewQueryReadSet(tc.want...)
			if err != nil || !reflect.DeepEqual(got.Paths, want.Paths) {
				t.Fatalf("resolved %+v, want %+v, err=%v", got, want, err)
			}
		})
	}
}
