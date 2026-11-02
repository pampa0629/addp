package service

import (
	"strings"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/envi"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
)

func observedENVIMediaSource(object bool) (*commonModels.Engine, *metaitem.DetectedItem) {
	engine := &commonModels.Engine{ID: 14, EngineType: "nfs", ConnectionInfo: commonModels.ConnectionInfo{"server": "nfs", "export_path": "/data", "nfs_version": "4"}}
	prefix := "imagery/"
	if object {
		engine.EngineType = "minio"
		engine.ConnectionInfo = commonModels.ConnectionInfo{"endpoint": "http://storage:9000", "access_key": "key", "secret_key": "secret"}
		prefix = "bucket/imagery/"
	}
	main, header := prefix+" 原名 %.img", prefix+" 原名 %.HDR"
	item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Format: "envi", DataType: datatype.Media, Layout: format.LayoutMulti, PrimaryContentPath: main, RefList: []dataitem.ItemRef{{Path: main, Role: "raster", Required: true, Primary: true}, {Path: header, Role: "header", Required: true}}}, Attributes: map[string]interface{}{"format_info": map[string]interface{}{"envi": map[string]interface{}{"profile": envi.Profile, "composition_complete": true, "header_path": header, "header_sha256": strings.Repeat("a", 64)}}}}
	return engine, item
}

func TestRuntimeMediaSourceCarriesTheFrozenObservedHeader(t *testing.T) {
	for _, object := range []bool{false, true} {
		engine, item := observedENVIMediaSource(object)
		plan, header, err := runtimeMediaSourcePlan(engine, item, item.PrimaryContentPath)
		if err != nil || plan.Validate() != nil || header != item.RefList[1].Path || len(plan.Source.RelatedFiles) != 1 || plan.Source.RelatedFiles[0].Role != "header" || plan.Source.Metadata["expected_header_sha256"] != strings.Repeat("a", 64) {
			t.Fatalf("lost ENVI observed pair: %+v %s %v", plan, header, err)
		}
		if object {
			if plan.Source.Access.Object != "imagery/ 原名 %.img" || plan.Source.RelatedFiles[0].Access.Object != "imagery/ 原名 %.HDR" {
				t.Fatal("object names rewritten")
			}
		} else if plan.Source.Access.Path != "/data/imagery/ 原名 %.img" || plan.Source.RelatedFiles[0].Access.Path != "/data/imagery/ 原名 %.HDR" {
			t.Fatal("file names rewritten")
		}
	}
}

func TestRuntimeMediaSourceRefusesIncompleteOrChangedComposition(t *testing.T) {
	for name, mutate := range map[string]func(*commonModels.Engine, *metaitem.DetectedItem){
		"missing header":   func(_ *commonModels.Engine, item *metaitem.DetectedItem) { item.RefList = item.RefList[:1] },
		"wrong role":       func(_ *commonModels.Engine, item *metaitem.DetectedItem) { item.RefList[1].Role = "texture" },
		"wrong primary":    func(_ *commonModels.Engine, item *metaitem.DetectedItem) { item.RefList[0].Primary = false },
		"different header": func(_ *commonModels.Engine, item *metaitem.DetectedItem) { item.RefList[1].Path = "imagery/other.HDR" },
		"incomplete": func(_ *commonModels.Engine, item *metaitem.DetectedItem) {
			item.Attributes["format_info"].(map[string]interface{})["envi"].(map[string]interface{})["composition_complete"] = false
		},
		"single layout": func(_ *commonModels.Engine, item *metaitem.DetectedItem) { item.Layout = format.LayoutSingle },
		"wrong engine":  func(engine *commonModels.Engine, _ *metaitem.DetectedItem) { engine.EngineType = "ftp" },
	} {
		t.Run(name, func(t *testing.T) {
			engine, item := observedENVIMediaSource(false)
			mutate(engine, item)
			if _, _, err := runtimeMediaSourcePlan(engine, item, item.PrimaryContentPath); err == nil {
				t.Fatal("unproven source reached runtime binding")
			}
		})
	}
}
