package dataitem_test

import (
	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/workflowaccess"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"path"
	"reflect"
	"testing"
)

func TestTIFFComponentsPreserveEveryRegisteredSibling(t *testing.T) {
	companions := []struct{ name, role string }{
		{"a.tfw", "world_file"}, {"a.hdr", "header"}, {"a.tif.aux.xml", "auxiliary_metadata"},
		{"a.prj", "crs"}, {"a.tif.ovr", "overview"}, {"a.tif.msk", "mask"},
	}
	for _, object := range []bool{false, true} {
		for count := 0; count <= len(companions); count++ {
			locator := &resourcetree.ResourceLocator{EngineID: 3, Type: resourcetree.TypeFile, Path: []string{"images", "a.tif"}}
			access := workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: "/data/images/a.tif"}
			if object {
				access = workflowaccess.Access{Method: workflowaccess.MethodObjectStore, Endpoint: "minio:9000", Bucket: "data", Object: "images/a.tif", AccessKey: "key", SecretKey: "secret"}
			}
			refs := []interface{}{map[string]interface{}{"role": "main", "path": locator.FullName(), "primary": true, "required": true}}
			for _, member := range companions[:count] {
				refs = append(refs, map[string]interface{}{"role": member.role, "path": "images/" + member.name})
			}
			item := &commonModels.MetaItem{EngineID: 3, FullName: locator.FullName(), Attributes: commonModels.JSONMap{"item": map[string]interface{}{"refs": refs}}}
			source, err := dataitem.WithTIFFComponents(workflowaccess.Source{Kind: workflowaccess.KindFile, Format: "tiff", Access: access}, locator, item)
			if err != nil || len(source.RelatedFiles) != count {
				t.Fatalf("composition=%#v err=%v", source, err)
			}
			if _, err := workflowaccess.NewSourcePlan(source); err != nil {
				t.Fatal(err)
			}
			for i, member := range source.RelatedFiles {
				expected := access
				if object {
					expected.Object = path.Join(path.Dir(access.Object), companions[i].name)
				} else {
					expected.Path = path.Join(path.Dir(access.Path), companions[i].name)
				}
				if member.Role != companions[i].role || !reflect.DeepEqual(member.Access, expected) {
					t.Fatal("lost exact registered member", member)
				}
			}
			item.EngineID = 4
			if _, err := dataitem.WithTIFFComponents(source, locator, item); err == nil {
				t.Fatal("accepted another item")
			}
		}
	}
}

func TestTIFFPlanRejectsForeignOrDuplicateMembers(t *testing.T) {
	for name, mutate := range map[string]func(*workflowaccess.Source){
		"connection": func(s *workflowaccess.Source) { s.RelatedFiles[0].Access.SecretKey = "other" },
		"parent":     func(s *workflowaccess.Source) { s.RelatedFiles[0].Access.Path = "/other/a.tfw" },
		"traversal":  func(s *workflowaccess.Source) { s.RelatedFiles[0].Access.Path = "/data/../data/a.tfw" },
		"role":       func(s *workflowaccess.Source) { s.RelatedFiles[0].Role = "texture" },
		"duplicate":  func(s *workflowaccess.Source) { s.RelatedFiles = append(s.RelatedFiles, s.RelatedFiles[0]) },
		"primary":    func(s *workflowaccess.Source) { s.RelatedFiles[0].Access = s.Access },
	} {
		t.Run(name, func(t *testing.T) {
			source := workflowaccess.Source{Kind: workflowaccess.KindFile, Format: "tiff", Access: workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: "/data/a.tif"}, RelatedFiles: []workflowaccess.RelatedFile{{Role: "world_file", Access: workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: "/data/a.tfw"}}}}
			mutate(&source)
			if _, err := workflowaccess.NewSourcePlan(source); err == nil {
				t.Fatal("accepted invalid composition")
			}
		})
	}
}
