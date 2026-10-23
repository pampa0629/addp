package metaitem

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"path"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format/plugins/obj"
)

type objStructureFixture struct {
	refMapContentReader
	opened     []string
	sequential int
}

func (r *objStructureFixture) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	r.sequential++
	return nil, fmt.Errorf("OBJ basic must use exact range access")
}

func (r *objStructureFixture) OpenRange(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	name := target.StringPath()
	name = strings.TrimPrefix(name, "bucket/")
	r.opened = append(r.opened, name)
	raw, exists := r.content[name]
	if !exists || opts.Offset < 0 || opts.Length < 0 || opts.Offset+opts.Length > int64(len(raw)) {
		return nil, io.ErrUnexpectedEOF
	}
	return io.NopCloser(bytes.NewReader(raw[opts.Offset : opts.Offset+opts.Length])), nil
}

func objResolveFixture(t *testing.T, object bool, change func(map[string][]byte)) (*DetectedItem, *objStructureFixture, *DetectionResult) {
	t.Helper()
	prefix := "models/"
	data := map[string][]byte{prefix + "model.obj": []byte("mtllib a material.mtl\nmtllib b.mtl\nv 0 0 0\n"),
		prefix + "a material.mtl": []byte("newmtl red\nmap_Kd red/same.png\n"), prefix + "b.mtl": []byte("newmtl blue\nmap_Kd blue/same.png\n"),
		prefix + "red/same.png": []byte("red"), prefix + "blue/same.png": []byte("blue")}
	if change != nil {
		change(data)
	}
	files := make([]StorageFileRef, 0, len(data))
	for name, raw := range data {
		files = append(files, StorageFileRef{Name: path.Base(name), Path: name, Size: int64(len(raw))})
	}
	reader := &objStructureFixture{refMapContentReader: refMapContentReader{content: data}}
	input := DirectoryResolveInput{EngineID: 9, DirPath: strings.TrimSuffix(prefix, "/"), Files: files, StructureReader: reader, Options: ResolveOptions{IncludeSingleResources: true}}
	if object {
		input.EngineCatalogPathFor = plugin.ObjectItemPathForBucket(9, "bucket")
	}
	result, err := (&commonDataItemResolver{}).ResolveItems(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range result.Items {
		if item.PrimaryContentPath == prefix+"model.obj" {
			return item, reader, result
		}
	}
	t.Fatal("OBJ identity missing")
	return nil, nil, nil
}

func TestOBJBasicCompositionReadsOnlyExactOBJAndDeclaredMTLs(t *testing.T) {
	for _, object := range []bool{false, true} {
		item, reader, _ := objResolveFixture(t, object, nil)
		native := item.Attributes["format_info"].(map[string]interface{})["obj"].(map[string]interface{})
		if native["composition_complete"] != true || len(item.RefList) != 5 || len(reader.opened) != 3 || reader.sequential != 0 {
			t.Fatalf("item=%+v reads=%v", item, reader.opened)
		}
		if _, exists := native["scan_complete"]; exists {
			t.Fatal("basic published deep geometry status")
		}
		for _, name := range reader.opened {
			if strings.HasSuffix(name, ".png") {
				t.Fatal("basic opened image content")
			}
		}
		for _, ref := range item.RefList {
			if !ref.Required {
				t.Fatal("OBJ composition contains optional dependency")
			}
		}
	}
}

func TestOBJBasicIncompleteClosureNeverKeepsPartialAuthority(t *testing.T) {
	for name, change := range map[string]func(map[string][]byte){
		"missing material":     func(data map[string][]byte) { delete(data, "models/b.mtl") },
		"nested material":      func(data map[string][]byte) { data["models/model.obj"] = []byte("mtllib nested/b.mtl\n") },
		"unsupported material": func(data map[string][]byte) { data["models/b.mtl"] = []byte("map_unknown secret.png\n") },
	} {
		t.Run(name, func(t *testing.T) {
			item, reader, _ := objResolveFixture(t, false, change)
			native := item.Attributes["format_info"].(map[string]interface{})["obj"].(map[string]interface{})
			if native["composition_complete"] != false || native["material_libraries"] != nil || len(item.RefList) != 1 || reader.sequential != 0 {
				t.Fatalf("partial authority: %+v", item)
			}
		})
	}
}

func TestOBJBasicMissingOrEmptyImageRejectsTheWholeComposition(t *testing.T) {
	for _, empty := range []bool{false, true} {
		item, reader, result := objResolveFixture(t, false, func(data map[string][]byte) {
			if empty {
				data["models/blue/same.png"] = nil
			} else {
				delete(data, "models/blue/same.png")
			}
		})
		native := item.Attributes["format_info"].(map[string]interface{})["obj"].(map[string]interface{})
		if native["composition_complete"] != false || native["material_libraries"] != nil || len(item.RefList) != 1 || result.Claims["models/blue/same.png"] || result.Claims["models/a material.mtl"] || len(reader.opened) != 3 {
			t.Fatal("incomplete OBJ kept partial authority")
		}
	}
}

func TestOBJMaterialAggregateBudgetIsCheckedBeforeOpeningTheNextMTL(t *testing.T) {
	data := map[string][]byte{"models/model.obj": []byte("mtllib a.mtl\nmtllib b.mtl\nv 0 0 0\n"), "models/a.mtl": []byte("newmtl a\n"), "models/b.mtl": []byte("newmtl b\n")}
	reader := &objStructureFixture{refMapContentReader: refMapContentReader{content: data}}
	files := map[string]StorageFileRef{}
	for name, raw := range data {
		files[name] = StorageFileRef{Name: path.Base(name), Path: name, Size: int64(len(raw))}
	}
	large := files["models/b.mtl"]
	large.Size = obj.MaxCompositionBytes
	files[large.Path] = large
	_, err := readOBJMaterialLibraries(t.Context(), DirectoryResolveInput{EngineID: 9, StructureReader: reader}, files["models/model.obj"], files)
	if err == nil || len(reader.opened) != 2 || reader.opened[1] != "models/a.mtl" {
		t.Fatalf("aggregate budget made unbounded reads: %v %v", err, reader.opened)
	}
	libraries := []obj.MaterialLibraryDeclaration{{Reference: "a.mtl", TextureRefs: []string{"image.png"}}}
	files["models/image.png"] = StorageFileRef{Path: "models/image.png", Name: "image.png", Size: math.MaxInt64}
	item := objResourceItem(files["models/model.obj"], libraries, files, true)
	native := item.Attributes["format_info"].(map[string]interface{})["obj"].(map[string]interface{})
	if native["composition_complete"] != false || len(item.RefList) != 1 || native["material_libraries"] != nil {
		t.Fatal("overflow kept partial composition")
	}
}

func TestOBJReferenceBudgetStopsBeforeReadingTheRemainingMTL(t *testing.T) {
	var material strings.Builder
	material.WriteString("newmtl a\n")
	for i := 0; i < 198; i++ {
		fmt.Fprintf(&material, "map_Kd t%d.png\n", i)
	}
	data := map[string][]byte{"models/model.obj": []byte("mtllib a.mtl\nmtllib b.mtl\n"), "models/a.mtl": []byte(material.String()), "models/b.mtl": []byte("newmtl b\n")}
	reader := &objStructureFixture{refMapContentReader: refMapContentReader{content: data}}
	files := map[string]StorageFileRef{}
	for name, raw := range data {
		files[name] = StorageFileRef{Name: path.Base(name), Path: name, Size: int64(len(raw))}
	}
	if _, err := readOBJMaterialLibraries(t.Context(), DirectoryResolveInput{EngineID: 9, StructureReader: reader}, files["models/model.obj"], files); err == nil || len(reader.opened) != 2 {
		t.Fatalf("reference limit read later manifests: %v %v", err, reader.opened)
	}
}
