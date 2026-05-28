package metaattr

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
)

func TestContainerInfoAttributesKeepsOnlyCanonicalChildFormat(t *testing.T) {
	t.Parallel()

	attrs := ContainerInfoAttributes(&datatype.ContainerInfo{
		ChildCount:    4,
		ResourceCount: 1,
		Children: []datatype.ContainerChildInfo{
			{
				Name:      "config/docker-compose.yml",
				ChildKind: "file",
				DataType:  datatype.DataTypeUnknown,
				Native: map[string]interface{}{
					"format": "yml",
					"vendor": "kept",
				},
			},
			{
				Name:      "data/table.data",
				ChildKind: "file",
				DataType:  datatype.DataTypeTable,
				Native: map[string]interface{}{
					"format": "csv",
				},
			},
			{
				Name:      "docs/readme",
				ChildKind: "file",
				DataType:  datatype.DataTypeDocument,
				Format:    string(format.FormatText),
			},
			{
				Name:      "raw/blob",
				ChildKind: "file",
				DataType:  datatype.DataTypeUnknown,
				Format:    "unknown",
			},
		},
	})

	children := attrs["children"].([]map[string]interface{})
	if children[0]["format"] != nil {
		t.Fatalf("unknown native format should not be written: %#v", children[0])
	}
	native := children[0]["native"].(map[string]interface{})
	if native["format"] != nil || native["vendor"] != "kept" {
		t.Fatalf("native cleanup = %#v, want vendor only", native)
	}
	if children[1]["format"] != string(format.FormatCSV) {
		t.Fatalf("native canonical format = %#v, want csv", children[1]["format"])
	}
	if children[2]["format"] != string(format.FormatText) {
		t.Fatalf("child format = %#v, want text", children[2]["format"])
	}
	if children[3]["format"] != nil {
		t.Fatalf("unknown child format should not be written: %#v", children[3])
	}
}
