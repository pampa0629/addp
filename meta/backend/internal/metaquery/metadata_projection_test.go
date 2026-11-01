package metaquery

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/meta/internal/models"
)

func TestMetadataProjectionPreservesNestedStructureWithoutContentOrMutation(t *testing.T) {
	secret := "private-content-marker"
	leaf := datatype.FieldInfo{Name: "value", Type: datatype.FieldTypeInt, NativeType: "int4", Comment: secret, DefaultExpression: secret, GenerationExpression: secret}
	field := datatype.FieldInfo{Name: "nested", Fields: []datatype.FieldInfo{leaf}, Element: &leaf, Comment: secret}
	count := int64(91)
	item := models.MetaItemLite{ID: 3, TenantID: 7, EngineID: 9, NodeID: 2, Name: "visible", FullName: "public.visible", Fingerprint: "fp", RowCount: &count, SizeBytes: &count,
		Attributes: map[string]interface{}{
			"item":         map[string]interface{}{"data_type": "table", "format": "postgresql", "layout": "single", "sample": secret},
			"storage":      map[string]interface{}{"physical_path": secret},
			"type_info":    map[string]interface{}{"table": datatype.TableInfoPayload(&datatype.TableInfo{Name: "visible", Fields: []datatype.FieldInfo{field}, RowCount: &count, Native: map[string]interface{}{"sample": secret}})},
			"capabilities": map[string]interface{}{"statistics": map[string]interface{}{"sample": secret}, "spatial": map[string]interface{}{"srid": 4326, "extent": []float64{1, 2, 3, 4}}},
		}}
	before, _ := json.Marshal(item)
	safe := MetadataOnlyItem(item)
	encoded, err := json.Marshal(safe)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{secret, `"row_count"`, `"size_bytes"`, `"extent"`, `"storage"`, `"statistics"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("projection leaked %s: %s", forbidden, encoded)
		}
	}
	table := tableInfoFromMetaAttributes(safe.Attributes)
	if table == nil || len(table.Fields) != 1 || len(table.Fields[0].Fields) != 1 || table.Fields[0].Element == nil || table.Fields[0].Fields[0].NativeType != "int4" || table.Fields[0].Element.Name != "value" {
		t.Fatalf("nested schema lost: %s", encoded)
	}
	if safe.ID != item.ID || safe.Fingerprint != item.Fingerprint || safe.FullName != item.FullName || !strings.Contains(string(encoded), `"srid":4326`) {
		t.Fatalf("identity/CRS lost: %s", encoded)
	}
	after, _ := json.Marshal(item)
	if string(before) != string(after) {
		t.Fatal("projection mutated owner snapshot")
	}
	// Machine readers still project the stored facts, including statistics.
	raw, err := SpatialMetadataFromItem(models.MetaItem{Attributes: item.Attributes, RowCount: item.RowCount})
	if err != nil || raw.RowCount != count || len(raw.Extent) != 4 {
		t.Fatalf("machine facts changed: %+v %v", raw, err)
	}
}
