package mvtbenchmark

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type fakeExecutor struct {
	mu       sync.Mutex
	queries  []string
	args     [][]any
	results  [][]byte
	failCall int
	calls    int
}

func (f *fakeExecutor) ExecuteTile(ctx context.Context, query string, args []any) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.queries = append(f.queries, query)
	f.args = append(f.args, append([]any(nil), args...))
	if f.failCall > 0 && f.calls == f.failCall {
		return nil, errors.New("forced failure")
	}
	if len(f.results) == 0 {
		return []byte("tile"), nil
	}
	result := f.results[(f.calls-1)%len(f.results)]
	return result, nil
}

func (f *fakeExecutor) Explain(context.Context, string, []any) ([]string, error) {
	return []string{"Seq Scan on roads", "Execution Time: 1.000 ms"}, nil
}

func (f *fakeExecutor) Stats() DBStats {
	return DBStats{MaxOpenConnections: 1}
}

func (f *fakeExecutor) Close() error {
	return nil
}

func TestRunBuildsSourceTransformAnd3857TargetQueries(t *testing.T) {
	cfg := NormalizeConfig(Config{
		DSN:        "postgres://example",
		Iterations: 1,
		Warmup:     0,
		Scenarios: []Scenario{
			{
				Name:           "source 4326",
				Schema:         "public",
				Table:          "roads",
				GeometryColumn: "shape",
				SRID:           4326,
				Tiles:          []TileCoord{{Z: 1, X: 0, Y: 0}},
			},
			{
				Name:           "quick view optimization target",
				TargetKind:     "source_schema_materialized_view",
				Schema:         "public",
				Table:          "addp_qvo_roads_3857",
				GeometryColumn: "geom_3857",
				SRID:           3857,
				Tiles:          []TileCoord{{Z: 1, X: 0, Y: 0}},
			},
		},
	})
	executor := &fakeExecutor{}

	report, err := Run(context.Background(), cfg, executor)
	if err != nil {
		t.Fatalf("run benchmark: %v", err)
	}
	if len(report.Scenarios) != 2 {
		t.Fatalf("scenario count = %d, want 2", len(report.Scenarios))
	}
	if report.Scenarios[0].RenderPath != "source_transform_path" {
		t.Fatalf("render path = %s, want source_transform_path", report.Scenarios[0].RenderPath)
	}
	if report.Scenarios[1].TargetKind != "source_schema_materialized_view" {
		t.Fatalf("target kind = %s, want source_schema_materialized_view", report.Scenarios[1].TargetKind)
	}
	if report.Scenarios[1].RenderPath != "ready_3857_target" {
		t.Fatalf("render path = %s, want ready_3857_target", report.Scenarios[1].RenderPath)
	}
	if !strings.Contains(executor.queries[0], `ST_Transform(t."shape", 3857)`) {
		t.Fatalf("source query should transform geometry:\n%s", executor.queries[0])
	}
	if strings.Contains(executor.queries[1], "ST_Transform") {
		t.Fatalf("3857 target query should not transform geometry:\n%s", executor.queries[1])
	}
	if len(report.Recommendations.RenderPaths) != 2 {
		t.Fatalf("recommendation count = %d, want 2", len(report.Recommendations.RenderPaths))
	}
	sourceRecommendation := recommendationByRenderPath(report, "source_transform_path")
	if sourceRecommendation.RecommendedAction != "quick_view_optimization" ||
		sourceRecommendation.RetryPolicy != "suppress_tile" {
		t.Fatalf("source recommendation = %#v, want quick view optimization with suppress_tile", sourceRecommendation)
	}
	targetRecommendation := recommendationByRenderPath(report, "ready_3857_target")
	if targetRecommendation.RecommendedAction != "tile_cache_generation" ||
		targetRecommendation.RetryPolicy != "ttl" ||
		targetRecommendation.QuickViewRealtimeTileRetryAfterSec != DefaultRetryAfterSec {
		t.Fatalf("target recommendation = %#v, want tile cache generation with ttl retry", targetRecommendation)
	}
	if sourceRecommendation.QuickViewRealtimeTileTimeoutMS < 1000 ||
		targetRecommendation.QuickViewRealtimeTileTimeoutMS < 1000 {
		t.Fatalf("timeout recommendations should use a deployable millisecond budget: source=%d target=%d",
			sourceRecommendation.QuickViewRealtimeTileTimeoutMS,
			targetRecommendation.QuickViewRealtimeTileTimeoutMS)
	}
}

func TestRunReportsErrorsAndDoesNotAbortMeasuredIterations(t *testing.T) {
	cfg := NormalizeConfig(Config{
		DSN:        "postgres://example",
		Iterations: 3,
		Warmup:     0,
		Scenarios: []Scenario{{
			Name:           "roads",
			Schema:         "public",
			Table:          "roads",
			GeometryColumn: "shape",
			SRID:           3857,
			Tiles:          []TileCoord{{Z: 1, X: 0, Y: 0}},
		}},
	})
	executor := &fakeExecutor{
		results:  [][]byte{[]byte("a"), []byte{}, []byte("abc")},
		failCall: 2,
	}

	report, err := Run(context.Background(), cfg, executor)
	if err != nil {
		t.Fatalf("run benchmark: %v", err)
	}
	summary := report.Scenarios[0].Tiles[0].Summary
	if summary.Runs != 3 || summary.SuccessfulRuns != 2 || summary.ErrorRuns != 1 {
		t.Fatalf("summary = %#v, want 3 runs, 2 successful, 1 error", summary)
	}
	if summary.MaxSizeBytes != 3 {
		t.Fatalf("max size = %d, want 3", summary.MaxSizeBytes)
	}
}

func TestValidateConfigRejectsMissingGeometryColumn(t *testing.T) {
	cfg := NormalizeConfig(Config{
		DSN: "postgres://example",
		Scenarios: []Scenario{{
			Name:   "roads",
			Schema: "public",
			Table:  "roads",
			SRID:   3857,
			Tiles:  []TileCoord{{Z: 1, X: 0, Y: 0}},
		}},
	})

	err := ValidateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "geometry_column is required") {
		t.Fatalf("validate error = %v, want geometry column error", err)
	}
}

func recommendationByRenderPath(report Report, renderPath string) RenderPathRecommendation {
	for _, recommendation := range report.Recommendations.RenderPaths {
		if recommendation.RenderPath == renderPath {
			return recommendation
		}
	}
	return RenderPathRecommendation{}
}
