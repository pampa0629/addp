package service

import (
	"math"
	"testing"
)

func TestEvaluateCheckCounts(t *testing.T) {
	tests := []struct {
		name       string
		counts     CheckCounts
		wantRate   float64
		wantPassed bool
		wantErr    bool
	}{
		{name: "empty table", counts: CheckCounts{}, wantRate: 100, wantPassed: true},
		{name: "all passed", counts: CheckCounts{TotalCount: 10}, wantRate: 100, wantPassed: true},
		{name: "partially failed", counts: CheckCounts{TotalCount: 10, FailedCount: 2}, wantRate: 80, wantPassed: false},
		{name: "failed exceeds total", counts: CheckCounts{TotalCount: 1, FailedCount: 2}, wantErr: true},
		{name: "negative count", counts: CheckCounts{TotalCount: -1}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate, passed, err := evaluateCheckCounts(tt.counts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("evaluateCheckCounts() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if math.Abs(rate-tt.wantRate) > 0.000001 || passed != tt.wantPassed {
				t.Fatalf("evaluateCheckCounts() = (%v, %v), want (%v, %v)", rate, passed, tt.wantRate, tt.wantPassed)
			}
		})
	}
}

func TestAggregateExecutionResult(t *testing.T) {
	details := []RuleResult{
		{Column: "z_col", PassRate: 50, Passed: false},
		{Column: "a_col", PassRate: 100, Passed: true},
		{Column: "z_col", PassRate: 100, Passed: true},
	}
	result, err := aggregateExecutionResult(details)
	if err != nil {
		t.Fatalf("aggregateExecutionResult() error = %v", err)
	}
	if result.QualityScore != 250.0/3.0 || result.TotalRules != 3 || result.PassedRules != 2 || result.FailedRules != 1 {
		t.Fatalf("aggregateExecutionResult() = %#v", result)
	}
	if len(result.FieldScores) != 2 || result.FieldScores[0].Column != "a_col" || result.FieldScores[1].Column != "z_col" {
		t.Fatalf("field score order = %#v", result.FieldScores)
	}
	if result.FieldScores[1].Score != 75 || result.FieldScores[1].RuleCount != 2 {
		t.Fatalf("z_col score = %#v", result.FieldScores[1])
	}

	metadata := executionResultMetadata(result)
	if metadata["schema_version"] != qualityExecutionResultSchemaVersion {
		t.Fatalf("metadata schema_version = %v", metadata["schema_version"])
	}
	if _, exists := metadata["result"]; exists {
		t.Fatal("metadata must not contain legacy result wrapper")
	}
}

func TestAggregateExecutionResultRejectsNoRules(t *testing.T) {
	if _, err := aggregateExecutionResult(nil); err == nil {
		t.Fatal("aggregateExecutionResult() error = nil, want no-rules error")
	}
}
