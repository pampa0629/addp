package image

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestExtractGeoTIFFSpatial(t *testing.T) {
	t.Parallel()

	data := testGeoTIFF(t)
	spatial := extractGeoTIFFSpatial(data, 2, 2)
	if spatial == nil {
		t.Fatalf("spatial attrs missing")
	}
	if spatial.Extent == nil {
		t.Fatalf("spatial extent missing")
	}
	extent := []float64{spatial.Extent[0], spatial.Extent[1], spatial.Extent[2], spatial.Extent[3]}
	wantExtent := []float64{100, 180, 120, 200}
	for i := range wantExtent {
		if extent[i] != wantExtent[i] {
			t.Fatalf("extent = %#v, want %#v", extent, wantExtent)
		}
	}
	if len(spatial.GeometryColumns) != 1 || spatial.GeometryColumns[0].SRID == nil || *spatial.GeometryColumns[0].SRID != 4326 {
		t.Fatalf("srid = %#v, want 4326", spatial.GeometryColumns)
	}
	if spatial.HasSpatialIndex == nil || *spatial.HasSpatialIndex {
		t.Fatalf("has_spatial_index = %#v, want false", spatial.HasSpatialIndex)
	}
}

func testGeoTIFF(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	order := binary.LittleEndian

	_, _ = buf.Write([]byte{'I', 'I'})
	_ = binary.Write(&buf, order, uint16(42))
	_ = binary.Write(&buf, order, uint32(8))
	entryCount := uint16(3)
	dataStart := 8 + 2 + int(entryCount)*12 + 4
	scaleOffset := dataStart
	scaleBytes := doublesBytes([]float64{10, 10, 0})
	tiepointOffset := scaleOffset + len(scaleBytes)
	tiepointBytes := doublesBytes([]float64{0, 0, 0, 100, 200, 0})
	geoKeyOffset := tiepointOffset + len(tiepointBytes)
	geoKeyBytes := shortsBytes([]uint16{1, 1, 0, 1, geoKeyGeographicType, 0, 1, 4326})

	_ = binary.Write(&buf, order, entryCount)
	buf.Write(buildIFDEntry(order, tagModelPixelScale, tiffTypeDouble, 3, uint32(scaleOffset)))
	buf.Write(buildIFDEntry(order, tagModelTiepoint, tiffTypeDouble, 6, uint32(tiepointOffset)))
	buf.Write(buildIFDEntry(order, tagGeoKeyDirectory, tiffTypeShort, 8, uint32(geoKeyOffset)))
	_ = binary.Write(&buf, order, uint32(0))
	buf.Write(scaleBytes)
	buf.Write(tiepointBytes)
	buf.Write(geoKeyBytes)
	return buf.Bytes()
}

func buildIFDEntry(order binary.ByteOrder, tag, typ uint16, count uint32, value uint32) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, order, tag)
	_ = binary.Write(&buf, order, typ)
	_ = binary.Write(&buf, order, count)
	_ = binary.Write(&buf, order, value)
	return buf.Bytes()
}

func doublesBytes(values []float64) []byte {
	var buf bytes.Buffer
	for _, value := range values {
		_ = binary.Write(&buf, binary.LittleEndian, value)
	}
	return buf.Bytes()
}

func shortsBytes(values []uint16) []byte {
	var buf bytes.Buffer
	for _, value := range values {
		_ = binary.Write(&buf, binary.LittleEndian, value)
	}
	return buf.Bytes()
}
