package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
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

// Exercise the actual bucket-relative detector -> persisted full-name projection
// before consuming the exact pair. Hand-assembled fully qualified refs hide this seam.
type enviObjectProofReader struct {
	plugin.StoreProvider
	files map[string][]byte
}

func (*enviObjectProofReader) Type() string { return "minio" }
func (p *enviObjectProofReader) OpenContent(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	raw, ok := p.files[target.StringPath()]
	if !ok {
		return nil, fmt.Errorf("undeclared fixture object")
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}
func (p *enviObjectProofReader) OpenRange(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, options plugin.ReadOptions) (io.ReadCloser, error) {
	raw, ok := p.files[target.StringPath()]
	if !ok || options.Offset < 0 || options.Length < 0 || options.Offset > int64(len(raw)) {
		return nil, io.EOF
	}
	return io.NopCloser(bytes.NewReader(raw[options.Offset:min(int64(len(raw)), options.Offset+options.Length)])), nil
}
func TestRuntimeENVISourceAfterObjectDetectionAndQualification(t *testing.T) {
	metaenrich.RegisterItemResolvers()
	const header = "ENVI\nsamples = 4\nlines = 1\nbands = 1\nheader offset = 0\nfile type = ENVI Standard\ndata type = 1\ninterleave = bsq\nbyte order = 0\n"
	for _, bucket := range []string{"fixture", "envi"} {
		for _, prefix := range []string{"envi/", "fixture/envi/"} {
			for _, deep := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/deep=%t", bucket, prefix, deep), func(t *testing.T) {
					engine, _ := observedENVIMediaSource(true)
					main, hdr := prefix+" 原名 %.img", prefix+" 原名 %.HDR"
					reader := &enviObjectProofReader{files: map[string][]byte{bucket + "/" + main: {1, 2, 3, 4}, bucket + "/" + hdr: []byte(header)}}
					resources := []scanresource.StorageResource{}
					for _, name := range []string{main, hdr} {
						resources = append(resources, scanresource.StorageResource{RootName: bucket, Path: name, FullPath: bucket + "/" + name, NodeType: "object", EngineCatalogPath: plugin.ObjectItemPath(engine.ID, bucket, name), SizeBytes: int64(len(reader.files[bucket+"/"+name]))})
					}
					_, items, warnings := scanflow.DetectObjectCatalogCompositeItems(t.Context(), reader, reader, plugin.ConnectionInfo{}, engine.ID, resources, deep)
					if len(warnings) != 0 || len(items) != 1 {
						t.Fatalf("detection: %v %v", items, warnings)
					}
					detected := items[0].Item
					for attempt := 0; attempt < 2; attempt++ {
						projected, ok := scanresource.PlanObjectCompositeItem(engine.ID, items[0], "object")
						if !ok {
							t.Fatal("missing object projection")
						}
						if got := commonJSON.String(projected.Attributes, "format_info.envi", "header_path"); got != bucket+"/"+hdr {
							t.Fatalf("projected header=%q, want exact %q", got, bucket+"/"+hdr)
						}
						plan, observed, err := runtimeMediaSourcePlan(engine, projected.DataItem, projected.FullName)
						if err != nil || plan.Validate() != nil || observed != bucket+"/"+hdr || plan.Source.Access.Bucket != bucket || plan.Source.Access.Object != main || len(plan.Source.RelatedFiles) != 1 || plan.Source.RelatedFiles[0].Access.Bucket != bucket || plan.Source.RelatedFiles[0].Access.Object != hdr {
							t.Fatalf("observed pair changed between detection and Runtime: %+v %q %v", plan, observed, err)
						}
						if commonJSON.String(detected.Attributes, "format_info.envi", "header_path") != hdr || detected.PrimaryContentPath != main {
							t.Fatal("projection mutated the detector's bucket-relative proof")
						}
						projected.DataItem.Attributes["format_info"].(map[string]interface{})["envi"].(map[string]interface{})["header_path"] = bucket + "/" + prefix + "other.HDR"
						if _, _, err := runtimeMediaSourcePlan(engine, projected.DataItem, projected.FullName); err == nil {
							t.Fatal("mismatched header proof was repaired from refs instead of rejected")
						}
					}
				})
			}
		}
	}
}
