package service

import (
	"encoding/json"
	"testing"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	rastercogref "github.com/addp/manager/internal/cog"
)

func TestManagerInfraObjectLineageRefUsesCanonicalInfraLocator(t *testing.T) {
	ref, err := managerInfraObjectLineageRef(
		rastercogref.ObjectStorageRef("manager", "tenant_7/raster-cog/source/result.tif"),
		"manager",
	)
	if err != nil {
		t.Fatalf("managerInfraObjectLineageRef: %v", err)
	}
	if ref.Port != managerLineageOutputPort {
		t.Fatalf("port = %q", ref.Port)
	}
	if ref.Locator != "addp-infra://minio/manager/tenant_7/raster-cog/source/result.tif?type=object" {
		t.Fatalf("locator = %q", ref.Locator)
	}
}

func TestManagerInfraLineageRefKeepsMultiObjectArtifactAsPrefix(t *testing.T) {
	ref, err := managerInfraLineageRef(
		rastercogref.ObjectStorageRef("manager", "tenant_7/model3d-tiles/source/3d_tiles"),
		"manager",
		"prefix",
	)
	if err != nil {
		t.Fatalf("managerInfraLineageRef: %v", err)
	}
	if ref.Locator != "addp-infra://minio/manager/tenant_7/model3d-tiles/source/3d_tiles?type=prefix" {
		t.Fatalf("locator = %q", ref.Locator)
	}
}

func TestManagerExecutionLineageUsesSharedContract(t *testing.T) {
	input := managerItemLineageRef("addp://engine/12/path/outdoor/roads?type=table&item_id=9", "fp-roads", 9)
	output := managerResourceLineageRef(managerLineageOutputPort, "addp-infra://minio/manager/result.pmtiles?type=object")
	metadata := managerExecutionLineage(commonModels.JSONMap{"kept": true}, "vector_tile_cache_generation", []commonExecution.LineageResourceRef{input}, []commonExecution.LineageResourceRef{output}, "", "scan-1")

	payload, err := json.Marshal(metadata["lineage_facts"])
	if err != nil {
		t.Fatalf("marshal lineage: %v", err)
	}
	var facts commonExecution.LineageFacts
	if err := json.Unmarshal(payload, &facts); err != nil {
		t.Fatalf("unmarshal lineage: %v", err)
	}
	if facts.SchemaVersion != commonExecution.LineageFactsSchemaVersion || len(facts.Inputs) != 1 || len(facts.Outputs) != 1 {
		t.Fatalf("facts = %+v", facts)
	}
	if len(facts.Operations) != 1 || facts.Operations[0].Operator != "vector_tile_cache_generation" {
		t.Fatalf("operations = %+v", facts.Operations)
	}
	if facts.Operations[0].Kind != "derive" {
		t.Fatalf("operation kind = %q", facts.Operations[0].Kind)
	}
	if len(facts.MetaScanRefs) != 1 || facts.MetaScanRefs[0] != "scan-1" {
		t.Fatalf("meta_scan_refs = %#v", facts.MetaScanRefs)
	}
	if metadata["kept"] != true {
		t.Fatalf("existing metadata was not preserved")
	}
}

func TestManagerLineageOperationKindKeepsAnalysisDistinctFromDerivation(t *testing.T) {
	if got := managerLineageOperationKind(commonExecution.TaskTypeDataProfiling); got != "profile" {
		t.Fatalf("data profiling kind = %q", got)
	}
	if got := managerLineageOperationKind(commonExecution.TaskTypeEmbedding); got != "embed" {
		t.Fatalf("embedding kind = %q", got)
	}
}

func TestManagerChildResourceLocatorBuildsBusinessObjectLocator(t *testing.T) {
	got := managerChildResourceLocator("addp://engine/26/path/vector-tiles?type=directory&node_id=4", "roads.pmtiles")
	want := "addp://engine/26/path/vector-tiles/roads.pmtiles?type=file"
	if got != want {
		t.Fatalf("locator = %q, want %q", got, want)
	}
}

func TestManagerEmbeddingExecutionLineageUsesFrozenTarget(t *testing.T) {
	metadata := managerEmbeddingExecutionLineage(commonModels.JSONMap{}, commonModels.JSONMap{
		"target": commonModels.JSONMap{
			"locator":          "addp://engine/12/path/outdoor/persons?type=collection&item_id=9",
			"item_id":          9,
			"item_fingerprint": "fp-persons",
		},
	})
	if metadata["lineage_facts"] == nil {
		t.Fatalf("lineage_facts missing")
	}
}
