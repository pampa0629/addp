package service

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// Persist and reload normal JSON attributes. A synthetic complete observation
// exercises the projection only; it is not a source scan or an access grant.
func TestGeoPackageAuthorizationFactsConsumeCommittedObservation(t *testing.T) {
	exerciseGeoPackageAuthorizationFacts(t, metatest.OpenMetadataDB(t))
}

func exerciseGeoPackageAuthorizationFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	item := seedFileAuthorizationFacts(t, db, 855, "geopackage")
	item.Name, item.FullName = "sources.GPKG", "models/sources.GPKG"
	item.Fingerprint = commonModels.GenerateItemFingerprint(item.EngineID, item.FullName)
	item.Attributes = models.JSONMap{
		"item":        map[string]any{"layout": "single", "data_type": "container", "format": "geopackage", "refs": []map[string]any{{"path": item.FullName, "role": "container", "required": true, "primary": true}}},
		"storage":     map[string]any{"physical_path": item.FullName},
		"format_info": map[string]any{"geopackage": map[string]any{"composition_complete": true}},
	}
	if err := db.Save(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	baseline := item.Attributes
	svc := NewMetadataQueryService(db)
	observe := func(attrs models.JSONMap) *authorizationfacts.AuthorizationFacts {
		t.Helper()
		if err := db.Model(&item).Update("attributes", attrs).Error; err != nil {
			t.Fatal(err)
		}
		f, err := svc.GetDataItemAuthorizationFacts(t.Context(), item.TenantID, item.Fingerprint)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	complete := observe(baseline)
	if complete.Boundary == nil || complete.Boundary.FileComposition == nil || complete.Boundary.FileComposition.Format != "geopackage" {
		t.Fatalf("committed single-file composition was not projected: %#v", complete)
	}
	binding, err := complete.ApprovedBinding()
	if err != nil {
		t.Fatal(err)
	}
	if target, err := binding.PrimaryTarget(); err != nil || !reflect.DeepEqual(target, plugin.FileItemPath(item.EngineID, item.FullName)) {
		t.Fatal("DataItem identity changed", target, err)
	}
	// Internal layers remain facts within the same DataItem, not new bindings.
	withChildren, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	var childrenAttrs models.JSONMap
	if err := json.Unmarshal(withChildren, &childrenAttrs); err != nil {
		t.Fatal(err)
	}
	childrenAttrs["type_info"] = map[string]any{"container": map[string]any{"children": []map[string]any{{"name": "roads"}, {"name": "parcels"}}, "child_count": 2}}
	children := observe(childrenAttrs)
	if !reflect.DeepEqual(complete.Boundary, children.Boundary) || children.BoundaryHash == nil || *complete.BoundaryHash != *children.BoundaryHash {
		t.Fatal("internal children created or changed an authorization target")
	}
	for _, variant := range []string{"missing observation", "false observation", "string observation", "no refs", "extra ref", "wrong role", "optional main", "no primary", "different primary", "wrong layout", "wrong data type", "wrong physical path"} {
		t.Run(variant, func(t *testing.T) {
			raw, err := json.Marshal(baseline)
			if err != nil {
				t.Fatal(err)
			}
			var attrs models.JSONMap
			if err := json.Unmarshal(raw, &attrs); err != nil {
				t.Fatal(err)
			}
			itemFacts := attrs["item"].(map[string]interface{})
			switch variant {
			case "missing observation":
				delete(attrs, "format_info")
			case "false observation":
				attrs["format_info"] = map[string]any{"geopackage": map[string]any{"composition_complete": false}}
			case "string observation":
				attrs["format_info"] = map[string]any{"geopackage": map[string]any{"composition_complete": "true"}}
			case "no refs":
				delete(itemFacts, "refs")
			case "extra ref":
				itemFacts["refs"] = []map[string]any{{"path": item.FullName, "role": "container", "required": true, "primary": true}, {"path": item.FullName + "-wal", "role": "journal", "required": true}}
			case "wrong role":
				itemFacts["refs"] = []map[string]any{{"path": item.FullName, "role": "model", "required": true, "primary": true}}
			case "optional main":
				itemFacts["refs"] = []map[string]any{{"path": item.FullName, "role": "container", "required": false, "primary": true}}
			case "no primary":
				itemFacts["refs"] = []map[string]any{{"path": item.FullName, "role": "container", "required": true, "primary": false}}
			case "different primary":
				itemFacts["refs"] = []map[string]any{{"path": "models/other.gpkg", "role": "container", "required": true, "primary": true}}
			case "wrong layout":
				itemFacts["layout"] = "multi"
			case "wrong data type":
				itemFacts["data_type"] = "model_3d"
			case "wrong physical path":
				attrs["storage"] = map[string]any{"physical_path": "models/other.gpkg"}
			}
			f := observe(attrs)
			if _, err := f.ApprovedBinding(); err == nil || (f.Boundary != nil && f.Boundary.FileComposition != nil) {
				t.Fatalf("%s promoted metadata to content: %#v", variant, f)
			}
		})
	}
	if restored := observe(baseline); !reflect.DeepEqual(complete.Boundary, restored.Boundary) || *complete.BoundaryHash != *restored.BoundaryHash {
		t.Fatal("restoring the same composition changed DataItem binding")
	}
}
