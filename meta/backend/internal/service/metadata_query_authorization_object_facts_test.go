package service

import (
	"encoding/json"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

func exerciseSingleObjectAuthorizationFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	for i, format := range []string{"csv", "json", "parquet", "text", "png"} {
		t.Run("native object "+format, func(t *testing.T) {
			engine := uint(982039 + i)
			root := models.MetaNode{TenantID: 7, EngineID: engine, NodeType: "service", FullName: ""}
			if err := db.Create(&root).Error; err != nil {
				t.Fatal(err)
			}
			bucket := models.MetaNode{TenantID: 7, EngineID: engine, NodeType: "bucket", Name: "addp-demo", FullName: "addp-demo", ParentNodeID: &root.ID}
			if err := db.Create(&bucket).Error; err != nil {
				t.Fatal(err)
			}
			prefix := models.MetaNode{TenantID: 7, EngineID: engine, NodeType: "prefix", Name: "tables", FullName: "addp-demo/tables", ParentNodeID: &bucket.ID}
			if err := db.Create(&prefix).Error; err != nil {
				t.Fatal(err)
			}
			name := "订单." + format
			full := prefix.FullName + "/" + name
			item := models.MetaItem{TenantID: 7, EngineID: engine, NodeID: prefix.ID, ItemType: "object", Name: name, FullName: full, Fingerprint: commonmodels.GenerateItemFingerprint(engine, full), ScannedDepth: "basic", Attributes: models.JSONMap{"item": map[string]any{"format": format, "layout": "single", "data_type": "document"}, "storage": map[string]any{"physical_path": full}}}
			if err := db.Create(&item).Error; err != nil {
				t.Fatal(err)
			}
			s := NewMetadataQueryService(db)
			read := func(content bool) *authorizationfacts.AuthorizationFacts {
				t.Helper()
				f, err := s.GetDataItemAuthorizationFacts(t.Context(), 7, item.Fingerprint)
				if err != nil || f.BasisState != authorizationfacts.AuthorizationBasisComplete {
					t.Fatal(f, err)
				}
				if content {
					if _, err := f.ProvePrimaryTarget(plugin.ObjectItemPath(engine, "addp-demo", "tables/"+name)); err != nil {
						t.Fatal(err)
					}
				} else if _, err := f.ApprovedBinding(); err == nil {
					t.Fatal("unproven object gained read boundary")
				}
				return f
			}
			initial := read(true)
			item.Attributes["item"].(map[string]any)["refs"] = []map[string]any{{"path": full, "role": "main", "required": true, "primary": true, "extension": "." + format}}
			if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
				t.Fatal(err)
			}
			if *read(true).BoundaryHash != *initial.BoundaryHash {
				t.Fatal("equivalent primary changed identity")
			}
			raw, _ := json.Marshal(item.Attributes)
			for _, change := range []func(models.JSONMap){
				func(a models.JSONMap) { a["item"].(map[string]any)["layout"] = "multi" },
				func(a models.JSONMap) { a["storage"].(map[string]any)["physical_path"] = "addp-demo/other.csv" },
				func(a models.JSONMap) {
					a["item"].(map[string]any)["refs"] = []map[string]any{{"path": full, "role": "main", "primary": true, "required": true}, {"path": full + ".aux", "role": "dependency", "required": true}}
				},
				func(a models.JSONMap) {
					a["item"].(map[string]any)["refs"] = []map[string]any{{"path": full, "role": "main", "primary": true, "required": true, "unknown": true}}
				},
				func(a models.JSONMap) { a["item"].(map[string]any)["refs"] = nil },
			} {
				var attrs models.JSONMap
				_ = json.Unmarshal(raw, &attrs)
				change(attrs)
				if err := db.Model(&item).Update("attributes", attrs).Error; err != nil {
					t.Fatal(err)
				}
				read(false)
			}
			if err := db.Model(&item).Update("attributes", item.Attributes).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := s.GetDataItemAuthorizationFacts(t.Context(), 8, item.Fingerprint); err == nil {
				t.Fatal("cross tenant object disclosed")
			}
			if err := db.Model(&prefix).Update("engine_id", engine+100).Error; err != nil {
				t.Fatal(err)
			}
			f, err := s.GetDataItemAuthorizationFacts(t.Context(), 7, item.Fingerprint)
			if err != nil || f.Boundary != nil {
				t.Fatal("cross-engine parent proved object", f, err)
			}
		})
	}
}
func TestSingleObjectAuthorizationFacts(t *testing.T) {
	exerciseSingleObjectAuthorizationFacts(t, metatest.OpenMetadataDB(t))
}
