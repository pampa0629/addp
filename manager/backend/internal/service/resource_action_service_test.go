package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/engineaccess"
)

type fakeResourceActionSystemClient struct {
	engines map[uint]*commonModels.Engine
}

func (c fakeResourceActionSystemClient) GetEngine(engineID uint) (*commonModels.Engine, error) {
	return onlineEngineFixture(c.engines[engineID]), nil
}

func (c fakeResourceActionSystemClient) GetEngineForTenant(_ context.Context, _ uint, engineID uint) (*commonModels.Engine, error) {
	return onlineEngineFixture(c.engines[engineID]), nil
}

func onlineEngineFixture(engine *commonModels.Engine) *commonModels.Engine {
	if engine == nil || engine.ConnectionStatus != "" {
		return engine
	}
	copy := *engine
	copy.ConnectionStatus = commonModels.EngineConnectionOnline
	return &copy
}

func TestResourceActionsStorageNodeSupportsUploadOnly(t *testing.T) {
	svc := NewResourceActionService(fakeResourceActionSystemClient{
		engines: map[uint]*commonModels.Engine{
			3: {
				ID:             3,
				TenantID:       uintPtr(7),
				EngineType:     "minio",
				LifecycleState: "active",
			},
		},
	})

	got, err := svc.GetResourceActions(t.Context(), "addp://engine/3/path/data/raw?type=prefix&node_id=20", uintPtr(7))
	if err != nil {
		t.Fatalf("GetResourceActions() error = %v", err)
	}
	if got.EngineCategory != "storage" || got.Kind != "node" {
		t.Fatalf("category/kind = %s/%s, want storage/node", got.EngineCategory, got.Kind)
	}
	if !got.Actions["upload"].Supported {
		t.Fatalf("upload action = %#v, want supported", got.Actions["upload"])
	}
	for _, action := range []string{"download", "import", "export"} {
		if got.Actions[action].Supported {
			t.Fatalf("%s action = %#v, want unsupported", action, got.Actions[action])
		}
	}
}

func TestResourceActionsStorageItemSupportsDownloadOnly(t *testing.T) {
	svc := NewResourceActionService(fakeResourceActionSystemClient{
		engines: map[uint]*commonModels.Engine{
			3: {
				ID:             3,
				TenantID:       uintPtr(7),
				EngineType:     "minio",
				LifecycleState: "active",
			},
		},
	})

	got, err := svc.GetResourceActions(t.Context(), "addp://engine/3/path/data/roads.shp?type=object&item_id=8", uintPtr(7))
	if err != nil {
		t.Fatalf("GetResourceActions() error = %v", err)
	}
	if got.EngineCategory != "storage" || got.Kind != "item" {
		t.Fatalf("category/kind = %s/%s, want storage/item", got.EngineCategory, got.Kind)
	}
	if !got.Actions["download"].Supported {
		t.Fatalf("download action = %#v, want supported", got.Actions["download"])
	}
	for _, action := range []string{"upload", "import", "export"} {
		if got.Actions[action].Supported {
			t.Fatalf("%s action = %#v, want unsupported", action, got.Actions[action])
		}
	}
}

func TestResourceActionsDatabaseNodeSupportsImportOnly(t *testing.T) {
	svc := NewResourceActionService(fakeResourceActionSystemClient{
		engines: map[uint]*commonModels.Engine{
			8: {
				ID:             8,
				TenantID:       uintPtr(7),
				EngineType:     "postgresql",
				LifecycleState: "active",
			},
		},
	})

	got, err := svc.GetResourceActions(t.Context(), "addp://engine/8/path/public?type=schema&node_id=18", uintPtr(7))
	if err != nil {
		t.Fatalf("GetResourceActions() error = %v", err)
	}
	if got.EngineCategory != "database" || got.Kind != "node" {
		t.Fatalf("category/kind = %s/%s, want database/node", got.EngineCategory, got.Kind)
	}
	if !got.Actions["import"].Supported {
		t.Fatalf("import action = %#v, want supported", got.Actions["import"])
	}
	if len(got.Actions["import"].Formats) == 0 {
		t.Fatalf("import formats are empty")
	}
	for _, action := range []string{"upload", "download", "export"} {
		if got.Actions[action].Supported {
			t.Fatalf("%s action = %#v, want unsupported", action, got.Actions[action])
		}
	}
}

func TestResourceActionsDatabaseItemSupportsExportOnly(t *testing.T) {
	svc := NewResourceActionService(fakeResourceActionSystemClient{
		engines: map[uint]*commonModels.Engine{
			8: {
				ID:             8,
				TenantID:       uintPtr(7),
				EngineType:     "postgresql",
				LifecycleState: "active",
			},
		},
	})

	got, err := svc.GetResourceActions(t.Context(), "addp://engine/8/path/public/roads?type=table&item_id=54", uintPtr(7))
	if err != nil {
		t.Fatalf("GetResourceActions() error = %v", err)
	}
	if got.EngineCategory != "database" || got.Kind != "item" {
		t.Fatalf("category/kind = %s/%s, want database/item", got.EngineCategory, got.Kind)
	}
	if !got.Actions["export"].Supported {
		t.Fatalf("export action = %#v, want supported", got.Actions["export"])
	}
	if len(got.Actions["export"].Formats) == 0 {
		t.Fatalf("export formats are empty")
	}
	hasShapefile := false
	for _, f := range got.Actions["export"].Formats {
		if f == "shapefile" {
			hasShapefile = true
		}
	}
	if !hasShapefile {
		t.Fatalf("export formats = %#v, want shapefile multi-ref export format", got.Actions["export"].Formats)
	}
	for _, action := range []string{"upload", "download", "import"} {
		if got.Actions[action].Supported {
			t.Fatalf("%s action = %#v, want unsupported", action, got.Actions[action])
		}
	}
}

func TestResourceActionsMongoCollectionSupportsCanonicalExtendedJSONExport(t *testing.T) {
	caps := plugin.NewDynamicSchemaCapabilities("mongodb")
	caps.Storage.Store.EncodedRecordReadSession = &plugin.EncodedRecordReadSessionCapability{
		Formats: []string{"mongodb_extended_jsonl"},
	}
	rawBytes, err := json.Marshal(caps)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	raw := commonModels.JSONString(rawBytes)
	svc := NewResourceActionService(fakeResourceActionSystemClient{engines: map[uint]*commonModels.Engine{
		11: {
			ID: 11, TenantID: uintPtr(7), EngineType: "mongodb", LifecycleState: "active", Capabilities: &raw,
		},
	}})

	got, err := svc.GetResourceActions(t.Context(), "addp://engine/11/path/Outdoor/Persons?type=collection&item_id=81", uintPtr(7))
	if err != nil {
		t.Fatalf("GetResourceActions() error = %v", err)
	}
	export := got.Actions["export"]
	if got.Kind != "item" || !export.Supported {
		t.Fatalf("response = %#v, want collection export supported", got)
	}
	if len(export.Formats) != 1 || export.Formats[0] != "mongodb_extended_jsonl" {
		t.Fatalf("formats = %#v", export.Formats)
	}
}

func TestResourceActionsRespectsInactiveEngine(t *testing.T) {
	svc := NewResourceActionService(fakeResourceActionSystemClient{
		engines: map[uint]*commonModels.Engine{
			3: {
				ID:             3,
				EngineType:     "minio",
				LifecycleState: "disabled",
			},
		},
	})

	_, err := svc.GetResourceActions(t.Context(), "addp://engine/3/path/data/raw?type=prefix&node_id=20", nil)
	if err != ErrEngineAccessDenied {
		t.Fatalf("GetResourceActions() error = %v, want ErrEngineAccessDenied", err)
	}
}

func TestResourceActionsRejectsOfflineEngineAsUnavailable(t *testing.T) {
	svc := NewResourceActionService(fakeResourceActionSystemClient{
		engines: map[uint]*commonModels.Engine{
			3: {
				ID:               3,
				TenantID:         uintPtr(7),
				EngineType:       "minio",
				LifecycleState:   commonModels.EngineLifecycleActive,
				ConnectionStatus: commonModels.EngineConnectionOffline,
			},
		},
	})

	_, err := svc.GetResourceActions(t.Context(), "addp://engine/3/path/data/raw?type=prefix&node_id=20", uintPtr(7))
	if !errors.Is(err, engineaccess.ErrUnavailable) {
		t.Fatalf("GetResourceActions() error = %v, want ErrUnavailable", err)
	}
}

func TestResourceActionsUsesStoredCapabilities(t *testing.T) {
	caps := plugin.NewObjectCapabilities("custom_object")
	raw := commonModels.JSONString(``)
	if b, err := json.Marshal(caps); err == nil {
		raw = commonModels.JSONString(b)
	}
	svc := NewResourceActionService(fakeResourceActionSystemClient{
		engines: map[uint]*commonModels.Engine{
			9: {
				ID:             9,
				TenantID:       uintPtr(7),
				EngineType:     "custom_object",
				LifecycleState: "active",
				Capabilities:   &raw,
			},
		},
	})

	got, err := svc.GetResourceActions(t.Context(), "addp://engine/9/path/bucket?type=bucket&node_id=1", uintPtr(7))
	if err != nil {
		t.Fatalf("GetResourceActions() error = %v", err)
	}
	if got.EngineCategory != "storage" || !got.Actions["upload"].Supported {
		t.Fatalf("response = %#v, want storage upload supported", got)
	}
}
