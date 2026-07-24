package pmtiles

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestPluginDescribesPMTilesV3(t *testing.T) {
	header := validHeader()
	result, err := (&Plugin{}).DescribeMedia(context.Background(), bytes.NewReader(header), nil)
	if err != nil {
		t.Fatalf("DescribeMedia() error = %v", err)
	}
	if result.Media.MIMEType != "application/vnd.pmtiles" || result.Spatial == nil || result.Spatial.SRID == nil || *result.Spatial.SRID != 4326 {
		t.Fatalf("result = %#v", result)
	}
	if result.FormatInfo["spec_version"] != uint8(3) || result.FormatInfo["tile_type"] != "mvt" {
		t.Fatalf("format info = %#v", result.FormatInfo)
	}
}

func TestDescriptorUsesMediaSingleIdentity(t *testing.T) {
	descriptor := (&Plugin{}).Descriptor()
	if descriptor.Format != format.FormatPMTiles || descriptor.DataType != datatype.Media || !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("descriptor = %#v", descriptor)
	}
}

func validHeader() []byte {
	b := make([]byte, 127)
	copy(b, "PMTiles")
	b[7] = 3
	binary.LittleEndian.PutUint64(b[8:16], 127)
	binary.LittleEndian.PutUint64(b[16:24], 1)
	binary.LittleEndian.PutUint64(b[56:64], 128)
	binary.LittleEndian.PutUint64(b[64:72], 1)
	b[96], b[97], b[98], b[99] = 1, 2, 2, 1
	b[100], b[101] = 0, 12
	minLon, minLat := int32(-1800000000), int32(-850000000)
	binary.LittleEndian.PutUint32(b[102:106], uint32(minLon))
	binary.LittleEndian.PutUint32(b[106:110], uint32(minLat))
	binary.LittleEndian.PutUint32(b[110:114], uint32(int32(1800000000)))
	binary.LittleEndian.PutUint32(b[114:118], uint32(int32(850000000)))
	return b
}
