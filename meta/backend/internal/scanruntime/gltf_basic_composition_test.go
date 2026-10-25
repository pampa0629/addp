package scanruntime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
)

type gltfFileStructureFixture struct {
	filesystemScanTestProvider
	primary      string
	raw          []byte
	opened       []string
	sequential   int
	allowContent bool
}

func (p *gltfFileStructureFixture) Type() string { return "ftp" }

func (p *gltfFileStructureFixture) OpenContent(ctx context.Context, conn plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	p.sequential++
	if p.allowContent {
		return p.filesystemScanTestProvider.OpenContent(ctx, conn, target, opts)
	}
	return nil, fmt.Errorf("glTF basic composition must not use deep content")
}

func (p *gltfFileStructureFixture) OpenRange(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	p.opened = append(p.opened, target.StringPath())
	if target.StringPath() != p.primary || opts.Offset < 0 || opts.Length <= 0 || opts.Offset+opts.Length > int64(len(p.raw)) {
		return nil, fmt.Errorf("glTF structure read escaped the exact primary")
	}
	return io.NopCloser(bytes.NewReader(p.raw[opts.Offset : opts.Offset+opts.Length])), nil
}

func hostedGLTFManifest(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../../business/fixtures/manager/addp_online_model_fixture.gltf")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestGLTFFileScanPlanKeepsBasicAndDeepCompositionIdentical(t *testing.T) {
	raw := hostedGLTFManifest(t)
	for _, kind := range []string{"ftp", "nfs"} {
		for _, depth := range []string{"basic", "deep"} {
			t.Run(kind+"/"+depth, func(t *testing.T) {
				const main = "models/gltf/model.gltf"
				primary := progressFile(main)
				*primary.Storage.SizeBytes = int64(len(raw))
				buffer := progressFile("models/gltf/buffers/ geometry%20.mesh")
				*buffer.Storage.SizeBytes = 132
				p := &gltfFileStructureFixture{primary: main, raw: raw, filesystemScanTestProvider: filesystemScanTestProvider{entriesByPath: map[string][]plugin.EngineCatalogEntry{
					"models":              {progressDirectory("models/gltf")},
					"models/gltf":         {primary, progressDirectory("models/gltf/buffers"), progressDirectory("models/gltf/red"), progressDirectory("models/gltf/blue")},
					"models/gltf/buffers": {buffer},
					"models/gltf/red":     {progressFile("models/gltf/red/color.png")},
					"models/gltf/blue":    {progressFile("models/gltf/blue/color.png")},
				}}}
				ctx, _ := scanflow.WithFileProgress(t.Context(), nil)
				plan, err := (&FilesystemCatalogRuntime{}).prepareFileScan(ctx, p, p, nil, &commonModels.Engine{ID: 26, EngineType: kind}, []string{"models"}, depth)
				if err != nil {
					t.Fatal(err)
				}
				var found []*metaitem.DetectedItem
				for _, directory := range plan.directories {
					if directory.detection != nil {
						for _, item := range directory.detection.Items {
							if item.PrimaryContentPath == main {
								found = append(found, item)
							}
						}
					}
				}
				if len(found) != 1 || found[0].Layout != "multi" || len(found[0].RefList) != 4 || commonJSON.Section(found[0].Attributes, "format_info.gltf")["composition_complete"] != true {
					t.Fatalf("%s scan lost complete exact glTF: %+v", depth, found)
				}
				if p.sequential != 0 || len(p.opened) == 0 {
					t.Fatalf("wrong structure route: sequential=%d ranges=%v", p.sequential, p.opened)
				}
				for _, name := range p.opened {
					if name != main {
						t.Fatal("dependency read during identification", name)
					}
				}
			})
		}
	}
}

func TestMinIOGLTFBasicCompositionUsesOnlyObservedManifest(t *testing.T) {
	raw := hostedGLTFManifest(t)
	for _, prefix := range []string{"", "models/gltf/"} {
		for _, missing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/missing=%v", prefix, missing), func(t *testing.T) {
				primary := prefix + "model.gltf"
				reader := &objectModelStructureSpy{primary: plugin.ObjectItemPath(9, "addp", primary), data: raw}
				resources := []scanresource.StorageResource{objectResourceForTest(9, "addp", primary, int64(len(raw)), "")}
				for _, name := range []string{"buffers/ geometry%20.mesh", "red/color.png", "blue/color.png", "unused.png"} {
					if missing && name == "blue/color.png" {
						continue
					}
					resources = append(resources, objectResourceForTest(9, "addp", prefix+name, 132, ""))
				}
				claims, items, warnings := scanflow.DetectObjectCatalogCompositeItems(t.Context(), reader, reader, nil, 9, resources, false)
				if len(warnings) != 0 || len(items) != 1 || reader.ranges == 0 || reader.contentCalls != 0 {
					t.Fatalf("wrong basic route: items=%+v warnings=%v reads=%d/%d", items, warnings, reader.ranges, reader.contentCalls)
				}
				item := items[0].Item
				want := 4
				if missing {
					want = 1
				}
				if item.Layout != "multi" || len(item.RefList) != want || commonJSON.Section(item.Attributes, "format_info.gltf")["composition_complete"] != !missing || !claims[primary] || claims[prefix+"unused.png"] {
					t.Fatalf("wrong basic glTF facts/claims: item=%+v claims=%v", item, claims)
				}
			})
		}
	}
}
