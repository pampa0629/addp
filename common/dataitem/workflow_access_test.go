package dataitem_test

import (
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/workflowaccess"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

func TestTIFFPAMUsesOnlyRegisteredSameNameCompanion(t *testing.T) {
	for _, object := range []bool{false, true} {
		for _, refPath := range []string{"", "images/a.tif.aux.xml", "images/other.tif.aux.xml"} {
			locator := &resourcetree.ResourceLocator{EngineID: 3, Type: resourcetree.TypeFile, Path: []string{"images", "a.tif"}}
			access := workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: "/data/images/a.tif"}
			if object {
				access = workflowaccess.Access{Method: workflowaccess.MethodObjectStore, Endpoint: "minio:9000", Bucket: "data", Object: "images/a.tif", AccessKey: "key", SecretKey: "secret"}
			}
			item := &commonModels.MetaItem{EngineID: 3, FullName: locator.FullName(), Attributes: commonModels.JSONMap{"item": map[string]interface{}{"refs": []interface{}{map[string]interface{}{"role": "auxiliary_metadata", "path": refPath}}}}}
			if refPath == "" {
				item.Attributes = nil
			}
			source, err := dataitem.WithTIFFPAM(workflowaccess.Source{Kind: workflowaccess.KindFile, Format: "tiff", Access: access}, locator, item)
			if refPath == "images/other.tif.aux.xml" {
				if err == nil {
					t.Fatal("silently ignored registered PAM outside the supported read profile")
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			expectedCount := 0
			if refPath == "images/a.tif.aux.xml" {
				expectedCount = 1
			}
			if len(source.RelatedFiles) != expectedCount {
				t.Fatalf("related files = %#v", source.RelatedFiles)
			}
			if _, err := workflowaccess.NewSourcePlan(source); err != nil {
				t.Fatal(err)
			}
			if expectedCount == 1 {
				source.RelatedFiles[0].Access.SecretKey = "different"
				if _, err := workflowaccess.NewSourcePlan(source); err == nil {
					t.Fatal("accepted different connection")
				}
			}
			item.EngineID = 4
			if _, err := dataitem.WithTIFFPAM(source, locator, item); err == nil {
				t.Fatal("accepted different item")
			}
		}
	}
}

func TestTIFFPAMDoesNotDropOtherRegisteredComponents(t *testing.T) {
	locator := &resourcetree.ResourceLocator{EngineID: 3, Type: resourcetree.TypeFile, Path: []string{"images", "a.tif"}}
	source := workflowaccess.Source{Kind: workflowaccess.KindFile, Format: "tiff",
		Access: workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: "/data/images/a.tif"}}
	refs := []interface{}{
		map[string]interface{}{"role": "main", "path": "images/a.tif", "primary": true, "required": true},
		map[string]interface{}{"role": "auxiliary_metadata", "path": "images/a.tif.aux.xml"},
	}
	item := &commonModels.MetaItem{EngineID: 3, FullName: locator.FullName(),
		Attributes: commonModels.JSONMap{"item": map[string]interface{}{"refs": refs}}}
	if prepared, err := dataitem.WithTIFFPAM(source, locator, item); err != nil || len(prepared.RelatedFiles) != 1 {
		t.Fatalf("registered TIFF/PAM = %#v, %v", prepared, err)
	}
	item.Attributes["item"].(map[string]interface{})["refs"] = append(refs,
		map[string]interface{}{"role": "world_file", "path": "images/a.tfw"})
	if _, err := dataitem.WithTIFFPAM(source, locator, item); err == nil {
		t.Fatal("silently dropped the world file from the declared source")
	}
}
