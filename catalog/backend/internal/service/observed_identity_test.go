package service

import (
	"context"
	"math"
	"testing"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

func TestNumericInt64PreservesObservedIdentity(t *testing.T) {
	for _, test := range []struct {
		name  string
		input any
		want  int64
		valid bool
	}{
		{"native_int", int(9), 9, true},
		{"native_int64", int64(math.MaxInt64), math.MaxInt64, true},
		{"native_negative", int64(math.MinInt64), math.MinInt64, true},
		{"json_integer", float64(9), 9, true},
		{"json_zero", float64(0), 0, true},
		{"json_safe_boundary", float64(1<<53 - 1), 1<<53 - 1, true},
		{"json_negative_safe_boundary", float64(-(1<<53 - 1)), -(1<<53 - 1), true},
		{"fraction", 9.5, 0, false},
		{"negative_fraction", -9.5, 0, false},
		{"ambiguous_positive_boundary", float64(1 << 53), 0, false},
		{"ambiguous_negative_boundary", float64(-(1 << 53)), 0, false},
		{"overflow", float64(1 << 63), 0, false},
		{"nan", math.NaN(), 0, false},
		{"positive_infinity", math.Inf(1), 0, false},
		{"negative_infinity", math.Inf(-1), 0, false},
		{"string_is_not_a_numeric_identity", "9", 0, false},
		{"missing", nil, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, valid := numericInt64(test.input)
			if got != test.want || valid != test.valid {
				t.Fatalf("numericInt64(%v) = (%d, %v), want (%d, %v)", test.input, got, valid, test.want, test.valid)
			}
		})
	}
}

func TestQualitySummaryDoesNotQueryAnAliasedEngine(t *testing.T) {
	for _, engineID := range []float64{9.5, float64(1 << 53)} {
		resolver := &fakeQualitySummaryResolver{result: commonClient.QualityCatalogSummaryResolution{
			Reference:  commonClient.QualityCatalogSummaryReference{EngineID: 9, SchemaName: "public", TableName: "orders"},
			Configured: true,
		}}
		s := NewEntryService(nil, nil, nil).WithQualitySummaryResolver(resolver)
		source := models.SourceBinding{
			TenantID: 7, SourceModule: models.SourceModuleMeta,
			SourceType: models.SourceTypeDataItem, SourceStatus: models.SourceStatusActive, IsCurrent: true,
			ObservedSnapshot: commonModels.JSONMap{
				"item_type": "table", "engine_id": engineID, "schema_name": "public", "table_name": "orders",
			},
		}
		if got := s.resolveQualitySummary(context.Background(), 7, source); got != nil || resolver.calls != 0 {
			t.Fatalf("ambiguous engine ID %v queried Quality: summary=%#v, calls=%d", engineID, got, resolver.calls)
		}
	}
}
