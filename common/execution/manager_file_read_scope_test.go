package execution

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

func TestRasterFileScopeBindsPurposeOwnerAndWholeFrozenConfig(t *testing.T) {
	pair, err := authorizationfacts.NewFileCompositionBoundary("envi", []authorizationfacts.FileCompositionRef{
		{Target: plugin.FileItemPath(14, "imagery/grid.img"), Role: "raster", Required: true, Primary: true},
		{Target: plugin.FileItemPath(14, "imagery/grid.hdr"), Role: "header", Required: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	object := map[string]interface{}{"config_version": ManagerRasterCOGConfigVersion, "owner_task": map[string]string{"id": "10", "version": "7"}, "source_engine_version": "5", "source_composition": pair,
		"target": map[string]interface{}{"source_engine_id": 14, "item_fingerprint": strings.Repeat("a", 64)}, "raster": map[string]interface{}{"source_format": "envi", "header_sha256": strings.Repeat("b", 64)}, "cog": map[string]interface{}{"block_size": 256}, "result": map[string]interface{}{"storage_ref": "result"}}
	raw, _ := json.Marshal(object)
	scope, err := NewManagerFileReadScope(raw, "10", "7", "5", pair)
	if err != nil || scope.Purpose() != TaskTypeRasterCOGGeneration {
		t.Fatalf("raster scope rejected: %#v %v", scope, err)
	}
	object["cog"].(map[string]interface{})["block_size"] = 512
	changed, _ := json.Marshal(object)
	other, err := NewManagerFileReadScope(changed, "10", "7", "5", pair)
	if err != nil || other.ConfigDigest == scope.ConfigDigest {
		t.Fatal("COG options escaped complete configuration binding")
	}
	object["config_version"] = ManagerModel3DConfigVersion
	wrong, _ := json.Marshal(object)
	if _, err := NewManagerFileReadScope(wrong, "10", "7", "5", pair); err == nil {
		t.Fatal("ENVI admitted under model purpose")
	}
	encoded, _ := json.Marshal(scope)
	var decoded ManagerFileReadScope
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Purpose() != TaskTypeRasterCOGGeneration {
		t.Fatal("closed file scope lost purpose", err)
	}
}

func TestTIFFPAMScopeBindsCompleteMetaComposition(t *testing.T) {
	composition, err := authorizationfacts.NewScannedFileCompositionBoundary("tiff", "multi", []authorizationfacts.FileCompositionRef{
		{Target: plugin.FileItemPath(14, "images/a.tif"), Role: "image", Required: true, Primary: true},
		{Target: plugin.FileItemPath(14, "images/a.tif.aux.xml"), Role: "auxiliary_metadata"},
	})
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"config_version": ManagerRasterCOGConfigVersion,
		"owner_task": map[string]string{"id": "10", "version": "7"}, "source_engine_version": "5",
		"source_composition": composition, "target": map[string]any{}, "raster": map[string]any{"source_format": "tiff"},
		"cog": map[string]any{}, "result": map[string]any{}}
	raw, _ := json.Marshal(config)
	scope, err := NewManagerFileReadScope(raw, "10", "7", "5", composition)
	if err != nil || scope.Purpose() != TaskTypeRasterCOGGeneration || len(scope.FileComposition.Refs) != 2 {
		t.Fatalf("TIFF/PAM scope rejected or truncated: %+v %v", scope, err)
	}
	encoded, _ := json.Marshal(scope)
	var decoded ManagerFileReadScope
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Purpose() != TaskTypeRasterCOGGeneration {
		t.Fatal("TIFF/PAM scope roundtrip failed", err)
	}
	primaryOnly := composition.Clone()
	primaryOnly.Refs = primaryOnly.Refs[:1]
	if _, err := NewManagerFileReadScope(raw, "10", "7", "5", primaryOnly); err == nil {
		t.Fatal("PAM removed from frozen scope")
	}
	config["config_version"] = ManagerModel3DConfigVersion
	raw, _ = json.Marshal(config)
	if _, err := NewManagerFileReadScope(raw, "10", "7", "5", composition); err == nil {
		t.Fatal("TIFF admitted as model conversion")
	}
}
