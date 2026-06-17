package service

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"testing"

	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/models"
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

func TestManagerOptimizationTargetFactsStatus(t *testing.T) {
	result := &models.QuickViewOptimization{
		TargetSRID:           spatial.SRIDWebMercator,
		TargetSchema:         "public",
		TargetTable:          "addp_qvo_roads",
		TargetGeometryColumn: models.QuickViewOptimizationTargetGeometryColumn,
	}
	tests := []struct {
		name         string
		populated    bool
		columnExists bool
		indexed      bool
		actualSRID   int
		wantReady    bool
		wantReason   string
	}{
		{
			name:         "ready",
			populated:    true,
			columnExists: true,
			indexed:      true,
			actualSRID:   spatial.SRIDWebMercator,
			wantReady:    true,
		},
		{
			name:         "not_populated",
			populated:    false,
			columnExists: true,
			indexed:      true,
			actualSRID:   spatial.SRIDWebMercator,
			wantReason:   "quick view optimization materialized view is not populated",
		},
		{
			name:         "missing_column",
			populated:    true,
			columnExists: false,
			indexed:      true,
			actualSRID:   spatial.SRIDWebMercator,
			wantReason:   "quick view optimization target geometry column is missing",
		},
		{
			name:         "wrong_srid",
			populated:    true,
			columnExists: true,
			indexed:      true,
			actualSRID:   4326,
			wantReason:   "quick view optimization target geometry srid is not 3857",
		},
		{
			name:         "missing_index",
			populated:    true,
			columnExists: true,
			indexed:      false,
			actualSRID:   spatial.SRIDWebMercator,
			wantReason:   "quick view optimization target geometry GiST index is missing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := managerOptimizationTargetFactsStatus(result, tt.populated, tt.columnExists, tt.indexed, tt.actualSRID)
			if got.Ready != tt.wantReady || got.Reason != tt.wantReason {
				t.Fatalf("status = %#v, want ready=%v reason=%q", got, tt.wantReady, tt.wantReason)
			}
		})
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
