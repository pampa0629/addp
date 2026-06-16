package service

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"testing"

	"github.com/addp/common/spatial"
	_ "github.com/lib/pq"
)

func TestExternal3857MaterializedViewCandidates(t *testing.T) {
	got := external3857MaterializedViewCandidates("dltb")
	want := []string{"dltb_mv3857", "dltb_3857"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("external3857MaterializedViewCandidates() = %#v, want %#v", got, want)
	}
}

func TestDiscoverExternal3857MaterializedViewForDLTB(t *testing.T) {
	if os.Getenv("ADDP_TEST_BUSINESS_POSTGRES") != "1" {
		t.Skip("set ADDP_TEST_BUSINESS_POSTGRES=1 to verify local business-postgres dltb external 3857 target")
	}
	dsn := getenv("ADDP_TEST_BUSINESS_POSTGRES_DSN", "postgres://business:business_password@localhost:5433/business?sslmode=disable")
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()

	target, err := discoverExternal3857MaterializedView(context.Background(), db, "public", "dltb")
	if err != nil {
		t.Fatalf("discover external 3857 materialized view: %v", err)
	}
	if target == nil {
		t.Fatal("target is nil, want public.dltb_mv3857 or public.dltb_3857")
	}
	if target.Schema != "public" ||
		target.GeomColumn != "geom_3857" ||
		target.SRID != spatial.SRIDWebMercator ||
		!target.QuickViewOptimizationTarget ||
		target.PerformanceMode != RealtimeTilePerformanceReady3857Target {
		t.Fatalf("target = %#v, want verified ready 3857 target", target)
	}
	if target.Table != "dltb_mv3857" && target.Table != "dltb_3857" {
		t.Fatalf("target table = %s, want dltb_mv3857 or dltb_3857", target.Table)
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
