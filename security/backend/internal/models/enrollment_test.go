package models

import (
	"encoding/json"
	"testing"
)

func TestCreateProtectionDiscoveryExecutionRequestAcceptsNumericVersion(t *testing.T) {
	var request CreateProtectionDiscoveryExecutionRequest
	if err := json.Unmarshal([]byte(`{"version":3}`), &request); err != nil {
		t.Fatalf("unmarshal numeric version: %v", err)
	}
	if request.Version != 3 {
		t.Fatalf("version = %d, want 3", request.Version)
	}
}

func TestCreateProtectionDiscoveryExecutionRequestRejectsStringVersion(t *testing.T) {
	var request CreateProtectionDiscoveryExecutionRequest
	if err := json.Unmarshal([]byte(`{"version":"3"}`), &request); err == nil {
		t.Fatal("expected string version to be rejected")
	}
}
