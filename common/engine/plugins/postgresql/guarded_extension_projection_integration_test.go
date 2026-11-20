package postgresql

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/twpayne/go-geom/encoding/ewkb"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestIntegrationPostgresQueryReadSessionFarmland(t *testing.T) {
	db, pg, conn := openPostgresPrepareIntegration(t, true)
	defer db.Close()
	ctx := t.Context()
	schema := fmt.Sprintf("farmland_it_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	// A preinstalled extension belongs to the environment. If missing, install
	// it in the fixture schema so dropping that schema removes it as well.
	if _, err := db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS hstore WITH SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	var hstoreSchema string
	if err := db.QueryRowContext(ctx, "SELECT quote_ident(n.nspname) FROM pg_catalog.pg_extension e JOIN pg_catalog.pg_namespace n ON n.oid=e.extnamespace WHERE e.extname='hstore'").Scan(&hstoreSchema); err != nil {
		t.Fatal(err)
	}
	table := schema + ".farmland"
	for _, sql := range []string{
		"CREATE TABLE " + table + ` ("SmID" integer, owner_name text, geom geometry(Point,4326), labels ` + hstoreSchema + ".hstore)",
		"INSERT INTO " + table + " VALUES (17,'private fixture name',ST_SetSRID(ST_MakePoint(1,2),4326),'crop=>corn')",
		"CREATE TABLE " + schema + ".ungranted (id integer)",
	} {
		if _, err := db.ExecContext(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	want := plugin.TabularItemPath(91, "schema", schema, "farmland")
	allowed := true
	denied := errors.New("fixture source read denied")
	check := func(_ context.Context, set *plugin.QueryReadSet) error {
		for _, path := range set.Paths {
			if !allowed || !reflect.DeepEqual(path, want) {
				return denied
			}
		}
		if len(set.Paths) == 0 {
			t.Fatal("missing actual farmland source")
		}
		return nil
	}
	open := func(t *testing.T, sql string, mask bool) (plugin.GuardedQueryReadSession, error) {
		t.Helper()
		prepared, err := pg.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 91, Language: "sql", Query: sql, Options: plugin.QueryOptions{ReadOnly: true}})
		if err != nil {
			t.Fatal(err)
		}
		if mask {
			err := prepared.(plugin.OutputTransformPreparedQuery).BindOutputTransforms(ctx, []plugin.QueryOutputTransform{{Column: "owner_name", Kind: "constant_text", Replacement: "[protected]"}})
			if err != nil {
				t.Fatal(err)
			}
		}
		return pg.OpenGuardedQueryReadSession(ctx, prepared, check)
	}
	single := `SELECT "SmID" FROM ` + table + " LIMIT 1"
	for _, tc := range []struct {
		name, query string
		mask        bool
	}{
		{"single", single, false}, {"star", "SELECT * FROM " + table + " LIMIT 1", false},
		{"masked_star", "SELECT * FROM " + table + " LIMIT 1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, err := open(t, tc.query, tc.mask)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close(ctx)
			batch, err := session.ReadBatch(ctx, 2)
			if err != nil {
				t.Fatal(err)
			}
			if len(batch.Rows) != 1 || fmt.Sprint(batch.Rows[0]["SmID"]) != "17" {
				t.Fatalf("unexpected row: %v", batch.Rows)
			}
			if tc.name == "single" {
				if len(batch.Rows[0]) != 1 {
					t.Fatal("extra output columns")
				}
				return
			}
			rows, err := datatype.QueryRowsForJSON(batch.Fields, batch.Rows)
			if err != nil {
				t.Fatal(err)
			}
			shape, ok := rows[0]["geom"].(datatype.GeometryValue)
			if !ok || shape.WKT != "POINT (1 2)" || shape.SRID != 4326 {
				t.Fatalf("incorrect geometry: %v", rows)
			}
			labels := fmt.Sprint(batch.Rows[0]["labels"])
			if raw, ok := batch.Rows[0]["labels"].([]byte); ok {
				labels = string(raw)
			}
			if labels != `"crop"=>"corn"` {
				t.Fatalf("incorrect hstore projection: %q", labels)
			}
			wantName := "private fixture name"
			if tc.mask {
				wantName = "[protected]"
			}
			if rows[0]["owner_name"] != wantName {
				t.Fatal("incorrect owner_name projection")
			}
		})
	}
	const transform = "ST_Transform(geom,3857)"
	projectedQuery := "SELECT " + transform + " AS projected FROM " + table + " LIMIT 1"
	for _, tc := range []struct{ name, expression string }{
		{"coordinate_transform", transform},
		{"transform_with_dictionary", "ST_Transform(geom,'+proj=longlat +datum=WGS84',3857)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, err := open(t, "SELECT "+tc.expression+" AS projected FROM "+table+" LIMIT 1", false)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close(ctx)
			batch, err := session.ReadBatch(ctx, 2)
			if err != nil || len(batch.Rows) != 1 {
				t.Fatalf("transform result=%v err=%v", batch, err)
			}
			geometry, err := ewkb.Unmarshal(batch.Rows[0]["projected"].([]byte))
			if err != nil || geometry.SRID() != 3857 || len(geometry.FlatCoords()) != 2 ||
				math.Abs(geometry.FlatCoords()[0]-111319.490793) > 0.001 || math.Abs(geometry.FlatCoords()[1]-222684.208506) > 0.001 {
				t.Fatalf("incorrect projected point %v, %v", geometry, err)
			}
		})
	}
	t.Run("geometry_wkt", func(t *testing.T) {
		session, err := open(t, "SELECT ST_AsText(geom) AS wkt FROM "+table+" LIMIT 1", false)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(ctx)
		batch, err := session.ReadBatch(ctx, 2)
		if err != nil || batch == nil || len(batch.Rows) != 1 || batch.Rows[0]["wkt"] != "POINT(1 2)" {
			t.Fatalf("geometry WKT projection: batch=%v err=%v", batch, err)
		}
	})
	t.Run("transformed_srid", func(t *testing.T) {
		session, err := open(t, "SELECT ST_SRID("+transform+") AS target_srid FROM "+table+" LIMIT 1", false)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(ctx)
		batch, err := session.ReadBatch(ctx, 2)
		if err != nil || len(batch.Rows) != 1 || fmt.Sprint(batch.Rows[0]["target_srid"]) != "3857" {
			t.Fatalf("unexpected transformed SRID: batch=%v err=%v", batch, err)
		}
	})
	t.Run("unrelated_overload_in_search_path", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, "CREATE FUNCTION "+schema+".st_srid(text) RETURNS integer LANGUAGE sql VOLATILE AS 'SELECT 1'"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, "SELECT pg_catalog.set_config('search_path', $1 || ',' || current_setting('search_path'), true)", schema); err != nil {
			t.Fatal(err)
		}
		catalog := &postgresDatabaseReadCatalog{db: tx}
		req := plugin.QueryRequest{EngineID: 91, Query: "SELECT ST_SRID(" + transform + ") FROM " + table, Options: plugin.QueryOptions{ReadOnly: true}}
		plan, err := prepareGuardedPostgresQuery(ctx, req, catalog)
		if err != nil {
			t.Fatal(err)
		}
		var srid int
		if err := tx.QueryRowContext(ctx, plan.sql).Scan(&srid); err != nil || srid != 3857 {
			t.Fatalf("overloaded SRID = %d, err=%v", srid, err)
		}
		req.Query = "SELECT ST_SRID('text') FROM " + table
		if _, err := prepareGuardedPostgresQuery(ctx, req, catalog); !errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
			t.Fatalf("unverified text overload: %v", err)
		}
	})
	t.Run("masked_field_with_transform", func(t *testing.T) {
		session, err := open(t, "SELECT owner_name, "+transform+" AS projected FROM "+table, true)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(ctx)
		batch, err := session.ReadBatch(ctx, 2)
		if err != nil || len(batch.Rows) != 1 || batch.Rows[0]["owner_name"] != "[protected]" {
			t.Fatalf("masked projection failed: %v", err)
		}
		value, ok := batch.Rows[0]["projected"].([]byte)
		if !ok {
			t.Fatal("missing projected geometry")
		}
		point, err := ewkb.Unmarshal(value)
		if err != nil || point.SRID() != 3857 {
			t.Fatalf("incorrect projected geometry %v, %v", point, err)
		}
	})
	t.Run("hstore_value", func(t *testing.T) {
		session, err := open(t, "SELECT "+hstoreSchema+".fetchval(labels,'crop') AS crop FROM "+table, false)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(ctx)
		batch, err := session.ReadBatch(ctx, 2)
		if err != nil || len(batch.Rows) != 1 || batch.Rows[0]["crop"] != "corn" {
			t.Fatalf("hstore value: %v, %v", batch, err)
		}
	})
	t.Run("unresolved_table_function", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS tablefunc WITH SCHEMA "+schema); err != nil {
			t.Fatal(err)
		}
		var operation string
		if err := db.QueryRowContext(ctx, "SELECT format('%I.crosstab', n.nspname) FROM pg_catalog.pg_extension e JOIN pg_catalog.pg_namespace n ON n.oid=e.extnamespace WHERE e.extname='tablefunc'").Scan(&operation); err != nil {
			t.Fatal(err)
		}
		query := "SELECT * FROM " + operation + "('SELECT id, 1, 2 FROM " + schema + ".ungranted') AS t(id integer, value integer)"
		prepared, err := pg.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 91, Language: "sql", Query: query, Options: plugin.QueryOptions{ReadOnly: true}})
		if err != nil {
			t.Fatal(err)
		}
		set, err := prepared.ReadSet(ctx)
		detail := plugin.QueryErrorContextOf(err)
		if set != nil || !errors.Is(err, plugin.ErrQueryReadSetUnresolved) || detail == nil || detail.Object == nil || detail.Object.Kind != "function" || !strings.HasSuffix(detail.Object.Name, `."crosstab"`) {
			t.Fatalf("expected only unresolved function identity, got %v, %v", detail, err)
		}
	})
	for _, tc := range []struct{ name, query string }{
		{"ungranted_join", "SELECT f.* FROM " + table + " f JOIN " + schema + `.ungranted u ON f."SmID"=u.id`},
		{"revoked_transform", projectedQuery},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "revoked_transform" {
				allowed = false
			}
			session, err := open(t, tc.query, false)
			if session != nil {
				session.Close(ctx)
				t.Fatal("unexpected content session")
			}
			if !errors.Is(err, denied) {
				t.Fatalf("expected source decision, got %v", err)
			}
		})
	}
}
