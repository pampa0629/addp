package lasfamily

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/addp/common/datatype"
)

const (
	Magic         = "LASF"
	MaxHeaderRead = 375
)

type Header struct {
	VersionMajor          uint8
	VersionMinor          uint8
	SystemIdentifier      string
	GeneratingSoftware    string
	HeaderSize            uint16
	OffsetToPointData     uint32
	VariableLengthRecords uint32
	PointDataFormat       uint8
	PointDataRecordLength uint16
	LegacyPointCount      uint32
	PointCount            uint64
	ScaleX                float64
	ScaleY                float64
	ScaleZ                float64
	OffsetX               float64
	OffsetY               float64
	OffsetZ               float64
	MaxX                  float64
	MinX                  float64
	MaxY                  float64
	MinY                  float64
	MaxZ                  float64
	MinZ                  float64
	EVLROffset            uint64
	EVLRCount             uint32
}

func ReadHeader(input io.Reader) (*Header, error) {
	buf := make([]byte, MaxHeaderRead)
	n, err := io.ReadFull(input, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, fmt.Errorf("read LAS-family header: %w", err)
	}
	if n < 227 {
		return nil, fmt.Errorf("LAS-family header too short: %d", n)
	}
	buf = buf[:n]
	if string(buf[:4]) != Magic {
		return nil, fmt.Errorf("invalid LAS-family magic")
	}
	header := &Header{
		VersionMajor:          buf[24],
		VersionMinor:          buf[25],
		SystemIdentifier:      trimNullString(buf[26:58]),
		GeneratingSoftware:    trimNullString(buf[58:90]),
		HeaderSize:            binary.LittleEndian.Uint16(buf[94:96]),
		OffsetToPointData:     binary.LittleEndian.Uint32(buf[96:100]),
		VariableLengthRecords: binary.LittleEndian.Uint32(buf[100:104]),
		PointDataFormat:       buf[104] & 0x3F,
		PointDataRecordLength: binary.LittleEndian.Uint16(buf[105:107]),
		LegacyPointCount:      binary.LittleEndian.Uint32(buf[107:111]),
		ScaleX:                mathFloat64(buf[131:139]),
		ScaleY:                mathFloat64(buf[139:147]),
		ScaleZ:                mathFloat64(buf[147:155]),
		OffsetX:               mathFloat64(buf[155:163]),
		OffsetY:               mathFloat64(buf[163:171]),
		OffsetZ:               mathFloat64(buf[171:179]),
		MaxX:                  mathFloat64(buf[179:187]),
		MinX:                  mathFloat64(buf[187:195]),
		MaxY:                  mathFloat64(buf[195:203]),
		MinY:                  mathFloat64(buf[203:211]),
		MaxZ:                  mathFloat64(buf[211:219]),
		MinZ:                  mathFloat64(buf[219:227]),
	}
	header.PointCount = uint64(header.LegacyPointCount)
	if header.VersionMajor > 1 || (header.VersionMajor == 1 && header.VersionMinor >= 4 && header.HeaderSize >= 375 && len(buf) >= 375) {
		header.EVLROffset = binary.LittleEndian.Uint64(buf[235:243])
		header.EVLRCount = binary.LittleEndian.Uint32(buf[243:247])
		extendedPointCount := binary.LittleEndian.Uint64(buf[247:255])
		if extendedPointCount > 0 {
			header.PointCount = extendedPointCount
		}
	}
	return header, nil
}

func BuildPointCloudInfo(header *Header, kind string) *datatype.PointCloudInfo {
	if header == nil {
		return nil
	}
	pointCount := int64(header.PointCount)
	dimensions := Dimensions(header.PointDataFormat)
	dimensionCount := len(dimensions)
	hasColor := PointFormatHasColor(header.PointDataFormat)
	hasIntensity := true
	hasClassification := true
	return datatype.NormalizePointCloudInfo(&datatype.PointCloudInfo{
		PointCloudKind:    kind,
		PointCount:        optionalInt64(pointCount),
		PointFormat:       fmt.Sprintf("las_%d.%d_point_format_%d", header.VersionMajor, header.VersionMinor, header.PointDataFormat),
		DimensionCount:    optionalInt(dimensionCount),
		Dimensions:        dimensions,
		Bounds3D:          Bounds(header),
		Scale:             []float64{header.ScaleX, header.ScaleY, header.ScaleZ},
		Offset:            []float64{header.OffsetX, header.OffsetY, header.OffsetZ},
		HasColor:          &hasColor,
		HasIntensity:      &hasIntensity,
		HasClassification: &hasClassification,
	})
}

func BuildSpatialInfo(header *Header) *datatype.SpatialInfo {
	if header == nil {
		return nil
	}
	extent := datatype.NewBoundingBox(header.MinX, header.MinY, header.MaxX, header.MaxY)
	return &datatype.SpatialInfo{
		Extent: &extent,
	}
}

func BuildHeaderFormatInfo(header *Header) map[string]interface{} {
	if header == nil {
		return nil
	}
	info := map[string]interface{}{
		"version":                  fmt.Sprintf("%d.%d", header.VersionMajor, header.VersionMinor),
		"header_size":              int(header.HeaderSize),
		"offset_to_point_data":     int(header.OffsetToPointData),
		"variable_length_records":  int(header.VariableLengthRecords),
		"point_data_format":        int(header.PointDataFormat),
		"point_data_record_length": int(header.PointDataRecordLength),
	}
	if header.LegacyPointCount > 0 {
		info["legacy_point_count"] = int64(header.LegacyPointCount)
	}
	if header.EVLROffset > 0 {
		info["evlr_offset"] = header.EVLROffset
	}
	if header.EVLRCount > 0 {
		info["evlr_count"] = int(header.EVLRCount)
	}
	if header.SystemIdentifier != "" {
		info["system_identifier"] = header.SystemIdentifier
	}
	if header.GeneratingSoftware != "" {
		info["generating_software"] = header.GeneratingSoftware
	}
	return info
}

func Bounds(header *Header) *datatype.Bounds3D {
	if header == nil {
		return nil
	}
	return &datatype.Bounds3D{
		MinX: float64Ptr(header.MinX),
		MinY: float64Ptr(header.MinY),
		MinZ: float64Ptr(header.MinZ),
		MaxX: float64Ptr(header.MaxX),
		MaxY: float64Ptr(header.MaxY),
		MaxZ: float64Ptr(header.MaxZ),
	}
}

func Dimensions(pointFormat uint8) []string {
	dimensions := []string{"x", "y", "z", "intensity", "return_number", "classification", "scan_angle", "gps_time"}
	if PointFormatHasColor(pointFormat) {
		dimensions = append(dimensions, "red", "green", "blue")
	}
	if pointFormat == 8 || pointFormat == 10 {
		dimensions = append(dimensions, "nir")
	}
	return dimensions
}

func PointFormatHasColor(pointFormat uint8) bool {
	switch pointFormat {
	case 2, 3, 5, 7, 8, 10:
		return true
	default:
		return false
	}
}

func optionalInt64(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func optionalInt(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}

func float64Ptr(value float64) *float64 {
	return &value
}

func mathFloat64(data []byte) float64 {
	return math.Float64frombits(binary.LittleEndian.Uint64(data))
}

func trimNullString(data []byte) string {
	return strings.TrimSpace(strings.TrimRight(string(data), "\x00"))
}
