package service

import (
	"encoding/json"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
)

type fakeResourceActionSystemClient struct {
	engines map[uint]*commonModels.Engine
}

func (c fakeResourceActionSystemClient) GetEngine(engineID uint) (*commonModels.Engine, error) {
	return c.engines[engineID], nil
}

func TestResourceActionsStorageNodeSupportsUploadOnly(t *testing.T) {
	svc := NewResourceActionService(fakeResourceActionSystemClient{
		engines: map[uint]*commonModels.Engine{
			3: {
				ID:         3,
				EngineType: "minio",
				IsActive:   true,
			},
		},
	})

	got, err := svc.GetResourceActions(t.Context(), "addp://engine/3/path/data/raw?type=prefix&node_id=20", nil)
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
				ID:         3,
				EngineType: "minio",
				IsActive:   true,
			},
		},
	})

	got, err := svc.GetResourceActions(t.Context(), "addp://engine/3/path/data/roads.shp?type=object&item_id=8", nil)
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
				ID:         8,
				EngineType: "postgresql",
				IsActive:   true,
			},
		},
	})

	got, err := svc.GetResourceActions(t.Context(), "addp://engine/8/path/public?type=schema&node_id=18", nil)
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
				ID:         8,
				EngineType: "postgresql",
				IsActive:   true,
			},
		},
	})

	got, err := svc.GetResourceActions(t.Context(), "addp://engine/8/path/public/roads?type=table&item_id=54", nil)
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

func TestResourceActionsRespectsInactiveEngine(t *testing.T) {
	svc := NewResourceActionService(fakeResourceActionSystemClient{
		engines: map[uint]*commonModels.Engine{
			3: {
				ID:         3,
				EngineType: "minio",
				IsActive:   false,
			},
		},
	})

	_, err := svc.GetResourceActions(t.Context(), "addp://engine/3/path/data/raw?type=prefix&node_id=20", nil)
	if err != ErrEngineAccessDenied {
		t.Fatalf("GetResourceActions() error = %v, want ErrEngineAccessDenied", err)
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
				ID:           9,
				EngineType:   "custom_object",
				IsActive:     true,
				Capabilities: &raw,
			},
		},
	})

	got, err := svc.GetResourceActions(t.Context(), "addp://engine/9/path/bucket?type=bucket&node_id=1", nil)
	if err != nil {
		t.Fatalf("GetResourceActions() error = %v", err)
	}
	if got.EngineCategory != "storage" || !got.Actions["upload"].Supported {
		t.Fatalf("response = %#v, want storage upload supported", got)
	}
}
