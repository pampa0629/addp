package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONStringAcceptsObjectAndString(t *testing.T) {
	var fromObject struct {
		Capabilities *JSONString `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(`{"capabilities":{"schema_version":"engine.capabilities/v1","engine_type":"postgresql"}}`), &fromObject); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}
	if got := *fromObject.Capabilities.StringPtr(); got != `{"schema_version":"engine.capabilities/v1","engine_type":"postgresql"}` {
		t.Fatalf("object capabilities = %s", got)
	}

	var fromString struct {
		Capabilities *JSONString `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(`{"capabilities":"{\"schema_version\":\"engine.capabilities/v1\"}"}`), &fromString); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if got := *fromString.Capabilities.StringPtr(); got != `{"schema_version":"engine.capabilities/v1"}` {
		t.Fatalf("string capabilities = %s", got)
	}
}

func TestEngineCapabilitiesMarshalAsObject(t *testing.T) {
	capabilities := JSONString(`{"schema_version":"engine.capabilities/v1"}`)
	engine := Engine{ID: 1, Capabilities: &capabilities}

	data, err := json.Marshal(engine)
	if err != nil {
		t.Fatalf("marshal engine: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("invalid json: %s", data)
	}
	if got := string(data); !strings.Contains(got, `"capabilities":{"schema_version":"engine.capabilities/v1"}`) {
		t.Fatalf("capabilities should marshal as object, got %s", got)
	}
}
