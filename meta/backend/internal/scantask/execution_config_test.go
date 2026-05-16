package scantask

import (
	"reflect"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestParseExecutionConfig(t *testing.T) {
	t.Parallel()

	config := ManualExecutionConfig(
		12,
		"postgresql",
		[]string{"public"},
		"deep",
		true,
		"token",
	)

	parsed := ParseExecutionConfig(config)
	if parsed.EngineID != 12 || parsed.StorageType != "postgresql" || parsed.ScanDepth != "deep" || !parsed.Force || parsed.Token != "token" {
		t.Fatalf("parsed scalar config = %#v", parsed)
	}
	if !reflect.DeepEqual(parsed.CatalogPaths, []string{"public"}) {
		t.Fatalf("catalog paths = %#v", parsed.CatalogPaths)
	}
}

func TestTaskExecutionConfigUsesDefaultScanDepth(t *testing.T) {
	t.Parallel()

	config := TaskExecutionConfig(7, "object_storage", nil, "deep")
	if config["scan_depth"] != "deep" {
		t.Fatalf("scan_depth = %#v, want deep", config["scan_depth"])
	}
}

func TestCatalogPathsFromParamsUsesCatalogPathsOnly(t *testing.T) {
	t.Parallel()

	params := models.JSONMap{
		"catalog_paths": []interface{}{"new/path"},
	}
	if got := catalogPathsFromParams(params); !reflect.DeepEqual(got, []string{"new/path"}) {
		t.Fatalf("catalog paths = %#v", got)
	}
}

func TestNormalizeScanDepth(t *testing.T) {
	t.Parallel()

	got, err := NormalizeScanDepth("", ScanDepthBasic)
	if err != nil || got != ScanDepthBasic {
		t.Fatalf("default depth = %q, err=%v", got, err)
	}

	got, err = NormalizeScanDepth("DEEP", ScanDepthBasic)
	if err != nil || got != ScanDepthDeep {
		t.Fatalf("normalized depth = %q, err=%v", got, err)
	}

	if _, err = NormalizeScanDepth("shallow", ScanDepthBasic); err == nil {
		t.Fatal("shallow should be rejected")
	}
}
