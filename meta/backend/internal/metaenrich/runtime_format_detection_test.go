package metaenrich

import (
	"context"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/access"
	_ "github.com/addp/common/format/plugins/pgeo"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
)

type runtimeFormatDetectorStub struct {
	format format.FormatType
	err    error
}

func (s runtimeFormatDetectorStub) DetectFormat(context.Context, *commonModels.Engine, uint, string, string, string) (format.FormatType, error) {
	return s.format, s.err
}

func TestRefineRuntimeFormatPromotesAccessToPGeo(t *testing.T) {
	item := &metaitem.DetectedItem{ResolvedItem: metaitemResolvedAccessItem()}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	err := RefineRuntimeFormat(context.Background(), attrs, runtimeFormatDetectorStub{format: format.FormatPGeo}, &commonModels.Engine{}, 1, item, "arcgis/source.mdb")
	if err != nil {
		t.Fatal(err)
	}
	if item.Format != string(format.FormatPGeo) || item.DataType != datatype.Container {
		t.Fatalf("item = %#v, want pgeo container", item.ResolvedItem)
	}
	itemAttrs, _ := attrs["item"].(map[string]interface{})
	if itemAttrs["format"] != string(format.FormatPGeo) {
		t.Fatalf("attributes.item.format = %#v, want pgeo", itemAttrs["format"])
	}
}

func TestRefineRuntimeFormatKeepsConfirmedAccess(t *testing.T) {
	item := &metaitem.DetectedItem{ResolvedItem: metaitemResolvedAccessItem()}
	attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(item)))

	err := RefineRuntimeFormat(context.Background(), attrs, runtimeFormatDetectorStub{format: format.FormatAccess}, &commonModels.Engine{}, 1, item, "source.mdb")
	if err != nil {
		t.Fatal(err)
	}
	if item.Format != string(format.FormatAccess) {
		t.Fatalf("item format = %q, want access", item.Format)
	}
}

func metaitemResolvedAccessItem() dataitem.ResolvedItem {
	return dataitem.ResolvedItem{
		Layout: format.LayoutSingle, DataType: datatype.Container, Format: string(format.FormatAccess),
		PrimaryContentPath: "source.mdb", RefList: metaitem.ItemRefsFromPaths([]string{"source.mdb"}),
	}
}
