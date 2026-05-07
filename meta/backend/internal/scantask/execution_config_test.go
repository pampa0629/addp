package scantask

import (
	"reflect"
	"testing"
)

func TestParseExecutionConfig(t *testing.T) {
	t.Parallel()

	config := ManualExecutionConfig(
		12,
		"postgresql",
		[]string{"public"},
		[]string{"bucket/path"},
		"deep",
		"token",
	)

	parsed := ParseExecutionConfig(config)
	if parsed.EngineID != 12 || parsed.StorageType != "postgresql" || parsed.ScanDepth != "deep" || parsed.Token != "token" {
		t.Fatalf("parsed scalar config = %#v", parsed)
	}
	if !reflect.DeepEqual(parsed.Namespaces, []string{"public"}) {
		t.Fatalf("namespaces = %#v", parsed.Namespaces)
	}
	if !reflect.DeepEqual(parsed.ObjectPaths, []string{"bucket/path"}) {
		t.Fatalf("object paths = %#v", parsed.ObjectPaths)
	}
}

func TestTaskExecutionConfigUsesDefaultScanDepth(t *testing.T) {
	t.Parallel()

	config := TaskExecutionConfig(7, "object_storage", nil, "deep")
	if config["scan_depth"] != "deep" {
		t.Fatalf("scan_depth = %#v, want deep", config["scan_depth"])
	}
}
