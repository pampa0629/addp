package metaenrich

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	avroformat "github.com/addp/common/format/plugins/avro"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metaquery"
	"github.com/addp/meta/internal/models"
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

func TestAvroNestedMetadataPreservesDeclaredFields(t *testing.T) {
	data, err := os.ReadFile("../../../../common/format/plugins/avro/testdata/synthetic-nested.avro")
	if err != nil {
		t.Fatal(err)
	}
	size := int64(len(data))
	item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Layout: format.LayoutSingle, DataType: datatype.Table, Format: "avro", PrimaryContentPath: "nested.avro", SizeBytes: &size}, PhysicalPath: "nested.avro"}
	enriched, ok, err := EnrichSingleTableFileItem(t.Context(), staticContentReader{content: string(data)}, nil, 1, item, "nested.avro", size, false, "", nil, func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) })
	if err != nil || !ok {
		t.Fatalf("nested metadata %v", err)
	}
	info := datatype.TableInfoFromPayload(commonJSON.Section(enriched.Attributes, "type_info.table"), "")
	if info == nil || *info.RowCount != 43 || len(info.Fields) != 5 || info.Fields[1].Type != datatype.FieldTypeJSON || len(info.Fields[1].Fields) != 9 || info.Fields[2].ElementType != datatype.FieldTypeJSON || !info.Fields[2].Element.Nullable {
		t.Fatalf("nested schema lost: %#v", info)
	}
	f := info.Fields[1].Fields
	if f[1].Precision != 38 || f[1].Scale != 6 || f[4].Temporal.Unit != "ms" || f[5].Temporal.Unit != "us" || f[6].Temporal.Unit != "ns" || f[6].Temporal.Timezone != "UTC" {
		t.Fatal("nested scalar metadata lost")
	}
	if info.Fields[3].Element.Element.Type != datatype.FieldTypeBigInt || info.Fields[4].Element.Precision != 38 {
		t.Fatal("array element metadata lost")
	}
	description, err := avroformat.NewPlugin().DescribeTable(t.Context(), bytes.NewReader(data), nil)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := json.Marshal(description.FormatInfo["schema"])
	stored, _ := json.Marshal(commonJSON.Section(enriched.Attributes, "format_info.avro.schema"))
	if !bytes.Equal(source, stored) {
		t.Fatal("stored source schema changed")
	}
	public := metaquery.MetadataOnlyItem(models.MetaItemLite{Attributes: enriched.Attributes})
	projected := datatype.TableInfoFromPayload(commonJSON.Section(public.Attributes, "type_info.table"), "")
	if projected == nil || !reflect.DeepEqual(projected.Fields, info.Fields) || projected.RowCount != nil || public.Attributes["format_info"] != nil {
		t.Fatal("user metadata must retain the declared tree without raw source facts")
	}
}
func TestAvroEnumMetadataPreservesDeclaredNamesAndCandidates(t *testing.T) {
	data, err := os.ReadFile("../../../../common/format/plugins/avro/testdata/synthetic-enum.avro")
	if err != nil {
		t.Fatal(err)
	}
	size := int64(len(data))
	item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Layout: format.LayoutSingle, DataType: datatype.Table, Format: "avro", PrimaryContentPath: "nested.avro", SizeBytes: &size}, PhysicalPath: "nested.avro"}
	enriched, ok, err := EnrichSingleTableFileItem(t.Context(), staticContentReader{content: string(data)}, nil, 1, item, "nested.avro", size, false, "", nil, func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) })
	if err != nil || !ok {
		t.Fatalf("nested metadata %v", err)
	}
	info := datatype.TableInfoFromPayload(commonJSON.Section(enriched.Attributes, "type_info.table"), "")
	if info == nil || *info.RowCount != 43 || len(info.Fields) != 6 {
		t.Fatalf("enum schema lost: %#v", info)
	}
	for _, f := range []datatype.FieldInfo{info.Fields[1], info.Fields[2], info.Fields[3].Fields[0], *info.Fields[4].Element} {
		if f.Type != datatype.FieldTypeString || f.Enum == nil || f.Enum.Name != "demo.State" || !reflect.DeepEqual(f.Enum.Symbols, []string{"z", "A", "a"}) {
			t.Fatalf("enum declaration lost: %#v", f)
		}
	}
	if info.Fields[5].Enum.Name != "other.State" {
		t.Fatal("enum names conflated")
	}
	description, err := avroformat.NewPlugin().DescribeTable(t.Context(), bytes.NewReader(data), nil)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := json.Marshal(description.FormatInfo["schema"])
	stored, _ := json.Marshal(commonJSON.Section(enriched.Attributes, "format_info.avro.schema"))
	if !bytes.Equal(source, stored) {
		t.Fatal("stored source schema changed")
	}
	public := metaquery.MetadataOnlyItem(models.MetaItemLite{Attributes: enriched.Attributes})
	projected := datatype.TableInfoFromPayload(commonJSON.Section(public.Attributes, "type_info.table"), "")
	if projected == nil || !reflect.DeepEqual(projected.Fields, info.Fields) || projected.RowCount != nil || public.Attributes["format_info"] != nil {
		t.Fatal("user metadata must retain the declared tree without raw source facts")
	}
}

func TestAvroMapDeepScanPreservesValuesWithoutObservedKeys(t *testing.T) {
	data, err := os.ReadFile("../../../../common/format/plugins/avro/testdata/synthetic-map.avro")
	if err != nil {
		t.Fatal(err)
	}
	size := int64(len(data))
	item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Layout: format.LayoutSingle, DataType: datatype.Table, Format: string(format.FormatAvro), PrimaryContentPath: "synthetic.avro", SizeBytes: &size}, PhysicalPath: "synthetic.avro"}
	enriched, ok, err := EnrichSingleTableFileItem(t.Context(), staticContentReader{content: string(data)}, nil, 1, item, "synthetic.avro", size, false, "", nil, func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) })
	if err != nil {
		t.Fatal(err)
	}
	if !ok || enriched.DataType != datatype.Table || enriched.Format != "avro" || commonJSON.Int64(enriched.Attributes, "type_info.table", "row_count") != 43 || len(enriched.Fields) != 6 {
		t.Fatalf("deep scan: %#v", enriched)
	}
	if commonJSON.String(enriched.Attributes, "format_info.avro", "codec") != "null" || len(commonJSON.Section(enriched.Attributes, "format_info.avro.schema")) == 0 {
		t.Fatalf("Avro source facts missing: %#v", enriched.Attributes)
	}
	if len(commonJSON.Section(enriched.Attributes, "capabilities.spatial")) != 0 || len(commonJSON.Section(enriched.Attributes, "access_index.table")) != 0 {
		t.Fatal("Avro acquired invented spatial/index facts")
	}
	if enriched.Fields[1].MapValue == nil || enriched.Fields[1].MapValue.Type != datatype.FieldTypeBigInt || len(enriched.Fields[1].Fields) != 0 || enriched.Fields[3].MapValue.Enum.Name != "demo.MapState" {
		t.Fatal("map value metadata lost")
	}
}

func TestAvroFixedMetadataRetainsExactDeclaration(t *testing.T) {
	data, err := os.ReadFile("../../../../common/format/plugins/avro/testdata/synthetic-fixed.avro")
	if err != nil {
		t.Fatal(err)
	}
	size := int64(len(data))
	item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Layout: format.LayoutSingle, DataType: datatype.Table, Format: "avro", PrimaryContentPath: "fixed.avro", SizeBytes: &size}, PhysicalPath: "fixed.avro"}
	enriched, ok, err := EnrichSingleTableFileItem(t.Context(), staticContentReader{content: string(data)}, nil, 1, item, "fixed.avro", size, false, "", nil, func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(1, path) })
	if err != nil || !ok {
		t.Fatalf("fixed metadata: %v", err)
	}
	info := datatype.TableInfoFromPayload(commonJSON.Section(enriched.Attributes, "type_info.table"), "")
	if info == nil || *info.RowCount != 43 || len(info.Fields) != 9 {
		t.Fatalf("fixed table: %#v", info)
	}
	for _, f := range []datatype.FieldInfo{info.Fields[1], info.Fields[2], *info.Fields[4].Element, *info.Fields[5].MapValue} {
		if f.Type != datatype.FieldTypeBytes || f.Fixed == nil || f.Fixed.Name != "demo.Token4" || f.Fixed.Size != 4 || f.Size != 0 {
			t.Fatalf("fixed declaration lost: %#v", f)
		}
	}
	public := metaquery.MetadataOnlyItem(models.MetaItemLite{Attributes: enriched.Attributes})
	projected := datatype.TableInfoFromPayload(commonJSON.Section(public.Attributes, "type_info.table"), "")
	if projected == nil || !reflect.DeepEqual(projected.Fields, info.Fields) || projected.RowCount != nil || public.Attributes["format_info"] != nil {
		t.Fatal("fixed metadata projection changed declarations or exposed content")
	}
	projected.Fields[1].Fixed.Name = "changed"
	if info.Fields[1].Fixed.Name != "demo.Token4" {
		t.Fatal("projection shares fixed declaration")
	}
}
