package scanflow

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestLeafExecutionConfigSurvivesPersistence(t *testing.T) {
	const target = "addp://engine/12/path/public/A?type=table"
	config := ManualExecutionConfig(12, 0, "postgresql", []string{target}, nil, nil, "deep", true, "meta")
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal(encoded, &stored); err != nil {
		t.Fatal(err)
	}
	parsed := ParseExecutionConfig(stored)
	if !reflect.DeepEqual(parsed.Targets, []string{target}) || len(parsed.CatalogPaths) != 0 || parsed.ItemID != 0 {
		t.Fatalf("stored leaf scope expanded: %+v", parsed)
	}
}

func TestParseExecutionConfig(t *testing.T) {
	t.Parallel()

	config := ManualExecutionConfig(
		12,
		1831,
		"postgresql",
		nil,
		[]string{"public"},
		[]models.ScanRefGroup{
			{
				Primary: "bucket/path/roads.shp",
				Refs: []models.ScanRef{
					{Path: "bucket/path/roads.shp", Role: "main", Required: true},
				},
			},
		},
		"deep",
		true,
		"transfer",
	)

	parsed := ParseExecutionConfig(config)
	if parsed.EngineID != 12 || parsed.ItemID != 1831 || parsed.StorageType != "postgresql" || parsed.ScanDepth != "deep" || !parsed.Force || parsed.Source != "transfer" {
		t.Fatalf("parsed scalar config = %#v", parsed)
	}
	if _, exists := config["token"]; exists {
		t.Fatal("execution config persisted a user token")
	}
	if !reflect.DeepEqual(parsed.CatalogPaths, []string{"public"}) {
		t.Fatalf("catalog paths = %#v", parsed.CatalogPaths)
	}
	if len(parsed.RefGroups) != 1 || parsed.RefGroups[0].Primary != "bucket/path/roads.shp" {
		t.Fatalf("ref groups = %#v", parsed.RefGroups)
	}
}

func TestTaskExecutionConfigUsesDefaultScanDepth(t *testing.T) {
	t.Parallel()

	config := TaskExecutionConfig(7, "object_storage", models.JSONMap{"type": "engine", "engine_id": 7}, nil, "deep", "meta")
	if config["scan_depth"] != "deep" {
		t.Fatalf("scan_depth = %#v, want deep", config["scan_depth"])
	}
	if config["source"] != "meta" {
		t.Fatalf("source = %#v", config["source"])
	}
}
