package metaenrich

import (
	"os"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaitem"
)

func TestAvroDeepScanUsesSharedTableDescription(t *testing.T) {
	data, err := os.ReadFile("../../../../common/format/plugins/avro/testdata/apache-primitives.avro")
	if err != nil {
		t.Fatal(err)
	}
	size := int64(len(data))
	item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Layout: format.LayoutSingle, DataType: datatype.Table, Format: string(format.FormatAvro), PrimaryContentPath: "synthetic.avro", SizeBytes: &size}, PhysicalPath: "synthetic.avro"}
	enriched, ok, err := EnrichSingleTableFileItem(t.Context(), staticContentReader{content: string(data)}, nil, 1, item, "synthetic.avro", size, false, "", nil, func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) })
	if err != nil {
		t.Fatal(err)
	}
	if !ok || enriched.DataType != datatype.Table || enriched.Format != "avro" || commonJSON.Int64(enriched.Attributes, "type_info.table", "row_count") != 3 || len(enriched.Fields) != 3 {
		t.Fatalf("deep scan: %#v", enriched)
	}
	if commonJSON.String(enriched.Attributes, "format_info.avro", "codec") != "null" || len(commonJSON.Section(enriched.Attributes, "format_info.avro.schema")) == 0 {
		t.Fatalf("Avro source facts missing: %#v", enriched.Attributes)
	}
	if len(commonJSON.Section(enriched.Attributes, "capabilities.spatial")) != 0 || len(commonJSON.Section(enriched.Attributes, "access_index.table")) != 0 {
		t.Fatal("Avro acquired invented spatial/index facts")
	}
}

func TestAvroDeepScanRejectsMalformedSourceInsteadOfDescriptorFallback(t *testing.T) {
	item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Layout: format.LayoutSingle, DataType: datatype.Table, Format: "avro", PrimaryContentPath: "broken.avro"}, PhysicalPath: "broken.avro"}
	_, ok, err := EnrichSingleTableFileItem(t.Context(), staticContentReader{content: "not an Avro file"}, nil, 1, item, "broken.avro", 16, false, "", nil, func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) })
	if err == nil || ok || !format.IsDefinitiveParseError(err) {
		t.Fatalf("parse error silently reduced to metadata: ok=%v err=%v", ok, err)
	}
}
