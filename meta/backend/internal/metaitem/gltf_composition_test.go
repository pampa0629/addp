package metaitem

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/addp/common/engine/plugin"
)

type gltfManifestReadSpy struct {
	refMapContentReader
	raw    []byte
	opened []string
}

func (s *gltfManifestReadSpy) OpenContent(_ context.Context, _ plugin.ConnectionInfo, p plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	s.opened = append(s.opened, p.StringPath())
	return io.NopCloser(bytes.NewReader(s.raw)), nil
}

func TestGLTFBasicClaimsOnlyExactObservedDecodedNames(t *testing.T) {
	for _, fault := range []string{"none", "missing", "wrong case", "short buffer", "invalid manifest", "embedded only"} {
		t.Run(fault, func(t *testing.T) {
			main := "models/ scene %.gltf"
			raw := []byte(`{"asset":{"version":"2.0"},"buffers":[{"uri":"buffers/literal%2520.mesh","byteLength":4}],"images":[{"uri":"textures/%20image.png"}]}`)
			observed := map[string]StorageFileRef{
				"models/buffers/literal%20.mesh": {Path: "models/buffers/literal%20.mesh", Size: 4},
				"models/textures/ image.png":     {Path: "models/textures/ image.png", Size: 8},
				"models/buffers/literal .mesh":   {Path: "models/buffers/literal .mesh", Size: 999},
			}
			switch fault {
			case "missing":
				delete(observed, "models/textures/ image.png")
			case "wrong case":
				delete(observed, "models/buffers/literal%20.mesh")
				observed["models/buffers/Literal%20.mesh"] = StorageFileRef{Path: "models/buffers/Literal%20.mesh", Size: 4}
			case "short buffer":
				observed["models/buffers/literal%20.mesh"] = StorageFileRef{Path: "models/buffers/literal%20.mesh", Size: 3}
			case "invalid manifest":
				raw = []byte(`{"asset":{"version":"2.0"},"buffers":[`)
			case "embedded only":
				raw = []byte(`{"asset":{"version":"2.0"}}`)
			}
			spy := &gltfManifestReadSpy{raw: raw}
			items := resolveGLTFManifestItems(t.Context(), DirectoryResolveInput{EngineID: 33, ContentReader: spy, ModelReferenceFiles: observed}, []StorageFileRef{{Name: " scene %.gltf", Path: main, Size: int64(len(raw))}})
			if len(items) != 1 || len(spy.opened) != 1 || spy.opened[0] != main {
				t.Fatal("scan read beyond exact primary", spy.opened)
			}
			item := items[0]
			native := item.Attributes["format_info"].(map[string]interface{})["gltf"].(map[string]interface{})
			complete := fault == "none" || fault == "embedded only"
			if native["composition_complete"] != complete || item.Layout != "multi" || item.RefList[0].Role != "manifest" {
				t.Fatal("wrong identity or completeness", item)
			}
			count := 1
			if fault == "none" {
				count = 3
			}
			if len(item.RefList) != count || len(item.ClaimPaths) != count {
				t.Fatal("failed observation retained partial or stale members", item.RefList, item.ClaimPaths)
			}
			for _, ref := range item.RefList {
				if ref.Path == "models/buffers/literal .mesh" {
					t.Fatal("URI decoded twice")
				}
			}
		})
	}
}

func TestGLTFBasicRejectsAProviderMappingToAnotherPrimaryBeforeIO(t *testing.T) {
	spy := &gltfManifestReadSpy{raw: []byte(`{"asset":{"version":"2.0"}}`)}
	items := resolveGLTFManifestItems(t.Context(), DirectoryResolveInput{EngineID: 33, ContentReader: spy, EngineCatalogPathFor: func(string) plugin.EngineCatalogPath { return plugin.FileItemPath(33, "other/scene.gltf") }}, []StorageFileRef{{Name: "scene.gltf", Path: "models/scene.gltf", Size: int64(len(spy.raw))}})
	if len(spy.opened) != 0 || len(items) != 1 || items[0].Attributes["format_info"].(map[string]interface{})["gltf"].(map[string]interface{})["composition_complete"] != false {
		t.Fatal("wrong native mapping opened content or produced complete facts")
	}
}
