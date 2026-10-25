package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func seedFileAuthorizationFacts(t *testing.T, db *gorm.DB, engineID uint, format string) models.MetaItem {
	t.Helper()
	root := models.MetaNode{TenantID: 7, EngineID: engineID, NodeType: "root", Name: "FTP", FullName: ""}
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	dir := models.MetaNode{TenantID: 7, EngineID: engineID, ParentNodeID: &root.ID, NodeType: "dir", Name: "models", FullName: "models"}
	if err := db.Create(&dir).Error; err != nil {
		t.Fatal(err)
	}
	name := "panel." + format
	refs := []map[string]any{{"path": "models/" + name, "role": "model", "required": true, "primary": true}}
	if format == "3ds" || format == "dae" || format == "obj" || format == "gltf" {
		refs = append(refs, map[string]any{"path": "models/TEST.PNG", "role": "texture", "required": true})
	}
	if format == "obj" {
		refs = append(refs, map[string]any{"path": "models/model.mtl", "role": "material_library", "required": true})
	}
	item := models.MetaItem{TenantID: 7, EngineID: engineID, NodeID: dir.ID, ItemType: "file", Name: name, FullName: "models/" + name, ScannedDepth: "basic",
		Attributes: models.JSONMap{"item": map[string]any{"layout": "single", "data_type": "model_3d", "format": format, "refs": refs},
			"storage":     map[string]any{"physical_path": "models/" + name},
			"format_info": map[string]any{format: map[string]any{"texture_refs": []string{"TEST.PNG"}, "scan_complete": true}}}}
	if format == "obj" {
		item.Attributes["format_info"] = map[string]any{"obj": map[string]any{"composition_complete": true, "material_libraries": []map[string]any{{"reference": "model.mtl", "texture_refs": []string{"TEST.PNG"}}}}}
	}
	if format == "stl" || format == "glb" {
		item.Attributes["format_info"] = map[string]any{format: map[string]any{"composition_complete": true}}
	}
	if format == "gltf" {
		refs[0]["role"] = "manifest"
		refs[1]["role"] = "image"
		item.Attributes["item"].(map[string]any)["layout"] = "multi"
		item.Attributes["format_info"] = map[string]any{"gltf": map[string]any{"composition_complete": true}}
	}
	item.Fingerprint = commonModels.GenerateItemFingerprint(engineID, item.FullName)
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	// Reload decoded JSON to exercise the actual persisted representation.
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	return item
}

func exerciseFileAuthorizationFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	for i, fixture := range []struct{ kind, format string }{{"file", "ifc"}, {"file", "3ds"}, {"file", "dae"}, {"object", "ifc"}, {"object", "3ds"}, {"object", "dae"}, {"file", "obj"}, {"object", "obj"}, {"file", "stl"}, {"object", "stl"}, {"file", "glb"}, {"object", "glb"}, {"file", "gltf"}, {"object", "gltf"}} {
		format := fixture.format
		t.Run(fixture.kind+"/"+format, func(t *testing.T) {
			item := seedFileAuthorizationFacts(t, db, uint(350+i), format)
			if fixture.kind == "object" {
				if err := db.Model(&models.MetaNode{}).Where("engine_id = ? AND node_type = ?", item.EngineID, "root").Update("node_type", "service").Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Model(&models.MetaNode{}).Where("engine_id = ? AND node_type = ?", item.EngineID, "dir").Update("node_type", "bucket").Error; err != nil {
					t.Fatal(err)
				}
				item.ItemType = "object"
				if err := db.Model(&item).Update("item_type", "object").Error; err != nil {
					t.Fatal(err)
				}
			}
			s := NewMetadataQueryService(db)
			facts, err := s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
			if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete {
				t.Fatalf("file facts=%+v err=%v", facts, err)
			}
			raw, err := json.Marshal(facts)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			boundary := wire["boundary"].(map[string]any)
			if boundary["file_composition"] == nil || boundary["native_target"] != nil {
				t.Fatal("file boundary used native or missing composition", string(raw))
			}
			hash := *facts.BoundaryHash
			if err := db.Model(&item).Updates(map[string]any{"size_bytes": 9999, "scanned_depth": "deep", "scanned_at": time.Now()}).Error; err != nil {
				t.Fatal(err)
			}
			current, err := s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
			if err != nil || current.BoundaryHash == nil || *current.BoundaryHash != hash {
				t.Fatal("I/O or deep facts changed composition", err)
			}
			if strings.Contains(string(raw), "physical_path") || strings.Contains(string(raw), "texture_refs") {
				t.Fatal("private attributes escaped fact projection")
			}
			if format == "stl" || format == "glb" || format == "gltf" {
				native := item.Attributes["format_info"].(map[string]any)[format].(map[string]any)
				native["composition_complete"] = false
				native["scan_complete"] = true
				if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
					t.Fatal(err)
				}
				incomplete, err := s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
				if err != nil || incomplete.BasisState != authorizationfacts.AuthorizationBasisIncomplete || incomplete.Boundary != nil {
					t.Fatalf("deep promoted STL basic closure: %+v %v", incomplete, err)
				}
				native["composition_complete"] = true
				if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
					t.Fatal(err)
				}
			}
			if format == "ifc" {
				delete(item.Attributes["item"].(map[string]any), "refs")
			} else if format != "stl" && format != "glb" {
				refs := item.Attributes["item"].(map[string]any)["refs"].([]any)
				refs[0], refs[1] = refs[1], refs[0]
			}
			if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
				t.Fatal(err)
			}
			current, err = s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
			if err != nil || current.BoundaryHash == nil || *current.BoundaryHash != hash {
				t.Fatal("equivalent single composition changed boundary", err)
			}
			if format == "3ds" || format == "dae" {
				refs := item.Attributes["item"].(map[string]any)["refs"].([]any)
				refs[0].(map[string]any)["path"] = "models/OTHER.PNG"
				item.Attributes["format_info"].(map[string]any)[format].(map[string]any)["texture_refs"] = []string{"OTHER.PNG"}
				if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
					t.Fatal(err)
				}
				current, err = s.GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
				if err != nil || current.BoundaryHash == nil || *current.BoundaryHash == hash {
					t.Fatal("changed texture target retained approved boundary", err)
				}
			}
		})
	}
	changes := []struct {
		name   string
		state  authorizationfacts.AuthorizationBasisState
		mutate func(models.JSONMap)
	}{
		{"unknown declaration", authorizationfacts.AuthorizationBasisIncomplete, func(a models.JSONMap) { delete(a, "format_info") }},
		{"unfinished declaration", authorizationfacts.AuthorizationBasisIncomplete, func(a models.JSONMap) {
			a["format_info"].(map[string]any)["3ds"].(map[string]any)["scan_complete"] = false
		}},
		{"string completeness flag", authorizationfacts.AuthorizationBasisIncomplete, func(a models.JSONMap) {
			a["format_info"].(map[string]any)["3ds"].(map[string]any)["scan_complete"] = "true"
		}},
		{"null declaration", authorizationfacts.AuthorizationBasisIncomplete, func(a models.JSONMap) {
			a["format_info"].(map[string]any)["3ds"].(map[string]any)["texture_refs"] = nil
		}},
		{"missing texture", authorizationfacts.AuthorizationBasisIncomplete, func(a models.JSONMap) {
			a["item"].(map[string]any)["refs"] = a["item"].(map[string]any)["refs"].([]any)[:1]
		}},
		{"case mismatch", authorizationfacts.AuthorizationBasisIncomplete, func(a models.JSONMap) {
			a["format_info"].(map[string]any)["3ds"].(map[string]any)["texture_refs"] = []string{"test.png"}
		}},
		{"outside declaration", authorizationfacts.AuthorizationBasisNeedsRefresh, func(a models.JSONMap) {
			a["format_info"].(map[string]any)["3ds"].(map[string]any)["texture_refs"] = []string{"../TEST.PNG"}
		}},
		{"extra undeclared ref", authorizationfacts.AuthorizationBasisNeedsRefresh, func(a models.JSONMap) {
			refs := a["item"].(map[string]any)["refs"].([]any)
			a["item"].(map[string]any)["refs"] = append(refs, map[string]any{"path": "models/secret.txt", "role": "texture_2", "required": true})
		}},
		{"duplicate ref", authorizationfacts.AuthorizationBasisNeedsRefresh, func(a models.JSONMap) {
			refs := a["item"].(map[string]any)["refs"].([]any)
			a["item"].(map[string]any)["refs"] = append(refs, refs[1])
		}},
		{"optional required ref", authorizationfacts.AuthorizationBasisNeedsRefresh, func(a models.JSONMap) {
			a["item"].(map[string]any)["refs"].([]any)[1].(map[string]any)["required"] = false
		}},
		{"string required flag", authorizationfacts.AuthorizationBasisNeedsRefresh, func(a models.JSONMap) {
			a["item"].(map[string]any)["refs"].([]any)[1].(map[string]any)["required"] = "true"
		}},
		{"wrong entrypoint", authorizationfacts.AuthorizationBasisNeedsRefresh, func(a models.JSONMap) { a["storage"].(map[string]any)["physical_path"] = "models" }},
		{"padded declaration cannot use an unpadded ref", authorizationfacts.AuthorizationBasisIncomplete, func(a models.JSONMap) {
			a["format_info"].(map[string]any)["3ds"].(map[string]any)["texture_refs"] = []string{" TEST.PNG"}
		}},
		{"unsupported format", authorizationfacts.AuthorizationBasisUnsupported, func(a models.JSONMap) { a["item"].(map[string]any)["format"] = "max" }},
	}
	for i, ref := range []string{"", "textures/TEST.PNG", "图%20.PNG", " TEST.PNG", " TEST.PNG ", " "} {
		t.Run("exact/"+ref, func(t *testing.T) {
			item := seedFileAuthorizationFacts(t, db, uint(550+i), "3ds")
			refs := item.Attributes["item"].(map[string]any)["refs"].([]any)
			declarations := []string{}
			if ref == "" {
				refs = refs[:1]
			} else {
				refs[1].(map[string]any)["path"] = "models/" + ref
				declarations = []string{ref}
			}
			item.Attributes["item"].(map[string]any)["refs"] = refs
			item.Attributes["format_info"].(map[string]any)["3ds"].(map[string]any)["texture_refs"] = declarations
			if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
				t.Fatal(err)
			}
			facts, err := NewMetadataQueryService(db).GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
			if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete {
				t.Fatalf("exact declaration rejected %+v %v", facts, err)
			}
		})
	}
	for i, tc := range changes {
		t.Run(tc.name, func(t *testing.T) {
			item := seedFileAuthorizationFacts(t, db, uint(400+i), "3ds")
			tc.mutate(item.Attributes)
			if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
				t.Fatal(err)
			}
			facts, err := NewMetadataQueryService(db).GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
			if err != nil || facts.BasisState != tc.state || facts.Boundary != nil || facts.BoundaryHash != nil {
				t.Fatalf("unsafe composition facts=%+v err=%v, want %s", facts, err, tc.state)
			}
		})
	}
	t.Run("parent ownership", func(t *testing.T) {
		item := seedFileAuthorizationFacts(t, db, 500, "3ds")
		if err := db.Model(&models.MetaNode{}).Where("id = ?", item.NodeID).Update("engine_id", 501).Error; err != nil {
			t.Fatal(err)
		}
		facts, err := NewMetadataQueryService(db).GetDataItemAuthorizationFacts(context.Background(), 7, item.Fingerprint)
		if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisIncomplete || facts.Boundary != nil {
			t.Fatalf("foreign parent accepted %+v %v", facts, err)
		}
	})
}

func TestFileDataItemAuthorizationFacts(t *testing.T) {
	exerciseFileAuthorizationFacts(t, metatest.OpenMetadataDB(t))
}

func TestDAEFileAuthorizationFactsDecodeURIsExactlyOnce(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	for i, tc := range []struct {
		raw, path string
		state     authorizationfacts.AuthorizationBasisState
	}{
		{"texture%20A.PNG", "models/texture A.PNG", authorizationfacts.AuthorizationBasisComplete},
		{"texture%2520A.PNG", "models/texture%20A.PNG", authorizationfacts.AuthorizationBasisComplete},
		{"texture%2520A.PNG", "models/texture A.PNG", authorizationfacts.AuthorizationBasisIncomplete},
		{"texture%2.PNG", "models/texture.PNG", authorizationfacts.AuthorizationBasisNeedsRefresh},
		{"%2e%2e/outside.PNG", "models/outside.PNG", authorizationfacts.AuthorizationBasisNeedsRefresh},
		{"https://host/texture.PNG", "models/texture.PNG", authorizationfacts.AuthorizationBasisNeedsRefresh},
		{"texture.BMP", "models/texture.BMP", authorizationfacts.AuthorizationBasisNeedsRefresh},
	} {
		item := seedFileAuthorizationFacts(t, db, uint(2900+i), "dae")
		refs := item.Attributes["item"].(map[string]any)["refs"].([]any)
		refs[1].(map[string]any)["path"] = tc.path
		item.Attributes["format_info"].(map[string]any)["dae"].(map[string]any)["texture_refs"] = []string{tc.raw}
		if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
			t.Fatal(err)
		}
		facts, err := NewMetadataQueryService(db).GetDataItemAuthorizationFacts(t.Context(), 7, item.Fingerprint)
		if err != nil || facts.BasisState != tc.state {
			t.Fatalf("%s: %+v %v", tc.raw, facts, err)
		}
	}
}
