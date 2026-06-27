package las

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const (
	lasMagic         = "LASF"
	lasMaxHeaderRead = 375
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register LAS format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatLAS
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-las",
		Format:   format.FormatLAS,
		I18nKey:  "format.las",
		DataType: datatype.PointCloud,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions:        []string{".las"},
			MimeTypes:         []string{"application/vnd.las", "application/octet-stream"},
			ContentSignatures: []string{"hex:4c415346"},
		},
	}
}

func (p *Plugin) SniffFormat(peek []byte) bool {
	return len(peek) >= 4 && string(peek[:4]) == lasMagic
}

func (p *Plugin) DescribePointCloud(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.PointCloudDescribeResult, error) {
	header, err := readLASHeader(input)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return &format.PointCloudDescribeResult{
		PointCloud: buildPointCloudInfo(header),
		Spatial:    buildLASSpatialInfo(header),
		FormatInfo: buildLASFormatInfo(header),
	}, nil
}

type lasHeader struct {
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

func readLASHeader(input io.Reader) (*lasHeader, error) {
	buf := make([]byte, lasMaxHeaderRead)
	n, err := io.ReadFull(input, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, fmt.Errorf("read LAS header: %w", err)
	}
	if n < 227 {
		return nil, fmt.Errorf("LAS header too short: %d", n)
	}
	buf = buf[:n]
	if string(buf[:4]) != lasMagic {
		return nil, fmt.Errorf("invalid LAS magic")
	}
	header := &lasHeader{
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
	if len(buf) >= 375 {
		header.EVLROffset = binary.LittleEndian.Uint64(buf[235:243])
		header.EVLRCount = binary.LittleEndian.Uint32(buf[243:247])
		extendedPointCount := binary.LittleEndian.Uint64(buf[247:255])
		if extendedPointCount > 0 {
			header.PointCount = extendedPointCount
		}
	}
	return header, nil
}

func buildPointCloudInfo(header *lasHeader) *datatype.PointCloudInfo {
	if header == nil {
		return nil
	}
	pointCount := int64(header.PointCount)
	dimensions := lasDimensions(header.PointDataFormat)
	dimensionCount := len(dimensions)
	hasColor := lasPointFormatHasColor(header.PointDataFormat)
	hasIntensity := true
	hasClassification := true
	return datatype.NormalizePointCloudInfo(&datatype.PointCloudInfo{
		PointCloudKind:    datatype.PointCloudKindRawPointCloud,
		PointCount:        optionalInt64(pointCount),
		PointFormat:       fmt.Sprintf("las_%d.%d_point_format_%d", header.VersionMajor, header.VersionMinor, header.PointDataFormat),
		DimensionCount:    optionalInt(dimensionCount),
		Dimensions:        dimensions,
		Bounds3D:          lasBounds(header),
		Scale:             []float64{header.ScaleX, header.ScaleY, header.ScaleZ},
		Offset:            []float64{header.OffsetX, header.OffsetY, header.OffsetZ},
		HasColor:          &hasColor,
		HasIntensity:      &hasIntensity,
		HasClassification: &hasClassification,
	})
}

func buildLASSpatialInfo(header *lasHeader) *datatype.SpatialInfo {
	if header == nil {
		return nil
	}
	extent := datatype.NewBoundingBox(header.MinX, header.MinY, header.MaxX, header.MaxY)
	return &datatype.SpatialInfo{
		Extent: &extent,
	}
}

func buildLASFormatInfo(header *lasHeader) map[string]interface{} {
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

func lasBounds(header *lasHeader) *datatype.Bounds3D {
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

func lasDimensions(pointFormat uint8) []string {
	dimensions := []string{"x", "y", "z", "intensity", "return_number", "classification", "scan_angle", "gps_time"}
	if lasPointFormatHasColor(pointFormat) {
		dimensions = append(dimensions, "red", "green", "blue")
	}
	if pointFormat == 8 || pointFormat == 10 {
		dimensions = append(dimensions, "nir")
	}
	return dimensions
}

func lasPointFormatHasColor(pointFormat uint8) bool {
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
