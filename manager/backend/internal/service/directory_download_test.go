package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
)

func TestFileGDBDirectoryDownloadPlanDoesNotTreatRootAsFile(t *testing.T) {
	item := &commonModels.MetaItem{Attributes: commonModels.JSONMap{
		"item": map[string]interface{}{"layout": "whole", "data_type": "container", "format": "filegdb"},
	}}
	root := "data/ 原名 %.gdb"
	plan, err := (&MetadataService{}).resolveStorageDownloadPlan(t.Context(), 9, "nfs", root, item, nil)
	if err != nil || plan.Kind != models.DownloadKindBundle || plan.FileName != " 原名 %.gdb.zip" ||
		plan.ContentType != "application/zip" || len(plan.Refs) != 0 ||
		!reflect.DeepEqual(plan.DirectoryRoot, pointerCatalogPath(plugin.FileDirectoryPath(9, root))) {
		t.Fatalf("whole container was not planned as directory ZIP: plan=%+v error=%v", plan, err)
	}
}

func pointerCatalogPath(value plugin.EngineCatalogPath) *plugin.EngineCatalogPath { return &value }

func TestFileGDBResourceActionUsesRealMetaDownloadPlanWithoutSourceIO(t *testing.T) {
	for _, kind := range []string{"filegdb", "osgb"} {
		t.Run(kind, func(t *testing.T) {
			system := testSystemClient(t, 9, "nfs", nil)
			meta := testMetaItemByIDClient(t, `{"id":1,"tenant_id":11,"engine_id":9,"item_type":"file","full_name":"data/test.gdb","attributes":{"item":{"layout":"whole","data_type":"container","format":"`+kind+`"}}}`)
			metadata := NewMetadataService(nil, system, meta, nil, nil)
			actions := NewResourceActionService(system, metadata)
			got, err := actions.GetResourceActions(t.Context(), "addp://engine/9/path/data/test.gdb?type=file&item_id=1", uintPtr(11))
			if err != nil {
				t.Fatal(err)
			}
			want := kind == "filegdb"
			if got.Actions["download"].Supported != want || got.Actions["download"].RequiresCurrentUser != want {
				t.Fatalf("capability diverged from actual plan: %+v", got.Actions["download"])
			}
		})
	}
}

func TestResourceActionCannotTreatUnknownDownloadPlanAsSupported(t *testing.T) {
	for _, scenario := range []string{"unsupported", "unavailable", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			upstream := errors.New("Meta unavailable")
			system := fakeResourceActionSystemClient{engines: map[uint]*commonModels.Engine{9: {ID: 9, TenantID: uintPtr(11), EngineType: "nfs", LifecycleState: "active"}}}
			planner := actionDownloadPlan(func(context.Context, string, *uint) (*StorageDataItem, *models.DownloadPlan, error) {
				if scenario == "unsupported" {
					return nil, nil, ErrDownloadNotSupported
				}
				if scenario == "unavailable" {
					return nil, nil, upstream
				}
				return nil, nil, nil
			})
			got, err := NewResourceActionService(system, planner).GetResourceActions(t.Context(), "addp://engine/9/path/data/test.gdb?type=file&item_id=1", uintPtr(11))
			if scenario == "unsupported" {
				if err != nil || got.Actions["download"].Supported {
					t.Fatalf("unsupported plan: %+v %v", got, err)
				}
				return
			}
			if err == nil || got != nil {
				t.Fatal("unknown plan became a download capability")
			}
			if scenario == "unavailable" && !errors.Is(err, upstream) {
				t.Fatal("upstream failure hidden")
			}
		})
	}
}

func TestDirectoryDownloadPlanDoesNotEnableOtherWholeFormatsOrEngines(t *testing.T) {
	for _, scenario := range []string{"other engine", "other whole", "wrong type", "mixed refs", "invalid root"} {
		t.Run(scenario, func(t *testing.T) {
			engine, root := "nfs", "data/test.gdb"
			attrs := map[string]interface{}{"layout": "whole", "data_type": "container", "format": "filegdb"}
			switch scenario {
			case "other engine":
				engine = "minio"
			case "other whole":
				attrs["format"] = "osgb"
			case "wrong type":
				attrs["data_type"] = "model_3d"
			case "mixed refs":
				attrs["refs"] = []interface{}{map[string]interface{}{"path": "data/other.bin", "primary": true, "required": true}}
			case "invalid root":
				root = "data/other"
			}
			_, err := (&MetadataService{}).resolveStorageDownloadPlan(t.Context(), 9, engine, root, &commonModels.MetaItem{Attributes: commonModels.JSONMap{"item": attrs}}, nil)
			if !errors.Is(err, ErrDownloadNotSupported) {
				t.Fatalf("unsupported directory plan accepted: %v", err)
			}
		})
	}
}
