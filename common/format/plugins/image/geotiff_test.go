package image

import (
	"bytes"
	"context"
	"encoding/binary"
	stdimage "image"
	"image/color"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"golang.org/x/image/tiff"
)

func TestExtractGeoTIFFSpatial(t *testing.T) {
	t.Parallel()

	data := newTIFFMetadata(testGeoTIFF(t), nil)
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
	if spatial.SRID == nil || *spatial.SRID != 4326 {
		t.Fatalf("srid = %#v, want 4326", spatial.SRID)
	}
	if len(spatial.GeometryColumns) != 0 {
		t.Fatalf("non-table spatial should not invent geometry columns: %#v", spatial.GeometryColumns)
	}
	if spatial.HasSpatialIndex == nil || *spatial.HasSpatialIndex {
		t.Fatalf("has_spatial_index = %#v, want false", spatial.HasSpatialIndex)
	}
}

func TestTIFFDescribeRefsUsesTextFactsForSidecars(t *testing.T) {
	t.Parallel()

	descriptors := TIFFDescribeRefs([]format.RelatedRef{
		format.NewRelatedRef(contentio.NewRef("srtm_40_01.tif", contentio.RoleMain), true, true),
		format.NewRelatedRef(contentio.NewRef("srtm_40_01.tfw", "world_file"), false, false),
		format.NewRelatedRef(contentio.NewRef("srtm_40_01.prj", "crs"), false, false),
		format.NewRelatedRef(contentio.NewRef("srtm_40_01.hdr", "header"), false, false),
		format.NewRelatedRef(contentio.NewRef("srtm_40_01.tif.aux.xml", "auxiliary_metadata"), false, false),
		format.NewRelatedRef(contentio.NewRef("srtm_40_01.ovr", "overview"), false, false),
	})

	byRole := map[string]format.RefDescriptor{}
	for _, descriptor := range descriptors {
		byRole[descriptor.Role] = descriptor
	}
	if got := byRole["main"].Format; got != format.FormatUnknown {
		t.Fatalf("main ref format = %s, want unknown binary ref file format", got)
	}
	for _, role := range []string{"world_file", "crs", "header", "auxiliary_metadata"} {
		descriptor := byRole[role]
		if descriptor.DataType != datatype.Document || descriptor.Format != format.FormatText {
			t.Fatalf("%s descriptor = %#v, want text document", role, descriptor)
		}
	}
	if got := byRole["overview"].Format; got != format.FormatUnknown {
		t.Fatalf("overview ref format = %s, want unknown binary ref file format", got)
	}
}

func TestExtractGeoTIFFSpatialCustomCRSDefinitionSource(t *testing.T) {
	t.Parallel()

	crsText := "LOCAL_CUSTOM_CRS|\x00"
	data := newTIFFMetadata(testTIFFWithTags(t, []tiffTestTag{
		{tag: tagModelPixelScale, typ: tiffTypeDouble, data: doublesBytes([]float64{10, 10, 0})},
		{tag: tagModelTiepoint, typ: tiffTypeDouble, data: doublesBytes([]float64{0, 0, 0, 100, 200, 0})},
		{tag: tagGeoKeyDirectory, typ: tiffTypeShort, data: shortsBytes([]uint16{1, 1, 0, 1, geoKeyGTCitation, tagGeoAsciiParams, uint16(len(crsText)), 0})},
		{tag: tagGeoAsciiParams, typ: tiffTypeASCII, data: []byte(crsText)},
	}, 0), nil)

	spatial := extractGeoTIFFSpatial(data, 2, 2)
	if spatial == nil || spatial.CRSRef == "" || len(spatial.CRSDefinitions) != 1 {
		t.Fatalf("spatial = %#v, want custom CRS definition", spatial)
	}
	definition := spatial.CRSDefinitions[0]
	if definition.Source != datatype.CRSDefinitionSourceGeoTIFFTags {
		t.Fatalf("definition source = %q, want geotiff_tags", definition.Source)
	}
	if definition.Definition != "LOCAL_CUSTOM_CRS" {
		t.Fatalf("definition = %q, want LOCAL_CUSTOM_CRS", definition.Definition)
	}
}

func TestExtractTIFFFormatInfoProfiles(t *testing.T) {
	t.Parallel()

	plain := newTIFFMetadata(testTIFFWithTags(t, nil, 0), nil)
	plainInfo := extractTIFFFormatInfo(plain, nil)
	if plainInfo["profile"] != "plain_tiff" {
		t.Fatalf("plain profile = %#v, want plain_tiff; info=%#v", plainInfo["profile"], plainInfo)
	}
	if plainInfo["is_tiled"] != false || plainInfo["has_overviews"] != false || plainInfo["big_tiff"] != false {
		t.Fatalf("plain tiff info = %#v", plainInfo)
	}

	geo := newTIFFMetadata(testGeoTIFF(t), nil)
	geoSpatial := extractGeoTIFFSpatial(geo, 2, 2)
	geoInfo := extractTIFFFormatInfo(geo, geoSpatial)
	if geoInfo["profile"] != "geotiff" {
		t.Fatalf("geo profile = %#v, want geotiff; info=%#v", geoInfo["profile"], geoInfo)
	}

	cogCandidate := newTIFFMetadata(testCOGCandidateTIFF(t), nil)
	cogSpatial := extractGeoTIFFSpatial(cogCandidate, 2, 2)
	cogInfo := extractTIFFFormatInfo(cogCandidate, cogSpatial)
	if cogInfo["profile"] != "cog" || cogInfo["cog_check_level"] != "heuristic" {
		t.Fatalf("cog info = %#v, want heuristic cog", cogInfo)
	}
	if cogInfo["is_cloud_optimized"] != true || cogInfo["is_tiled"] != true || cogInfo["has_overviews"] != true {
		t.Fatalf("cog structure flags = %#v", cogInfo)
	}
}

func TestExtractTIFFFormatInfoReadsGDALRenderStats(t *testing.T) {
	t.Parallel()

	gdalMetadata := `<GDALMetadata>` +
		`<Item name="STATISTICS_MINIMUM" sample="0">-49</Item>` +
		`<Item name="STATISTICS_MAXIMUM" sample="0">406</Item>` +
		`</GDALMetadata>` + "\x00"
	data := newTIFFMetadata(testTIFFWithTags(t, []tiffTestTag{
		{tag: tagGDALNoData, typ: tiffTypeASCII, data: []byte("-32768\x00")},
		{tag: tagGDALMetadata, typ: tiffTypeASCII, data: []byte(gdalMetadata)},
	}, 0), nil)

	info := extractTIFFFormatInfo(data, nil)
	if info["nodata"] != float64(-32768) {
		t.Fatalf("nodata = %#v, want -32768", info["nodata"])
	}
	if info["sample_min"] != float64(-49) || info["sample_max"] != float64(406) {
		t.Fatalf("sample range = %#v/%#v, want -49/406", info["sample_min"], info["sample_max"])
	}
	if info["display_min"] != float64(-49) || info["display_max"] != float64(406) {
		t.Fatalf("display range = %#v/%#v, want -49/406", info["display_min"], info["display_max"])
	}
	if info["display_range_method"] != "metadata_statistics" {
		t.Fatalf("display_range_method = %#v, want metadata_statistics", info["display_range_method"])
	}
}

func TestExtractTIFFFormatInfoReadsSampleValueRenderStats(t *testing.T) {
	t.Parallel()

	data := newTIFFMetadata(testTIFFWithTags(t, []tiffTestTag{
		{tag: tagSMinSampleValue, typ: tiffTypeDouble, data: doublesBytes([]float64{1.5})},
		{tag: tagSMaxSampleValue, typ: tiffTypeDouble, data: doublesBytes([]float64{9.5})},
	}, 0), nil)

	info := extractTIFFFormatInfo(data, nil)
	if info["sample_min"] != float64(1.5) || info["sample_max"] != float64(9.5) {
		t.Fatalf("sample range = %#v/%#v, want 1.5/9.5", info["sample_min"], info["sample_max"])
	}
	if info["display_min"] != float64(1.5) || info["display_max"] != float64(9.5) {
		t.Fatalf("display range = %#v/%#v, want 1.5/9.5", info["display_min"], info["display_max"])
	}
	if info["display_range_method"] != "sample_value_tags" {
		t.Fatalf("display_range_method = %#v, want sample_value_tags", info["display_range_method"])
	}
}

func TestExtractTIFFFormatInfoDoesNotTrustBareSubIFDTag(t *testing.T) {
	t.Parallel()

	cogLikeButUnresolved := newTIFFMetadata(testTIFFWithTags(t, []tiffTestTag{
		{tag: tagModelPixelScale, typ: tiffTypeDouble, data: doublesBytes([]float64{10, 10, 0})},
		{tag: tagModelTiepoint, typ: tiffTypeDouble, data: doublesBytes([]float64{0, 0, 0, 100, 200, 0})},
		{tag: tagGeoKeyDirectory, typ: tiffTypeShort, data: shortsBytes([]uint16{1, 1, 0, 1, geoKeyGeographicType, 0, 1, 4326})},
		{tag: tagTileWidth, typ: tiffTypeLong, value: 256},
		{tag: tagTileLength, typ: tiffTypeLong, value: 256},
		{tag: tagTileOffsets, typ: tiffTypeLong, value: 1024},
		{tag: tagTileByteCounts, typ: tiffTypeLong, value: 4096},
		{tag: tagSubIFDs, typ: tiffTypeLong, value: 2048},
	}, 0), nil)
	spatial := extractGeoTIFFSpatial(cogLikeButUnresolved, 2, 2)
	info := extractTIFFFormatInfo(cogLikeButUnresolved, spatial)
	if info["profile"] == "cog" || info["has_overviews"] != false {
		t.Fatalf("unresolved SubIFD tag should not be COG: %#v", info)
	}
}

func TestExtractTIFFFormatInfoDoesNotTreatPlainNextIFDAsOverview(t *testing.T) {
	t.Parallel()

	nextIFDOffset := uint32(8 + 2 + 7*12 + 4)
	data := newTIFFMetadata(testTIFFWithTags(t, []tiffTestTag{
		{tag: tagModelPixelScale, typ: tiffTypeDouble, data: doublesBytes([]float64{10, 10, 0})},
		{tag: tagModelTiepoint, typ: tiffTypeDouble, data: doublesBytes([]float64{0, 0, 0, 100, 200, 0})},
		{tag: tagGeoKeyDirectory, typ: tiffTypeShort, data: shortsBytes([]uint16{1, 1, 0, 1, geoKeyGeographicType, 0, 1, 4326})},
		{tag: tagTileWidth, typ: tiffTypeLong, value: 256},
		{tag: tagTileLength, typ: tiffTypeLong, value: 256},
		{tag: tagTileOffsets, typ: tiffTypeLong, value: 1024},
		{tag: tagTileByteCounts, typ: tiffTypeLong, value: 4096},
	}, nextIFDOffset), nil)
	spatial := extractGeoTIFFSpatial(data, 2, 2)
	info := extractTIFFFormatInfo(data, spatial)
	if info["profile"] == "cog" || info["has_overviews"] != false {
		t.Fatalf("next IFD without reduced-resolution flag should not be COG: %#v", info)
	}
}

func TestExtractTIFFFormatInfoBigTIFF(t *testing.T) {
	t.Parallel()

	data := newTIFFMetadata([]byte{'I', 'I', 43, 0, 8, 0, 0, 0}, nil)
	info := extractTIFFFormatInfo(data, nil)
	if info["profile"] != "unknown" || info["big_tiff"] != true {
		t.Fatalf("big tiff info = %#v", info)
	}
	for _, key := range []string{"is_cloud_optimized", "is_tiled", "has_overviews"} {
		if _, ok := info[key]; ok {
			t.Fatalf("big tiff info contains %s=%#v, want omitted when unknown; info=%#v", key, info[key], info)
		}
	}
}

func TestDescribeMediaReturnsTIFFFormatInfo(t *testing.T) {
	t.Parallel()

	provider := newPlugin(format.FormatTIFF)
	info, err := provider.DescribeMedia(context.Background(), bytes.NewReader(testEncodedTIFF(t)), nil)
	if err != nil {
		t.Fatalf("DescribeMedia() error = %v", err)
	}
	if info.Media == nil || info.Media.Encoding != "tiff" {
		t.Fatalf("media info = %#v, want tiff encoding", info.Media)
	}
	if info.FormatInfo["profile"] != "plain_tiff" {
		t.Fatalf("format info = %#v, want plain_tiff profile", info.FormatInfo)
	}
}

func TestDescribeMediaReadsBoundedTIFFMetadata(t *testing.T) {
	t.Parallel()

	content := append([]byte(nil), testEncodedTIFF(t)...)
	content = append(content, bytes.Repeat([]byte{0}, tiffMetadataReadLimit*2)...)
	reader := &countingReader{reader: bytes.NewReader(content)}
	provider := newPlugin(format.FormatTIFF)

	info, err := provider.DescribeMedia(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("DescribeMedia() error = %v", err)
	}
	if info.Media == nil || info.Media.Width != 2 || info.Media.Height != 3 {
		t.Fatalf("media info = %#v, want bounded TIFF dimensions", info.Media)
	}
	if reader.bytesRead > tiffMetadataReadLimit*2 {
		t.Fatalf("bytes read = %d, want at most two metadata windows %d", reader.bytesRead, tiffMetadataReadLimit*2)
	}
}

func TestDescribeMediaReadsTailTIFFMetadataWhenSeekable(t *testing.T) {
	t.Parallel()

	content := testGeoTIFFWithIFDOffset(t, tiffMetadataReadLimit+4096)
	reader := &countingReadSeeker{reader: bytes.NewReader(content)}
	provider := newPlugin(format.FormatTIFF)

	info, err := provider.DescribeMedia(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("DescribeMedia() error = %v", err)
	}
	if info.Media == nil || info.Media.Width != 6000 || info.Media.Height != 6000 {
		t.Fatalf("media info = %#v, want dimensions from tail IFD", info.Media)
	}
	if info.Spatial == nil || info.Spatial.SRID == nil || *info.Spatial.SRID != 4326 {
		t.Fatalf("spatial info = %#v, want EPSG:4326 from tail IFD", info.Spatial)
	}
	if info.Spatial.Extent == nil {
		t.Fatalf("spatial extent missing")
	}
	if reader.bytesRead > tiffMetadataReadLimit*2 {
		t.Fatalf("bytes read = %d, want at most two metadata windows %d", reader.bytesRead, tiffMetadataReadLimit*2)
	}
}

func TestDescribeMediaIgnoresTIFFMetadataOutsideHeadAndTailWindows(t *testing.T) {
	t.Parallel()

	content := testGeoTIFFWithIFDOffset(t, tiffMetadataReadLimit+4096)
	content = append(content, bytes.Repeat([]byte{0}, tiffMetadataReadLimit+4096)...)
	reader := &countingReadSeeker{reader: bytes.NewReader(content)}
	provider := newPlugin(format.FormatTIFF)

	info, err := provider.DescribeMedia(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("DescribeMedia() error = %v", err)
	}
	if info.Media != nil || info.Spatial != nil {
		t.Fatalf("media=%#v spatial=%#v, want no facts when IFD is outside windows", info.Media, info.Spatial)
	}
}

func TestDescribeMediaReturnsPartialBigTIFFFormatInfo(t *testing.T) {
	t.Parallel()

	provider := newPlugin(format.FormatTIFF)
	info, err := provider.DescribeMedia(context.Background(), bytes.NewReader([]byte{'I', 'I', 43, 0, 8, 0, 0, 0}), nil)
	if err != nil {
		t.Fatalf("DescribeMedia() error = %v", err)
	}
	if info.Media != nil {
		t.Fatalf("media info = %#v, want nil when bounded parser cannot read BigTIFF dimensions", info.Media)
	}
	if info.FormatInfo["profile"] != "unknown" || info.FormatInfo["big_tiff"] != true {
		t.Fatalf("format info = %#v, want partial BigTIFF facts", info.FormatInfo)
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

func testGeoTIFFWithIFDOffset(t *testing.T, ifdOffset int) []byte {
	t.Helper()

	if ifdOffset < 8 {
		t.Fatalf("ifdOffset must be >= 8")
	}
	tags := []tiffTestTag{
		{tag: tagImageWidth, typ: tiffTypeLong, value: 6000},
		{tag: tagImageLength, typ: tiffTypeLong, value: 6000},
		{tag: tagModelPixelScale, typ: tiffTypeDouble, data: doublesBytes([]float64{0.000833333333333, 0.000833333333333, 0})},
		{tag: tagModelTiepoint, typ: tiffTypeDouble, data: doublesBytes([]float64{0, 0, 0, 15, 60, 0})},
		{tag: tagGeoKeyDirectory, typ: tiffTypeShort, data: shortsBytes([]uint16{1, 1, 0, 1, geoKeyGeographicType, 0, 1, 4326})},
	}
	ifd := testIFDAtOffset(t, ifdOffset, tags, 0)
	data := make([]byte, ifdOffset+len(ifd))
	copy(data[:8], []byte{'I', 'I', 42, 0, byte(ifdOffset), byte(ifdOffset >> 8), byte(ifdOffset >> 16), byte(ifdOffset >> 24)})
	copy(data[ifdOffset:], ifd)
	return data
}

func testIFDAtOffset(t *testing.T, ifdOffset int, tags []tiffTestTag, nextOffset uint32) []byte {
	t.Helper()

	var buf bytes.Buffer
	order := binary.LittleEndian
	entryCount := uint16(len(tags))
	dataStart := ifdOffset + 2 + int(entryCount)*12 + 4
	nextDataOffset := dataStart
	entries := make([][]byte, 0, len(tags))
	payloads := make([][]byte, 0, len(tags))
	for _, tag := range tags {
		count := uint32(1)
		value := tag.value
		if len(tag.data) > 0 {
			count = uint32(len(tag.data) / tiffTypeSize(tag.typ))
			value = uint32(nextDataOffset)
			nextDataOffset += len(tag.data)
			payloads = append(payloads, tag.data)
		}
		entries = append(entries, buildIFDEntry(order, tag.tag, tag.typ, count, value))
	}
	_ = binary.Write(&buf, order, entryCount)
	for _, entry := range entries {
		buf.Write(entry)
	}
	_ = binary.Write(&buf, order, nextOffset)
	for _, payload := range payloads {
		buf.Write(payload)
	}
	return buf.Bytes()
}

func testCOGCandidateTIFF(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	order := binary.LittleEndian
	_, _ = buf.Write([]byte{'I', 'I'})
	_ = binary.Write(&buf, order, uint16(42))
	_ = binary.Write(&buf, order, uint32(8))

	entryCount := uint16(8)
	dataStart := 8 + 2 + int(entryCount)*12 + 4
	scaleBytes := doublesBytes([]float64{10, 10, 0})
	tiepointBytes := doublesBytes([]float64{0, 0, 0, 100, 200, 0})
	geoKeyBytes := shortsBytes([]uint16{1, 1, 0, 1, geoKeyGeographicType, 0, 1, 4326})
	scaleOffset := dataStart
	tiepointOffset := scaleOffset + len(scaleBytes)
	geoKeyOffset := tiepointOffset + len(tiepointBytes)
	subIFDOffset := geoKeyOffset + len(geoKeyBytes)
	subIFDBytes := reducedResolutionIFDBytes(order)

	_ = binary.Write(&buf, order, entryCount)
	buf.Write(buildIFDEntry(order, tagModelPixelScale, tiffTypeDouble, 3, uint32(scaleOffset)))
	buf.Write(buildIFDEntry(order, tagModelTiepoint, tiffTypeDouble, 6, uint32(tiepointOffset)))
	buf.Write(buildIFDEntry(order, tagGeoKeyDirectory, tiffTypeShort, 8, uint32(geoKeyOffset)))
	buf.Write(buildIFDEntry(order, tagTileWidth, tiffTypeLong, 1, 256))
	buf.Write(buildIFDEntry(order, tagTileLength, tiffTypeLong, 1, 256))
	buf.Write(buildIFDEntry(order, tagTileOffsets, tiffTypeLong, 1, 1024))
	buf.Write(buildIFDEntry(order, tagTileByteCounts, tiffTypeLong, 1, 4096))
	buf.Write(buildIFDEntry(order, tagSubIFDs, tiffTypeLong, 1, uint32(subIFDOffset)))
	_ = binary.Write(&buf, order, uint32(0))
	buf.Write(scaleBytes)
	buf.Write(tiepointBytes)
	buf.Write(geoKeyBytes)
	buf.Write(subIFDBytes)
	return buf.Bytes()
}

func reducedResolutionIFDBytes(order binary.ByteOrder) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, order, uint16(1))
	buf.Write(buildIFDEntry(order, tagNewSubfileType, tiffTypeLong, 1, 1))
	_ = binary.Write(&buf, order, uint32(0))
	return buf.Bytes()
}

func testEncodedTIFF(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := tiff.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode tiff: %v", err)
	}
	return buf.Bytes()
}

type tiffTestTag struct {
	tag   uint16
	typ   uint16
	value uint32
	data  []byte
}

func testTIFFWithTags(t *testing.T, tags []tiffTestTag, nextOffset uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	order := binary.LittleEndian
	_, _ = buf.Write([]byte{'I', 'I'})
	_ = binary.Write(&buf, order, uint16(42))
	_ = binary.Write(&buf, order, uint32(8))
	entryCount := uint16(len(tags))
	dataStart := 8 + 2 + int(entryCount)*12 + 4
	nextDataOffset := dataStart
	entries := make([][]byte, 0, len(tags))
	payloads := make([][]byte, 0, len(tags))
	for _, tag := range tags {
		count := uint32(1)
		value := tag.value
		if len(tag.data) > 0 {
			count = uint32(len(tag.data) / tiffTypeSize(tag.typ))
			value = uint32(nextDataOffset)
			nextDataOffset += len(tag.data)
			payloads = append(payloads, tag.data)
		}
		entries = append(entries, buildIFDEntry(order, tag.tag, tag.typ, count, value))
	}
	_ = binary.Write(&buf, order, entryCount)
	for _, entry := range entries {
		buf.Write(entry)
	}
	_ = binary.Write(&buf, order, nextOffset)
	for _, payload := range payloads {
		buf.Write(payload)
	}
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

type countingReader struct {
	reader    *bytes.Reader
	bytesRead int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytesRead += int64(n)
	return n, err
}

type countingReadSeeker struct {
	reader    *bytes.Reader
	bytesRead int64
}

func (r *countingReadSeeker) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytesRead += int64(n)
	return n, err
}

func (r *countingReadSeeker) Seek(offset int64, whence int) (int64, error) {
	return r.reader.Seek(offset, whence)
}
