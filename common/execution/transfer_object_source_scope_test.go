package execution

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resourcetree"
)

func TestTransferObjectScopeBindsExactParquetAndFrozenConfig(t *testing.T) {
	p := plugin.ObjectItemPath(4, "demo", "tables/orders.parquet")
	binding, err := authorizationfacts.NativeCandidateBinding(p)
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]any{
		"runtime": map[string]any{"boundary": "bounded"}, "load": map[string]any{"mode": "snapshot"},
		"source":        map[string]any{"locator": "addp://engine/4/path/demo/tables/orders.parquet?type=object&item_id=21", "data_type": "table", "representation": "encoded", "format": "parquet"},
		"target":        map[string]any{"parent_locator": "addp://engine/5/path/public?type=schema", "name": "orders", "data_type": "table", "representation": "native", "policy": map[string]any{"apply_mode": "replace"}},
		"object_source": TransferObjectSource{EngineVersion: "8", Binding: binding},
	}
	config["source"].(map[string]any)["options"] = map[string]any{}
	raw, _ := json.Marshal(config)
	scope, err := NewTransferObjectReadScope(raw)
	if err != nil {
		t.Fatalf("valid object scope: %v", err)
	}
	wire, _ := json.Marshal(scope)
	for _, invalid := range []string{strings.Replace(string(wire), `"config_digest":`, `"Config_Digest":`, 1), strings.Replace(string(wire), `"engine_version":`, `"Engine_Version":`, 1), strings.TrimSuffix(string(wire), "}") + `,"extra":true}`} {
		if json.Unmarshal([]byte(invalid), new(TransferObjectReadScope)) == nil {
			t.Fatal("scope wire accepted an alias or unknown member")
		}
	}
	cloned := scope.Clone()
	cloned.ObjectSource.Binding.NativeTarget.Segments[1].Name = "changed"
	if scope.ObjectSource.Binding.NativeTarget.Segments[1].Name != "demo" {
		t.Fatal("scope clone aliases source")
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"sibling", func(c map[string]any) {
			c["source"].(map[string]any)["locator"] = "addp://engine/4/path/demo/tables/other.parquet?type=object&item_id=21"
		}},
		{"bucket", func(c map[string]any) {
			c["source"].(map[string]any)["locator"] = "addp://engine/4/path/other/tables/orders.parquet?type=object&item_id=21"
		}},
		{"missing item", func(c map[string]any) {
			c["source"].(map[string]any)["locator"] = "addp://engine/4/path/demo/tables/orders.parquet?type=object"
		}},
		{"bucket scope", func(c map[string]any) {
			c["source"].(map[string]any)["locator"] = "addp://engine/4/path/demo?type=bucket&item_id=21"
		}},
		{"query", func(c map[string]any) {
			c["source"].(map[string]any)["query"] = map[string]any{"statement": "select *"}
		}},
		{"csv", func(c map[string]any) { c["source"].(map[string]any)["format"] = "csv" }},
		{"continuous", func(c map[string]any) { c["runtime"].(map[string]any)["boundary"] = "continuous" }},
		{"incremental", func(c map[string]any) { c["load"].(map[string]any)["mode"] = "incremental" }},
		{"same engine", func(c map[string]any) {
			c["target"].(map[string]any)["parent_locator"] = "addp://engine/4/path/public?type=schema"
		}},
		{"unknown source identity", func(c map[string]any) { c["object_source"].(map[string]any)["extra"] = true }},
		{"bad binding", func(c map[string]any) {
			c["object_source"].(map[string]any)["binding"].(map[string]any)["boundary_hash"] = strings.Repeat("a", 64)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var c map[string]any
			_ = json.Unmarshal(raw, &c)
			tc.mutate(c)
			value, _ := json.Marshal(c)
			if _, err := NewTransferObjectReadScope(value); err == nil {
				t.Fatal("accepted invalid scope")
			}
		})
	}
	var changed map[string]any
	_ = json.Unmarshal(raw, &changed)
	changed["target"].(map[string]any)["name"] = "other_orders"
	value, _ := json.Marshal(changed)
	other, err := NewTransferObjectReadScope(value)
	if err != nil || other.ConfigDigest == scope.ConfigDigest {
		t.Fatal("target change did not change frozen digest", err)
	}
	if _, err := NewTransferObjectReadScope([]byte(`{"runtime":{},"runtime":{}}`)); err == nil {
		t.Fatal("accepted duplicate JSON members")
	}
	chinese := plugin.ObjectItemPath(4, "demo", "tables/订单.parquet")
	chineseBinding, err := authorizationfacts.NativeCandidateBinding(chinese)
	if err != nil {
		t.Fatal(err)
	}
	itemID := uint(21)
	config["source"].(map[string]any)["locator"] = (&resourcetree.ResourceLocator{EngineID: 4, Type: resourcetree.TypeObject, Path: []string{"demo", "tables", "订单.parquet"}, ItemID: &itemID}).ToURI()
	config["object_source"] = TransferObjectSource{EngineVersion: "8", Binding: chineseBinding}
	chineseRaw, _ := json.Marshal(config)
	if _, err := NewTransferObjectReadScope(chineseRaw); err != nil {
		t.Fatal("canonical Chinese source rejected", err)
	}
}
