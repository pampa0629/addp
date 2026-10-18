package postgresql

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/common/engine/plugin"
	pgquery "github.com/pganalyze/pg_query_go/v6"
)

func TestSessionProofExpressionSliceDoesNotGuessHiddenDependencies(t *testing.T) {
	for _, tc := range []struct {
		query string
		ok    bool
	}{
		{`SELECT a.id FROM s.a a JOIN s.b b ON a.id=b.id WHERE a.id >= $1`, true},
		{`SELECT a || b FROM s.t`, true},
		{`SELECT pg_catalog.concat(pg_catalog.left(a,1),pg_catalog.repeat($1::pg_catalog.text,pg_catalog.char_length(a) OPERATOR(pg_catalog.-) 2),pg_catalog.right(a,1)) FROM s.t`, true},
		{`SELECT a OPERATOR(pg_catalog.<=) 2 FROM s.t`, true},
		{`SELECT pg_catalog.left(public.hidden(a),1) FROM s.t`, false},
		{`SELECT pg_catalog.lower(a) FROM s.t`, false},
		{`SELECT left(a,1) FROM s.t`, false},
		{`SELECT public.left(a,1) FROM s.t`, false},
		{`SELECT $1::public.text FROM s.t`, false},
		{`SELECT a::pg_catalog.text FROM s.t`, false},
		{`SELECT $1::pg_catalog.text[] FROM s.t`, false},
		{`SELECT public.hidden(id) FROM s.t`, false},
		{`SELECT id::public.hidden FROM s.t`, false},
		{`SELECT id COLLATE public.hidden FROM s.t`, false},
		{`SELECT a OPERATOR(public.+) b FROM s.t`, false},
		{`SELECT a @@@ b FROM s.t`, false},
		{`SELECT a FROM s.t ORDER BY a USING OPERATOR(public.<)`, false},
		{`SELECT a FROM s.t TABLESAMPLE public.hidden(1)`, false},
	} {
		parsed, err := pgquery.Parse(tc.query)
		if err != nil {
			t.Fatal(err)
		}
		if got := sessionTransparentMessage(parsed.ProtoReflect()); got != tc.ok {
			t.Fatalf("proof slice query=%s got=%v want=%v", tc.query, got, tc.ok)
		}
	}
}

func TestSessionProofIsImmutableAndUnavailableIsNotEmptySuccess(t *testing.T) {
	set, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(9, "schema", "outdoor", "persons"))
	for _, session := range []plugin.ReadSessionEvidence{&postgresTableReadSession{readSet: set}, &postgresQueryReadSession{readSet: set}} {
		got, err := session.ReadSet(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got.Paths[0].Segments[2].Name = "changed"
		again, err := session.ReadSet(context.Background())
		if err != nil || again.Paths[0].Segments[2].Name != "persons" {
			t.Fatal("session evidence aliases caller memory", err)
		}
	}
	for _, session := range []plugin.ReadSessionEvidence{&postgresTableReadSession{}, &postgresQueryReadSession{}} {
		if set, err := session.ReadSet(context.Background()); set != nil || !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
			t.Fatal("missing proof became an empty successful set", set, err)
		}
	}
}
