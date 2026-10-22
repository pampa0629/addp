package service

import (
	"encoding/json"
	"strings"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

func TestGLBFrozenConversionPreservesExactRuntimeOptionNumbers(t *testing.T) {
	raw := json.RawMessage(`{"source":{"item_locator":"addp://engine/33/path/models/model.ifc?type=file","source_engine_id":33,"item_fingerprint":"scanned","item_id":41,"format":"ifc"},"result":{"storage_ref":"unique-owned-object","file_name":"model.glb"},"options":{"integer":9007199254740993,"nested":{"integer":9007199254740995}}}`)
	config, err := decodeFileModel3DGLBConversion(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(config.Options)
	if err != nil || !strings.Contains(string(encoded), "9007199254740993") || !strings.Contains(string(encoded), "9007199254740995") {
		t.Fatalf("options=%s err=%v", encoded, err)
	}
	if config.Source.SourceEngineID != 33 || config.Source.ItemID != 41 || config.Result.StorageRef != "unique-owned-object" {
		t.Fatalf("config=%+v", config)
	}
	for _, bad := range []string{strings.Replace(string(raw), `"source_engine_id":33`, `"source_engine_id":33.5`, 1), strings.Replace(string(raw), `"item_id":41`, `"item_id":-1`, 1), strings.Replace(string(raw), `"options":{"integer":9007199254740993,"nested":{"integer":9007199254740995}}`, `"options":null`, 1)} {
		if _, err := decodeFileModel3DGLBConversion(json.RawMessage(bad)); err == nil {
			t.Fatal("malformed frozen config accepted")
		}
	}
}

func TestGLBFTPFreezesOwnerEngineCompositionAndIsolatedOutputWithoutRounding(t *testing.T) {
	source, item := model3DGLBCandidateFixture("3ds")
	candidate, err := model3DGLBCandidateFromMeta(7, source, item)
	if err != nil {
		t.Fatal(err)
	}
	core := map[string]interface{}{
		"source":                         map[string]interface{}{"item_locator": source.ItemLocator, "source_engine_id": 33, "item_fingerprint": source.ItemFingerprint, "item_id": source.ItemID, "format": "3ds"},
		"result":                         map[string]interface{}{"storage_ref": "previous-default", "file_name": "model.glb", "converter_property": "retained"},
		"options":                        map[string]interface{}{"integer": json.Number("9007199254740993")},
		"additional_conversion_property": json.Number("9007199254740995"),
	}
	raw, _ := json.Marshal(core)
	snapshot := &repository.Model3DGLBTaskSnapshot{TaskID: 17, TenantID: 7, Version: 3, Config: raw}
	tenant := uint(7)
	engine := &commonModels.Engine{ID: 33, TenantID: &tenant, EngineType: "ftp", Version: 5}
	executionID := uuid.NewString()
	config, err := freezeFileModel3DGLBConfig(snapshot, engine, candidate, executionID, "manager")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(config)
	if err != nil || !strings.Contains(string(encoded), "9007199254740993") || !strings.Contains(string(encoded), "9007199254740995") {
		t.Fatalf("frozen=%s err=%v", encoded, err)
	}
	header, err := decodeModel3DGLBFrozenHeader(encoded)
	if err != nil || header.OwnerTask.ID != "17" || header.OwnerTask.Version != "3" || header.EngineVersion != "5" {
		t.Fatalf("header=%+v %v", header, err)
	}
	conversion, err := decodeFileModel3DGLBConversion(encoded)
	if err != nil || validateModel3DGLBExecutionOutput(7, executionID, "manager", conversion.Result) != nil {
		t.Fatalf("conversion=%+v %v", conversion, err)
	}
	if string(snapshot.Config) != string(raw) || !strings.Contains(string(raw), "previous-default") {
		t.Fatal("task snapshot was rewritten")
	}
	if !strings.Contains(string(encoded), "converter_property") || strings.Contains(string(encoded), "texture_refs") {
		t.Fatal("conversion property lost or raw declarations persisted")
	}
	snapshot.Config = json.RawMessage(strings.Replace(string(raw), `"integer":9007199254740993`, `"integer":0,"integer":9007199254740993`, 1))
	if _, err := freezeFileModel3DGLBConfig(snapshot, engine, candidate, executionID, "manager"); err == nil {
		t.Fatal("duplicate task options were erased before the signed configuration digest")
	}
	core["config_version"] = "forged"
	snapshot.Config, _ = json.Marshal(core)
	if _, err := freezeFileModel3DGLBConfig(snapshot, engine, candidate, executionID, "manager"); err == nil {
		t.Fatal("caller-supplied preparation marker accepted")
	}
}
