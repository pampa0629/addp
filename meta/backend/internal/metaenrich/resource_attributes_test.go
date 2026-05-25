package metaenrich

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
)

func TestEnrichResourceAttributesKeepsContainerSummaryWhenContentOpenFails(t *testing.T) {
	t.Parallel()

	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:    dataitem.LayoutSingle,
			DataType:  dataitem.DataTypeContainer,
			Format:    string(format.FormatZIP),
			EntryPath: "broken.zip",
		},
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	_, _, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{
		ContentReader: failingContentReader{},
		Item:          item,
		PhysicalPath:  "broken.zip",
		CatalogPathFor: func(path string) plugin.CatalogPath {
			return plugin.FileItemPath(1, path)
		},
	})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}

	container := commonJSON.Section(attrs, "type_info.container")
	if container["child_count"] != 0 || container["resource_count"] != 1 {
		t.Fatalf("type_info.container = %#v, want summary", container)
	}
}

func TestEnrichResourceAttributesKeepsKnownFieldsWithoutContentReader(t *testing.T) {
	t.Parallel()

	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:   dataitem.LayoutSingle,
			DataType: dataitem.DataTypeTable,
			Format:   string(format.FormatCSV),
		},
		Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}},
	}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	_, fields, err := EnrichResourceAttributes(context.Background(), attrs, ResourceAttributesInput{Item: item})
	if err != nil {
		t.Fatalf("EnrichResourceAttributes() error = %v", err)
	}
	if len(fields) != 1 {
		t.Fatalf("fields len = %d, want 1", len(fields))
	}
	table := commonJSON.Section(attrs, "type_info.table")
	tableFields := commonJSON.InterfaceSlice(table["fields"])
	if len(tableFields) != 1 {
		t.Fatalf("type_info.table.fields = %#v, want one field", tableFields)
	}
}

type failingContentReader struct{}

func (r failingContentReader) Type() string         { return "failing" }
func (r failingContentReader) DisplayName() string  { return "failing" }
func (r failingContentReader) EngineOrigin() string { return "general" }
func (r failingContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r failingContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (r failingContentReader) DefaultPort() int                                   { return 0 }
func (r failingContentReader) RequiredFields() []string                           { return nil }
func (r failingContentReader) SensitiveFields() []string                          { return nil }
func (r failingContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r failingContentReader) StoreSemantics() plugin.StoreSemantics { return plugin.StoreSemantics{} }
func (r failingContentReader) OpenContent(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("open failed")
}
