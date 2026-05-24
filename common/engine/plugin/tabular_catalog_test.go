package plugin

import (
	"testing"
	"time"

	"github.com/addp/common/datatype"
)

func TestTableAttributesCarriesTableNativeFacts(t *testing.T) {
	updatedAt := time.Unix(200, 0)
	table := datatype.TableInfo{
		Name:      "orders",
		Comment:   "order facts",
		UpdatedAt: &updatedAt,
		Native: map[string]interface{}{
			"engine": "MergeTree",
		},
	}

	attrs := tableAttributes("analytics", table)
	table.Native["engine"] = "Log"

	if attrs["namespace"] != "analytics" || attrs["table"] != "orders" {
		t.Fatalf("table identity attrs = %#v", attrs)
	}
	if attrs["comment"] != "order facts" {
		t.Fatalf("comment attr = %#v, want order facts", attrs["comment"])
	}
	if attrs["updated_at"] != &updatedAt {
		t.Fatalf("updated_at attr = %#v, want original pointer", attrs["updated_at"])
	}
	native, ok := attrs["native"].(map[string]interface{})
	if !ok || native["engine"] != "MergeTree" {
		t.Fatalf("native attrs = %#v, want copied engine", attrs["native"])
	}
}
