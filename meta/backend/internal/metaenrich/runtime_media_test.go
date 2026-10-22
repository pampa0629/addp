package metaenrich

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

type mediaInspectorStub struct {
	result *format.MediaDescribeResult
	err    error
}

func (s mediaInspectorStub) InspectMedia(context.Context, *commonModels.Engine, uint, string, string) (*format.MediaDescribeResult, error) {
	return s.result, s.err
}

func TestIMGDeepScanReplacesMediaSpatialAndInvalidatesStaleFacts(t *testing.T) {
	attrs := models.JSONMap{}
	item := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{DataType: datatype.Media, Format: "img", Layout: format.LayoutSingle}}
	srid := 32650
	extent := datatype.BoundingBox{500000, 3000000, 500320, 3000160}
	inspector := mediaInspectorStub{result: &format.MediaDescribeResult{
		Media:      &datatype.MediaInfo{Kind: datatype.MediaKindImage, Width: 32, Height: 16, Encoding: "HFA"},
		Spatial:    &datatype.SpatialInfo{SRID: &srid, Extent: &extent},
		FormatInfo: map[string]interface{}{"scan_complete": true, "band_count": 3},
	}}
	handled, err := EnrichRuntimeMediaItem(context.Background(), attrs, inspector, nil, 7, item, "a.img")
	if !handled || err != nil || commonJSON.InterfaceInt64(commonJSON.Section(attrs, "type_info.media")["width"]) != 32 || commonJSON.Section(attrs, "format_info.img")["scan_complete"] != true {
		t.Fatalf("%v %v %+v", handled, err, attrs)
	}
	_, err = EnrichRuntimeMediaItem(context.Background(), attrs, mediaInspectorStub{err: errors.New("changed source requires spill")}, nil, 7, item, "a.img")
	if err == nil || len(commonJSON.Section(attrs, "type_info.media")) != 0 || len(commonJSON.Section(attrs, "capabilities.spatial")) != 0 || len(commonJSON.Section(attrs, "format_info.img")) != 0 {
		t.Fatalf("stale IMG facts survived: %+v %v", attrs, err)
	}
}
