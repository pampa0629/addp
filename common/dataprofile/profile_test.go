package dataprofile

import (
	"math"
	"testing"
	"time"

	"github.com/addp/common/datatype"
)

func TestBuildNumericAndTextProfiles(t *testing.T) {
	rowCount := int64(100)
	rows := []map[string]interface{}{
		{"id": int64(0), "name": "", "active": true},
		{"id": int64(1), "name": "Alice", "active": false},
		{"id": int64(2), "name": "Alice", "active": true},
		{"id": nil, "name": "   ", "active": nil},
	}
	profile := Build(rows, []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, Nullable: true},
		{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
		{Name: "active", Type: datatype.FieldTypeBool, Nullable: true},
	}, BuildOptions{RowsScanned: 20, RowCount: &rowCount, TopN: 3, HistogramBins: 4, Truncated: true})

	if profile.SchemaVersion != SchemaVersionV1 || profile.SampleSize != 4 || profile.RowsScanned != 20 || !profile.Truncated {
		t.Fatalf("unexpected profile summary: %#v", profile)
	}
	id := profile.Fields[0]
	if id.Status != MetricStatusComputed || id.NullCount != 1 || id.ValueCount != 3 || id.DistinctCount != 3 || !id.DistinctApproximate {
		t.Fatalf("unexpected id counts: %#v", id)
	}
	if id.Numeric == nil || id.Numeric.ZeroCount != 1 || id.Numeric.Min != 0 || id.Numeric.Max != 2 || id.Numeric.Median != 1 {
		t.Fatalf("unexpected numeric metrics: %#v", id.Numeric)
	}
	name := profile.Fields[1]
	if name.Text == nil || name.Text.EmptyCount != 1 || name.Text.BlankCount != 2 || name.Text.MinLength != 0 || name.Text.MaxLength != 5 {
		t.Fatalf("unexpected text metrics: %#v", name.Text)
	}
	if len(name.TopValues) != 3 || name.TopValues[0].Value != "Alice" || name.TopValues[0].Count != 2 {
		t.Fatalf("unexpected top values: %#v", name.TopValues)
	}
	active := profile.Fields[2]
	if active.Boolean == nil || active.Boolean.TrueCount != 2 || active.Boolean.FalseCount != 1 || len(active.Distribution) != 2 {
		t.Fatalf("unexpected boolean metrics: %#v", active)
	}
}

func TestBuildTemporalAndObservations(t *testing.T) {
	rows := make([]map[string]interface{}, 0, 20)
	for i := 0; i < 20; i++ {
		rows = append(rows, map[string]interface{}{
			"created_at": time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC),
			"constant":   "same",
			"mostly_null": func() interface{} {
				if i < 4 {
					return i
				}
				return nil
			}(),
		})
	}
	profile := Build(rows, []datatype.FieldInfo{
		{Name: "created_at", Type: datatype.FieldTypeTimestamp},
		{Name: "constant", Type: datatype.FieldTypeString},
		{Name: "mostly_null", Type: datatype.FieldTypeInt, Nullable: true},
	}, BuildOptions{RowsScanned: 20, HistogramBins: 5})

	created := profile.Fields[0]
	if created.Temporal == nil || created.Temporal.Min != "2026-01-01T00:00:00Z" || created.Temporal.Max != "2026-01-20T00:00:00Z" {
		t.Fatalf("unexpected temporal metrics: %#v", created.Temporal)
	}
	if !hasObservation(profile.Fields[1].Observations, ObservationConstant) {
		t.Fatalf("constant observation missing: %#v", profile.Fields[1].Observations)
	}
	if !hasObservation(profile.Fields[2].Observations, ObservationHighMissing) || math.Abs(profile.Fields[2].NullRate-0.8) > 1e-9 {
		t.Fatalf("high missing observation missing: %#v", profile.Fields[2])
	}
	if !hasObservation(created.Observations, ObservationHighCardinality) {
		t.Fatalf("high-cardinality temporal observation missing: %#v", created.Observations)
	}
	if hasObservation(created.Observations, ObservationPossibleIdentifier) {
		t.Fatalf("temporal field must not be inferred as an identifier: %#v", created.Observations)
	}
}

func TestBuildInfersFieldsAndKeepsUnsupportedBaseMetrics(t *testing.T) {
	profile := Build([]map[string]interface{}{
		{"payload": map[string]interface{}{"a": 1}},
		{"payload": nil},
	}, nil, BuildOptions{RowsScanned: 2})
	if len(profile.Fields) != 1 {
		t.Fatalf("fields = %d, want 1", len(profile.Fields))
	}
	field := profile.Fields[0]
	if field.Name != "payload" || field.Status != MetricStatusUnsupported || field.ValueCount != 1 || field.NullCount != 1 {
		t.Fatalf("unexpected inferred field: %#v", field)
	}
}

func hasObservation(observations []Observation, code string) bool {
	for _, observation := range observations {
		if observation.Code == code {
			return true
		}
	}
	return false
}
