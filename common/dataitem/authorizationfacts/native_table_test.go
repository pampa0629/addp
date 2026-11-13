package authorizationfacts

import (
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestNativeTableBindingReusesActualMetaIdentity(t *testing.T) {
	path := plugin.TabularItemPath(12, "schema", "户外.schema", "people.table")
	item := models.MetaItem{ID: 7, NodeID: 3, TenantID: 2, EngineID: 12, ItemType: "table", Name: "people.table", FullName: "户外.schema.people.table",
		Fingerprint: models.GenerateItemFingerprint(12, "户外.schema.people.table"),
		Attributes:  map[string]interface{}{"item": map[string]interface{}{"layout": "single"}, "storage": map[string]interface{}{"schema_name": "户外.schema"}}}
	expected, err := bindingFacts(t, path).ApprovedBinding()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := NativeTableBindingFromMeta(item, path)
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("binding=%+v err=%v", actual, err)
	}
	path.Segments[2].Name = "mutated"
	if actual.NativeTarget.Segments[2].Name != item.Name {
		t.Fatal("binding aliases the ReadSet")
	}
	for _, change := range []func(*models.MetaItem){
		func(x *models.MetaItem) { x.ID = 0 }, func(x *models.MetaItem) { x.NodeID = 0 },
		func(x *models.MetaItem) { x.TenantID = 0 }, func(x *models.MetaItem) { x.EngineID++ },
		func(x *models.MetaItem) { x.ItemType = "collection" }, func(x *models.MetaItem) { x.Name = "other" },
		func(x *models.MetaItem) { x.Fingerprint = models.GenerateItemFingerprint(12, "other") },
		func(x *models.MetaItem) { x.Attributes = nil },
		func(x *models.MetaItem) {
			x.Attributes = map[string]interface{}{"item": map[string]interface{}{"layout": "single"}, "storage": map[string]interface{}{"schema_name": "other"}}
		},
		func(x *models.MetaItem) {
			x.Attributes = map[string]interface{}{"item": map[string]interface{}{"layout": "single"}, "storage": map[string]interface{}{"schema_name": "户外.schema"}, "type_info": map[string]interface{}{"table": map[string]interface{}{"kind": "view"}}}
		},
	} {
		copy := item
		change(&copy)
		if _, err := NativeTableBindingFromMeta(copy, expected.NativeTarget); err == nil {
			t.Fatal("invalid metadata became a table proof")
		}
	}
}
