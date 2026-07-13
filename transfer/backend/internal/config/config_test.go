package config

import (
	"testing"
	"time"
)

func TestValidateContinuousRuntimeObservability(t *testing.T) {
	valid := &Config{
		ContinuousDiagnosticsInterval:      15 * time.Second,
		ContinuousRetentionDegradedHorizon: 6 * time.Hour,
		ContinuousRetentionCriticalHorizon: time.Hour,
	}
	if err := valid.ValidateContinuousRuntimeObservability(); err != nil {
		t.Fatalf("valid continuous observability config: %v", err)
	}

	invalidInterval := *valid
	invalidInterval.ContinuousDiagnosticsInterval = 0
	if err := invalidInterval.ValidateContinuousRuntimeObservability(); err == nil {
		t.Fatal("zero diagnostics interval was accepted")
	}

	invalidThresholds := *valid
	invalidThresholds.ContinuousRetentionCriticalHorizon = invalidThresholds.ContinuousRetentionDegradedHorizon
	if err := invalidThresholds.ValidateContinuousRuntimeObservability(); err == nil {
		t.Fatal("critical horizon greater than or equal to degraded horizon was accepted")
	}
}
