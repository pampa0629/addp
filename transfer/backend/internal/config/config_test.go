package config

import (
	"testing"
	"time"
)

func TestValidateContinuousRuntime(t *testing.T) {
	valid := &Config{
		ContinuousDiagnosticsInterval:      15 * time.Second,
		ContinuousRetentionDegradedHorizon: 6 * time.Hour,
		ContinuousRetentionCriticalHorizon: time.Hour,
		ContinuousCheckpointStaleAfter:     5 * time.Minute,
		ContinuousRecoveryInitialBackoff:   time.Second,
		ContinuousRecoveryMaxBackoff:       time.Minute,
		ContinuousRecoveryMaxFailures:      5,
		ContinuousRecoveryCircuitOpenTime:  5 * time.Minute,
		ContinuousRecoveryStabilityWindow:  5 * time.Minute,
		DeadLetterReconcileInterval:        time.Minute,
		DeadLetterReconcileBatchSize:       100,
		DeadLetterReconcileTimeout:         10 * time.Second,
		DeadLetterReconcileFetchMaxBytes:   10 * 1024 * 1024,
		ContinuousRuntimeStopTimeout:       45 * time.Second,
		ContinuousRuntimeStopPollInterval:  250 * time.Millisecond,
	}
	if err := valid.ValidateContinuousRuntime(); err != nil {
		t.Fatalf("valid continuous runtime config: %v", err)
	}

	invalidInterval := *valid
	invalidInterval.ContinuousDiagnosticsInterval = 0
	if err := invalidInterval.ValidateContinuousRuntime(); err == nil {
		t.Fatal("zero diagnostics interval was accepted")
	}

	invalidThresholds := *valid
	invalidThresholds.ContinuousRetentionCriticalHorizon = invalidThresholds.ContinuousRetentionDegradedHorizon
	if err := invalidThresholds.ValidateContinuousRuntime(); err == nil {
		t.Fatal("critical horizon greater than or equal to degraded horizon was accepted")
	}

	invalidCheckpointThreshold := *valid
	invalidCheckpointThreshold.ContinuousCheckpointStaleAfter = invalidCheckpointThreshold.ContinuousDiagnosticsInterval
	if err := invalidCheckpointThreshold.ValidateContinuousRuntime(); err == nil {
		t.Fatal("checkpoint stale threshold equal to diagnostics interval was accepted")
	}

	invalidRecovery := *valid
	invalidRecovery.ContinuousRecoveryInitialBackoff = invalidRecovery.ContinuousRecoveryMaxBackoff + time.Second
	if err := invalidRecovery.ValidateContinuousRuntime(); err == nil {
		t.Fatal("recovery initial backoff greater than max backoff was accepted")
	}

	invalidFailures := *valid
	invalidFailures.ContinuousRecoveryMaxFailures = 0
	if err := invalidFailures.ValidateContinuousRuntime(); err == nil {
		t.Fatal("zero recovery max failures was accepted")
	}

	invalidDLQBatch := *valid
	invalidDLQBatch.DeadLetterReconcileBatchSize = 1001
	if err := invalidDLQBatch.ValidateContinuousRuntime(); err == nil {
		t.Fatal("oversized dead-letter reconcile batch was accepted")
	}

	invalidDLQTimeout := *valid
	invalidDLQTimeout.DeadLetterReconcileTimeout = 0
	if err := invalidDLQTimeout.ValidateContinuousRuntime(); err == nil {
		t.Fatal("zero dead-letter reconcile timeout was accepted")
	}

	invalidStopPoll := *valid
	invalidStopPoll.ContinuousRuntimeStopPollInterval = invalidStopPoll.ContinuousRuntimeStopTimeout
	if err := invalidStopPoll.ValidateContinuousRuntime(); err == nil {
		t.Fatal("continuous runtime stop poll interval equal to timeout was accepted")
	}
}

func TestInfraKafkaTransferConnectionInfoUsesDedicatedPrincipal(t *testing.T) {
	cfg := Config{
		InfraKafkaBootstrapServers: "localhost:19092",
		InfraKafkaSecurityProtocol: "sasl_plaintext",
		InfraKafkaTransferPassword: "secret",
	}
	info, err := cfg.InfraKafkaTransferConnectionInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info["username"] != "transfer" || info["password"] != "secret" || info["sasl_mechanism"] != "plain" {
		t.Fatalf("connection info = %#v", info)
	}
}

func TestInfraKafkaAdminConnectionInfoUsesConfiguredPrincipal(t *testing.T) {
	cfg := Config{
		InfraKafkaBootstrapServers: "localhost:19092",
		InfraKafkaSecurityProtocol: "sasl_plaintext",
		InfraKafkaAdminUsername:    "admin-user",
		InfraKafkaAdminPassword:    "admin-secret",
	}
	info, err := cfg.InfraKafkaAdminConnectionInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info["username"] != "admin-user" || info["password"] != "admin-secret" || info["client_id"] != "addp-transfer-dlq-cleanup" {
		t.Fatalf("admin connection info = %#v", info)
	}
}
