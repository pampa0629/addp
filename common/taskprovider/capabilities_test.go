package taskprovider

import (
	"strings"
	"testing"
)

func TestParseCapabilitiesAcceptsTaskCapabilitiesV2(t *testing.T) {
	caps, err := ParseCapabilities(validCapabilitiesForTest())
	if err != nil {
		t.Fatalf("ParseCapabilities() error = %v, want nil", err)
	}
	if caps.SchemaVersion != CapabilitiesSchemaVersion {
		t.Fatalf("schema version = %q, want %q", caps.SchemaVersion, CapabilitiesSchemaVersion)
	}
	if len(caps.TaskCapabilities) != 1 {
		t.Fatalf("task capabilities len = %d, want 1", len(caps.TaskCapabilities))
	}
	capability := caps.TaskCapabilities[0]
	if capability.Type != "scan" || capability.DisplayName != "扫描任务" {
		t.Fatalf("capability = %#v, want scan capability", capability)
	}
	if got := caps.CapabilityFor("scan"); got == nil || got.Type != "scan" {
		t.Fatalf("CapabilityFor(scan) = %#v, want scan", got)
	}
}

func TestParseCapabilitiesAcceptsTopLevelPrivateExtension(t *testing.T) {
	raw := strings.Replace(
		validCapabilitiesForTest(),
		`"task_capabilities":[{`,
		`"x_owner_features":{"supports_preview":true},"task_capabilities":[{`,
		1,
	)
	caps, err := ParseCapabilities(raw)
	if err != nil {
		t.Fatalf("ParseCapabilities() error = %v, want nil", err)
	}
	if _, ok := caps.Extensions["x_owner_features"]; !ok {
		t.Fatalf("extensions = %#v, want x_owner_features", caps.Extensions)
	}
}

func TestParseCapabilitiesRejectsUnknownTaskCapabilityField(t *testing.T) {
	raw := strings.Replace(validCapabilitiesForTest(), `"deprecated":false`, `"deprecated":false,"owner_runtime":"custom"`, 1)
	assertCapabilitiesError(t, raw, "owner_runtime is not allowed")
}

func TestParseCapabilitiesRejectsLegacyTaskTypesField(t *testing.T) {
	raw := strings.Replace(validCapabilitiesForTest(), `"task_capabilities"`, `"task_types"`, 1)
	assertCapabilitiesError(t, raw, "task_types must be a standard field or x_ private extension")
}

func TestParseCapabilitiesRejectsDuplicateTaskType(t *testing.T) {
	raw := strings.Replace(
		validCapabilitiesForTest(),
		`}]`,
		`},{
			"type":"scan",
			"display_name":"扫描任务",
			"description":"执行元数据扫描",
			"definition_schema":{"type":"object"},
			"supports_schedule":true,
			"supports_cancel":false,
			"supports_inline_execution":false,
			"create_url":"/meta/scan",
			"edit_url":"/meta/scan?task_id=:id",
			"deprecated":false
		}]`,
		1,
	)
	assertCapabilitiesError(t, raw, `duplicate task_type "scan"`)
}

func TestParseCapabilitiesRejectsUnsupportedSchemaKeyword(t *testing.T) {
	raw := strings.Replace(validCapabilitiesForTest(), `"definition_schema":{"type":"object"}`, `"definition_schema":{"type":"object","oneOf":[{"type":"object"}]}`, 1)
	assertCapabilitiesError(t, raw, "oneOf is not supported")
}

func TestParseCapabilitiesRejectsMissingBooleanField(t *testing.T) {
	raw := strings.Replace(validCapabilitiesForTest(), `"supports_cancel":false,`, ``, 1)
	assertCapabilitiesError(t, raw, "supports_cancel must be boolean")
}

func TestParseCapabilitiesRejectsNonConsoleRouteURL(t *testing.T) {
	raw := strings.Replace(validCapabilitiesForTest(), `"create_url":"/meta/scan"`, `"create_url":"https://example.invalid/meta/scan"`, 1)
	assertCapabilitiesError(t, raw, "create_url must be a Console route starting with /")
}

func TestParseCapabilitiesRejectsInlineExecutionInV2(t *testing.T) {
	raw := strings.Replace(validCapabilitiesForTest(), `"supports_inline_execution":false`, `"supports_inline_execution":true`, 1)
	assertCapabilitiesError(t, raw, "supports_inline_execution must be false")
}

func TestParseCapabilitiesDetectsCancelableTaskType(t *testing.T) {
	raw := strings.Replace(validCapabilitiesForTest(), `"supports_cancel":false`, `"supports_cancel":true`, 1)
	caps, err := ParseCapabilities(raw)
	if err != nil {
		t.Fatalf("ParseCapabilities() error = %v, want nil", err)
	}
	if !caps.HasCancelableTaskType() {
		t.Fatal("HasCancelableTaskType() = false, want true")
	}
}

func assertCapabilitiesError(t *testing.T, raw string, want string) {
	t.Helper()
	_, err := ParseCapabilities(raw)
	if err == nil {
		t.Fatalf("ParseCapabilities() error = nil, want error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("ParseCapabilities() error = %q, want containing %q", err.Error(), want)
	}
}

func validCapabilitiesForTest() string {
	return `{
		"schema_version":"task.capabilities/v2",
		"task_capabilities":[{
			"type":"scan",
			"display_name":"扫描任务",
			"description":"执行元数据扫描",
			"definition_schema":{"type":"object"},
			"supports_schedule":true,
			"supports_cancel":false,
			"supports_inline_execution":false,
			"create_url":"/meta/scan",
			"edit_url":"/meta/scan?task_id=:id",
			"deprecated":false
		}]
	}`
}
