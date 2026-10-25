package service

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"path"
	"strings"
	"testing"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
)

func TestGLTFRawDownloadReprovesManifestBeforeAnyDependency(t *testing.T) {
	for _, kind := range []string{"ftp", "nfs", "minio"} {
		for _, fault := range []string{"none", "changed references", "remote reference", "denied", "embedded only"} {
			t.Run(kind+"/"+fault, func(t *testing.T) {
				svc, target, provider, tenant, _ := storageModelFixture(t, kind == "minio", kind)
				name := strings.TrimSuffix(target.StorageRef, ".ifc") + ".gltf"
				doc := `{"asset":{"version":"2.0"},"buffers":[{"uri":"buffers/literal%2520.mesh","byteLength":4}]}`
				if fault == "embedded only" {
					doc = `{"asset":{"version":"2.0"}}`
				}
				parsed, err := format.ParseGLTFComposition(t.Context(), []byte(doc))
				if err != nil {
					t.Fatal(err)
				}
				refs, err := dataitem.GLTFCompositionRefs(name, parsed.Resources)
				if err != nil {
					t.Fatal(err)
				}
				rawRefs := make([]any, 0, len(refs))
				provider.files = map[string]string{name: doc}
				for _, ref := range refs {
					rawRefs = append(rawRefs, map[string]any{"path": ref.Path, "role": ref.Role, "primary": ref.Primary, "required": ref.Required})
					if !ref.Primary {
						provider.files[ref.Path] = "mesh"
					}
				}
				target.StorageRef = name
				target.ItemFingerprint = commonModels.GenerateItemFingerprint(9, name)
				target.item.Name = path.Base(name)
				target.item.FullName = name
				target.item.Fingerprint = target.ItemFingerprint
				target.item.Attributes["item"] = map[string]any{"layout": "multi", "data_type": "model_3d", "format": "gltf", "refs": rawRefs}
				target.item.Attributes["storage"] = map[string]any{"physical_path": name}
				target.item.Attributes["format_info"] = map[string]any{"gltf": map[string]any{"composition_complete": true}}
				target.Descriptor = dataitem.ItemDescriptor{Layout: format.LayoutMulti, DataType: datatype.Model3D, Format: "gltf"}
				target.locator = resourcetree.LocatorFromFullName(9, kind, target.item.ItemType, name, &target.item.ID).ToURI()
				svc.SetModelSourceReadChecker(storageModelCheck(func(context.Context, string, commonClient.ManagerModel3DExpectedSource) error {
					if fault == "denied" {
						return commonClient.ErrManagerPreviewReadDenied
					}
					return nil
				}))
				plan, err := svc.resolveStorageDownloadPlan(t.Context(), 9, kind, name, target.item, tenant)
				if err != nil || plan.Kind != models.DownloadKindBundle {
					t.Fatal("multi download must retain a logical bundle", plan, err)
				}
				if fault == "changed references" {
					provider.files[name] = strings.ReplaceAll(doc, "literal%2520.mesh", "unapproved.mesh")
				}
				if fault == "remote reference" {
					provider.files[name] = strings.ReplaceAll(doc, "buffers/literal%2520.mesh", "https://outside.invalid/a.mesh")
				}
				reader, err := svc.OpenStorageDownloadPlan(t.Context(), target, plan, tenant, "addp_at_current_user", true)
				if fault != "none" && fault != "embedded only" {
					if err == nil || reader != nil || provider.opens > 1 {
						t.Fatalf("unproved manifest reached dependency: %v opens=%d", err, provider.opens)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				raw, err := io.ReadAll(reader)
				closeErr := reader.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
				archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
				if err != nil || len(archive.File) != len(refs) {
					t.Fatal("bundle lost exact composition", err)
				}
				for _, file := range archive.File {
					stream, err := file.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(stream)
					closeErr := stream.Close()
					if err != nil || closeErr != nil || string(data) != provider.files[path.Join(path.Dir(name), file.Name)] {
						t.Fatal("bundle member path or bytes changed", file.Name, err, closeErr)
					}
				}
			})
		}
	}
}
