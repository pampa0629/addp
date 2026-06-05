package image

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/spatial"
)

const (
	tiffTypeShort  = 3
	tiffTypeDouble = 12

	tagModelPixelScale     = 33550
	tagModelTiepoint       = 33922
	tagModelTransformation = 34264
	tagGeoKeyDirectory     = 34735
	tagGeoAsciiParams      = 34737

	geoKeyProjectedCSType = 3072
	geoKeyGeographicType  = 2048
	geoKeyGTCitation      = 1026
	geoKeyGeogCitation    = 2049
	geoKeyProjectedCS     = 3073
)

type tiffIFD struct {
	order   binary.ByteOrder
	payload []byte
	tags    map[uint16]tiffTag
}

type tiffTag struct {
	typ    uint16
	count  uint32
	value  uint32
	inline []byte
}

func extractGeoTIFFSpatial(data []byte, width, height int) *datatype.SpatialInfo {
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

func parseFirstIFD(data []byte) (*tiffIFD, bool) {
	if len(data) < 8 {
		return nil, false
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, false
	}
	if order.Uint16(data[2:4]) != 42 {
		return nil, false
	}
	offset := int(order.Uint32(data[4:8]))
	if offset < 0 || offset+2 > len(data) {
		return nil, false
	}
	count := int(order.Uint16(data[offset : offset+2]))
	pos := offset + 2
	if count < 0 || pos+count*12 > len(data) {
		return nil, false
	}
	ifd := &tiffIFD{order: order, payload: data, tags: map[uint16]tiffTag{}}
	for i := 0; i < count; i++ {
		entry := data[pos+i*12 : pos+(i+1)*12]
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

func (ifd *tiffIFD) ascii(tag uint16) string {
	entry, ok := ifd.tags[tag]
	if !ok {
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
	offset := int(entry.value)
	if offset < 0 || offset+total > len(ifd.payload) {
		return nil, false
	}
	return ifd.payload[offset : offset+total], true
}

func tiffTypeSize(typ uint16) int {
	switch typ {
	case 1, 2, 6, 7:
		return 1
	case 3, 8:
		return 2
	case 4, 9, 11:
		return 4
	case 5, 10, 12:
		return 8
	default:
		return 0
	}
}
