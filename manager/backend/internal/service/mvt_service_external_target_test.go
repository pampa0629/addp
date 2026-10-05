package service

import (
	"reflect"
	"testing"

	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/models"
)

func TestExternal3857MaterializedViewCandidates(t *testing.T) {
	got := external3857MaterializedViewCandidates("dltb")
	want := []string{"dltb_mv3857", "dltb_3857"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("external3857MaterializedViewCandidates() = %#v, want %#v", got, want)
	}
}

func TestManagerOptimizationTargetFactsStatus(t *testing.T) {
	result := &models.VectorMaterializedView{
		TargetSRID:           spatial.SRIDWebMercator,
		TargetSchema:         "public",
		TargetTable:          "addp_vmv_roads",
		TargetGeometryColumn: models.VectorMaterializedViewTargetGeometryColumn,
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
			wantReason:   "vector materialized view materialized view is not populated",
		},
		{
			name:         "missing_column",
			populated:    true,
			columnExists: false,
			indexed:      true,
			actualSRID:   spatial.SRIDWebMercator,
			wantReason:   "vector materialized view target geometry column is missing",
		},
		{
			name:         "wrong_srid",
			populated:    true,
			columnExists: true,
			indexed:      true,
			actualSRID:   4326,
			wantReason:   "vector materialized view target geometry srid is not 3857",
		},
		{
			name:         "undeclared_srid",
			populated:    true,
			columnExists: true,
			indexed:      true,
			wantReason:   "vector materialized view target geometry srid is missing",
		},
		{
			name:         "missing_index",
			populated:    true,
			columnExists: true,
			indexed:      false,
			actualSRID:   spatial.SRIDWebMercator,
			wantReason:   "vector materialized view target geometry GiST index is missing",
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
