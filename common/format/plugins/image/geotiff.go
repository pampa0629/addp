package image

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"math"
	"strconv"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/spatial"
)

const (
	tiffTypeByte   = 1
	tiffTypeASCII  = 2
	tiffTypeShort  = 3
	tiffTypeLong   = 4
	tiffTypeSByte  = 6
	tiffTypeSShort = 8
	tiffTypeSLong  = 9
	tiffTypeFloat  = 11
	tiffTypeDouble = 12
	tiffTypeIFD    = 13

	tagNewSubfileType      = 254
	tagImageWidth          = 256
	tagImageLength         = 257
	tagModelPixelScale     = 33550
	tagModelTiepoint       = 33922
	tagModelTransformation = 34264
	tagSubIFDs             = 330
	tagTileWidth           = 322
	tagTileLength          = 323
	tagTileOffsets         = 324
	tagTileByteCounts      = 325
	tagSMinSampleValue     = 340
	tagSMaxSampleValue     = 341
	tagGeoKeyDirectory     = 34735
	tagGeoAsciiParams      = 34737
	tagGDALMetadata        = 42112
	tagGDALNoData          = 42113

	geoKeyProjectedCSType = 3072
	geoKeyGeographicType  = 2048
	geoKeyGTCitation      = 1026
	geoKeyGeogCitation    = 2049
	geoKeyProjectedCS     = 3073
)

type tiffIFD struct {
	order      binary.ByteOrder
	payload    tiffMetadata
	tags       map[uint16]tiffTag
	nextOffset uint32
}

type tiffTag struct {
	typ    uint16
	count  uint32
	value  uint32
	inline []byte
}

func extractTIFFDimensions(data tiffMetadata) (int, int, bool) {
	ifd, ok := parseFirstIFD(data)
	if !ok {
		return 0, 0, false
	}
	width, okWidth := ifd.firstLong(tagImageWidth)
	height, okHeight := ifd.firstLong(tagImageLength)
	if !okWidth || !okHeight || width == 0 || height == 0 {
		return 0, 0, false
	}
	if uint64(width) > uint64(math.MaxInt) || uint64(height) > uint64(math.MaxInt) {
		return 0, 0, false
	}
	return int(width), int(height), true
}

func extractGeoTIFFSpatial(data tiffMetadata, width, height int) *datatype.SpatialInfo {
	if width <= 0 || height <= 0 {
		return nil
	}
	ifd, ok := parseFirstIFD(data)
	if !ok {
		return nil
	}

	spatialInfo := &datatype.SpatialInfo{}
	hasSpatialFact := false
	if extent, ok := geoTIFFExtent(ifd, width, height); ok {
		bbox := datatype.BoundingBox{extent[0], extent[1], extent[2], extent[3]}
		spatialInfo.Extent = &bbox
		hasSpatialFact = true
	}
	if srid, crs := geoTIFFCRS(ifd); srid > 0 {
		spatialInfo.SRID = &srid
		hasSpatialFact = true
	} else if crs != "" {
		spatialInfo.CRSRef = datatype.CustomCRSRef(crs)
		spatialInfo.CRSDefinitions = []datatype.CRSDefinition{{
			ID:                 spatialInfo.CRSRef,
			DefinitionEncoding: datatype.CRSDefinitionEncodingWKT,
			Definition:         crs,
			Source:             datatype.CRSDefinitionSourceGeoTIFFTags,
		}}
		hasSpatialFact = true
	}
	hasSpatialIndex := false
	spatialInfo.HasSpatialIndex = &hasSpatialIndex
	if !hasSpatialFact {
		return nil
	}
	return spatialInfo
}

func extractTIFFFormatInfo(data tiffMetadata, spatialInfo *datatype.SpatialInfo) map[string]interface{} {
	info := map[string]interface{}{}
	if data.Len() < 4 {
		return info
	}
	if isBigTIFF(data) {
		info["profile"] = "unknown"
		info["big_tiff"] = true
		return info
	}
	ifd, ok := parseFirstIFD(data)
	if !ok {
		return info
	}
	isTiled := ifd.hasTag(tagTileWidth) && ifd.hasTag(tagTileLength) && ifd.hasTag(tagTileOffsets) && ifd.hasTag(tagTileByteCounts)
	hasOverviews := hasReducedResolutionSubIFD(ifd) || hasReducedResolutionNextIFD(ifd)
	hasSpatial := spatialInfo != nil
	profile := "plain_tiff"
	isCloudOptimized := false
	if hasSpatial {
		profile = "geotiff"
	}
	if hasSpatial && isTiled && hasOverviews {
		profile = "cog"
		isCloudOptimized = true
		info["cog_check_level"] = "heuristic"
	}
	info["profile"] = profile
	info["big_tiff"] = false
	info["is_cloud_optimized"] = isCloudOptimized
	info["is_tiled"] = isTiled
	info["has_overviews"] = hasOverviews
	for key, value := range tiffRenderStats(ifd) {
		info[key] = value
	}
	return info
}

func tiffRenderStats(ifd *tiffIFD) map[string]interface{} {
	if ifd == nil {
		return nil
	}
	stats := map[string]interface{}{}
	if nodata, ok := parseFiniteFloat(ifd.ascii(tagGDALNoData)); ok {
		stats["nodata"] = nodata
	}

	minValue, hasMin := ifd.firstNumber(tagSMinSampleValue)
	maxValue, hasMax := ifd.firstNumber(tagSMaxSampleValue)
	method := ""
	if metadataMin, metadataMax, ok := gdalMetadataStatistics(ifd.ascii(tagGDALMetadata)); ok {
		minValue, maxValue = metadataMin, metadataMax
		hasMin, hasMax = true, true
		method = "metadata_statistics"
	} else if hasMin && hasMax {
		method = "sample_value_tags"
	}
	if hasMin {
		stats["sample_min"] = minValue
	}
	if hasMax {
		stats["sample_max"] = maxValue
	}
	if hasMin && hasMax && maxValue > minValue {
		stats["display_min"] = minValue
		stats["display_max"] = maxValue
		if method != "" {
			stats["display_range_method"] = method
		}
	}
	if len(stats) == 0 {
		return nil
	}
	return stats
}

func gdalMetadataStatistics(payload string) (float64, float64, bool) {
	payload = strings.TrimSpace(strings.TrimRight(payload, "\x00"))
	if payload == "" {
		return 0, 0, false
	}
	decoder := xml.NewDecoder(strings.NewReader(payload))
	var minValue, maxValue float64
	var hasMin, hasMax bool
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || !strings.EqualFold(start.Name.Local, "Item") {
			continue
		}
		name := ""
		for _, attr := range start.Attr {
			if strings.EqualFold(attr.Name.Local, "name") {
				name = strings.TrimSpace(attr.Value)
				break
			}
		}
		if !strings.EqualFold(name, "STATISTICS_MINIMUM") && !strings.EqualFold(name, "STATISTICS_MAXIMUM") {
			continue
		}
		var text string
		if err := decoder.DecodeElement(&text, &start); err != nil {
			continue
		}
		value, ok := parseFiniteFloat(text)
		if !ok {
			continue
		}
		if strings.EqualFold(name, "STATISTICS_MINIMUM") && !hasMin {
			minValue = value
			hasMin = true
		}
		if strings.EqualFold(name, "STATISTICS_MAXIMUM") && !hasMax {
			maxValue = value
			hasMax = true
		}
	}
	return minValue, maxValue, hasMin && hasMax && maxValue > minValue
}

func parseFiniteFloat(text string) (float64, bool) {
	text = strings.TrimSpace(strings.TrimRight(text, "\x00"))
	if text == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func isBigTIFF(data tiffMetadata) bool {
	header := data.firstBytes(4)
	if len(header) < 4 {
		return false
	}
	switch string(header[:2]) {
	case "II":
		return binary.LittleEndian.Uint16(header[2:4]) == 43
	case "MM":
		return binary.BigEndian.Uint16(header[2:4]) == 43
	default:
		return false
	}
}

func hasReducedResolutionNextIFD(ifd *tiffIFD) bool {
	if ifd == nil || ifd.nextOffset == 0 {
		return false
	}
	nextIFD, ok := parseIFDAt(ifd.payload, ifd.order, uint64(ifd.nextOffset))
	if !ok {
		return false
	}
	value, ok := nextIFD.firstLong(tagNewSubfileType)
	return ok && value&1 == 1
}

func hasReducedResolutionSubIFD(ifd *tiffIFD) bool {
	if ifd == nil {
		return false
	}
	entry, ok := ifd.tags[tagSubIFDs]
	if !ok || entry.count == 0 || (entry.typ != tiffTypeLong && entry.typ != tiffTypeIFD) {
		return false
	}
	raw, ok := ifd.tagBytes(entry)
	if !ok || len(raw) < int(entry.count)*4 {
		return false
	}
	for i := 0; i < int(entry.count); i++ {
		offset := ifd.order.Uint32(raw[i*4 : i*4+4])
		if offset == 0 {
			continue
		}
		subIFD, ok := parseIFDAt(ifd.payload, ifd.order, uint64(offset))
		if !ok {
			continue
		}
		value, ok := subIFD.firstLong(tagNewSubfileType)
		if ok && value&1 == 1 {
			return true
		}
	}
	return false
}

func parseFirstIFD(data tiffMetadata) (*tiffIFD, bool) {
	header := data.firstBytes(8)
	if len(header) < 8 {
		return nil, false
	}
	order, ok := data.byteOrder()
	if !ok || order.Uint16(header[2:4]) != 42 {
		return nil, false
	}
	offset := uint64(order.Uint32(header[4:8]))
	return parseIFDAt(data, order, offset)
}

func parseIFDAt(data tiffMetadata, order binary.ByteOrder, offset uint64) (*tiffIFD, bool) {
	countBytes, ok := data.slice(offset, 2)
	if !ok {
		return nil, false
	}
	count := int(order.Uint16(countBytes))
	entryBytes, ok := data.slice(offset+2, uint64(count)*12+4)
	if !ok {
		return nil, false
	}
	ifd := &tiffIFD{order: order, payload: data, tags: map[uint16]tiffTag{}}
	for i := 0; i < count; i++ {
		entry := entryBytes[i*12 : (i+1)*12]
		tag := order.Uint16(entry[0:2])
		typ := order.Uint16(entry[2:4])
		valueCount := order.Uint32(entry[4:8])
		value := order.Uint32(entry[8:12])
		ifd.tags[tag] = tiffTag{
			typ:    typ,
			count:  valueCount,
			value:  value,
			inline: append([]byte(nil), entry[8:12]...),
		}
	}
	ifd.nextOffset = order.Uint32(entryBytes[count*12 : count*12+4])
	return ifd, true
}

func geoTIFFExtent(ifd *tiffIFD, width, height int) ([]float64, bool) {
	if matrix, ok := ifd.doubles(tagModelTransformation); ok && len(matrix) >= 16 {
		return extentFromTransform(matrix, width, height)
	}
	scale, hasScale := ifd.doubles(tagModelPixelScale)
	tiepoints, hasTiepoints := ifd.doubles(tagModelTiepoint)
	if !hasScale || !hasTiepoints || len(scale) < 2 || len(tiepoints) < 6 {
		return nil, false
	}
	return extentFromScaleTiepoint(scale, tiepoints, width, height)
}

func extentFromScaleTiepoint(scale, tiepoints []float64, width, height int) ([]float64, bool) {
	i, j := tiepoints[0], tiepoints[1]
	x, y := tiepoints[3], tiepoints[4]
	scaleX, scaleY := scale[0], scale[1]
	if scaleX == 0 || scaleY == 0 {
		return nil, false
	}
	minX := x - i*scaleX
	maxX := minX + float64(width)*scaleX
	maxY := y + j*scaleY
	minY := maxY - float64(height)*scaleY
	return normalizeExtent(minX, minY, maxX, maxY)
}

func extentFromTransform(matrix []float64, width, height int) ([]float64, bool) {
	points := [][2]float64{
		transformGeoTIFFPoint(matrix, 0, 0),
		transformGeoTIFFPoint(matrix, float64(width), 0),
		transformGeoTIFFPoint(matrix, 0, float64(height)),
		transformGeoTIFFPoint(matrix, float64(width), float64(height)),
	}
	minX, minY := points[0][0], points[0][1]
	maxX, maxY := minX, minY
	for _, point := range points[1:] {
		minX = math.Min(minX, point[0])
		minY = math.Min(minY, point[1])
		maxX = math.Max(maxX, point[0])
		maxY = math.Max(maxY, point[1])
	}
	return normalizeExtent(minX, minY, maxX, maxY)
}

func transformGeoTIFFPoint(matrix []float64, x, y float64) [2]float64 {
	return [2]float64{
		matrix[0]*x + matrix[1]*y + matrix[3],
		matrix[4]*x + matrix[5]*y + matrix[7],
	}
}

func normalizeExtent(minX, minY, maxX, maxY float64) ([]float64, bool) {
	values := []float64{minX, minY, maxX, maxY}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, false
		}
	}
	if minX > maxX {
		minX, maxX = maxX, minX
	}
	if minY > maxY {
		minY, maxY = maxY, minY
	}
	return []float64{minX, minY, maxX, maxY}, true
}

func geoTIFFCRS(ifd *tiffIFD) (int, string) {
	keys, ok := ifd.shorts(tagGeoKeyDirectory)
	if !ok || len(keys) < 4 {
		return 0, ""
	}
	ascii := ifd.ascii(tagGeoAsciiParams)
	entryCount := int(keys[3])
	crsParts := []string{}
	for i := 0; i < entryCount; i++ {
		pos := 4 + i*4
		if pos+3 >= len(keys) {
			break
		}
		keyID := keys[pos]
		location := keys[pos+1]
		count := keys[pos+2]
		value := keys[pos+3]
		switch keyID {
		case geoKeyProjectedCSType, geoKeyGeographicType:
			if location == 0 && value > 0 && value != 32767 {
				return int(value), ""
			}
		case geoKeyGTCitation, geoKeyGeogCitation, geoKeyProjectedCS:
			if location == tagGeoAsciiParams {
				if text := geoASCIIValue(ascii, int(value), int(count)); text != "" {
					crsParts = append(crsParts, text)
				}
			}
		}
	}
	crsText := strings.TrimSpace(strings.Join(crsParts, " "))
	if srid := spatial.ParseSRID(crsText); srid > 0 {
		return srid, ""
	}
	return 0, crsText
}

func geoASCIIValue(ascii string, offset, count int) string {
	if offset < 0 || offset >= len(ascii) || count <= 0 {
		return ""
	}
	end := offset + count
	if end > len(ascii) {
		end = len(ascii)
	}
	text := strings.Trim(ascii[offset:end], "|\x00 ")
	return strings.TrimSpace(text)
}

func (ifd *tiffIFD) shorts(tag uint16) ([]uint16, bool) {
	entry, ok := ifd.tags[tag]
	if !ok || entry.typ != tiffTypeShort || entry.count == 0 {
		return nil, false
	}
	raw, ok := ifd.tagBytes(entry)
	if !ok || len(raw) < int(entry.count)*2 {
		return nil, false
	}
	result := make([]uint16, int(entry.count))
	for i := range result {
		result[i] = ifd.order.Uint16(raw[i*2 : i*2+2])
	}
	return result, true
}

func (ifd *tiffIFD) hasTag(tag uint16) bool {
	_, ok := ifd.tags[tag]
	return ok
}

func (ifd *tiffIFD) firstLong(tag uint16) (uint32, bool) {
	entry, ok := ifd.tags[tag]
	if !ok {
		return 0, false
	}
	switch entry.typ {
	case tiffTypeShort:
		raw, ok := ifd.tagBytes(entry)
		if !ok || len(raw) < 2 {
			return 0, false
		}
		return uint32(ifd.order.Uint16(raw[:2])), true
	case tiffTypeLong:
		raw, ok := ifd.tagBytes(entry)
		if !ok || len(raw) < 4 {
			return 0, false
		}
		return ifd.order.Uint32(raw[:4]), true
	default:
		return 0, false
	}
}

func (ifd *tiffIFD) doubles(tag uint16) ([]float64, bool) {
	entry, ok := ifd.tags[tag]
	if !ok || entry.typ != tiffTypeDouble || entry.count == 0 {
		return nil, false
	}
	raw, ok := ifd.tagBytes(entry)
	if !ok || len(raw) < int(entry.count)*8 {
		return nil, false
	}
	result := make([]float64, int(entry.count))
	for i := range result {
		bits := ifd.order.Uint64(raw[i*8 : i*8+8])
		result[i] = math.Float64frombits(bits)
	}
	return result, true
}

func (ifd *tiffIFD) firstNumber(tag uint16) (float64, bool) {
	entry, ok := ifd.tags[tag]
	if !ok || entry.count == 0 {
		return 0, false
	}
	raw, ok := ifd.tagBytes(entry)
	if !ok {
		return 0, false
	}
	var value float64
	switch entry.typ {
	case tiffTypeByte:
		if len(raw) < 1 {
			return 0, false
		}
		value = float64(raw[0])
	case tiffTypeSByte:
		if len(raw) < 1 {
			return 0, false
		}
		value = float64(int8(raw[0]))
	case tiffTypeShort:
		if len(raw) < 2 {
			return 0, false
		}
		value = float64(ifd.order.Uint16(raw[:2]))
	case tiffTypeSShort:
		if len(raw) < 2 {
			return 0, false
		}
		value = float64(int16(ifd.order.Uint16(raw[:2])))
	case tiffTypeLong:
		if len(raw) < 4 {
			return 0, false
		}
		value = float64(ifd.order.Uint32(raw[:4]))
	case tiffTypeSLong:
		if len(raw) < 4 {
			return 0, false
		}
		value = float64(int32(ifd.order.Uint32(raw[:4])))
	case tiffTypeFloat:
		if len(raw) < 4 {
			return 0, false
		}
		value = float64(math.Float32frombits(ifd.order.Uint32(raw[:4])))
	case tiffTypeDouble:
		if len(raw) < 8 {
			return 0, false
		}
		value = math.Float64frombits(ifd.order.Uint64(raw[:8]))
	default:
		return 0, false
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func (ifd *tiffIFD) ascii(tag uint16) string {
	entry, ok := ifd.tags[tag]
	if !ok || entry.typ != tiffTypeASCII {
		return ""
	}
	raw, ok := ifd.tagBytes(entry)
	if !ok {
		return ""
	}
	return string(bytes.TrimRight(raw, "\x00"))
}

func (ifd *tiffIFD) tagBytes(entry tiffTag) ([]byte, bool) {
	size := tiffTypeSize(entry.typ)
	if size == 0 {
		return nil, false
	}
	total := int(entry.count) * size
	if total <= 4 {
		return entry.inline[:total], true
	}
	return ifd.payload.slice(uint64(entry.value), uint64(total))
}

func tiffTypeSize(typ uint16) int {
	switch typ {
	case 1, 2, 6, 7:
		return 1
	case 3, 8:
		return 2
	case 4, 9, 11, 13:
		return 4
	case 5, 10, 12:
		return 8
	default:
		return 0
	}
}
