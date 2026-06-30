package ply

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const (
	maxPLYHeaderLines           = 4096
	maxExactPLYBoundsVertices   = 100000
	plySampledBoundsSampleCount = 8192
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register PLY format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatPLY
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-ply",
		Format:   format.FormatPLY,
		I18nKey:  "format.ply",
		DataType: datatype.Model3D,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions:        []string{".ply"},
			MimeTypes:         []string{"model/ply", "application/octet-stream"},
			ContentSignatures: []string{"text:ply"},
		},
	}
}

func (p *Plugin) SniffFormat(peek []byte) bool {
	trimmed := bytes.TrimLeft(peek, "\ufeff \t\r\n")
	return bytes.HasPrefix(trimmed, []byte("ply\n")) || bytes.HasPrefix(trimmed, []byte("ply\r\n"))
}

func (p *Plugin) DescribeModel3D(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	header, err := readPLYHeader(ctx, input)
	if err != nil {
		return nil, err
	}
	formatInfo := plyFormatInfo(header)
	if header.Layout != "mesh" {
		return &format.Model3DDescribeResult{FormatInfo: formatInfo}, nil
	}
	model := &datatype.Model3DInfo{
		ModelKind:   datatype.Model3DKindMeshScene,
		MeshCount:   int64Ptr(1),
		VertexCount: int64Ptr(header.VertexCount),
	}
	return &format.Model3DDescribeResult{Model3D: model, FormatInfo: formatInfo}, nil
}

func (p *Plugin) DescribePointCloud(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.PointCloudDescribeResult, error) {
	header, err := readPLYHeader(ctx, input)
	if err != nil {
		return nil, err
	}
	if header.Layout != "point_cloud" {
		return &format.PointCloudDescribeResult{FormatInfo: plyFormatInfo(header)}, nil
	}
	dimensionCount := len(header.VertexProperties)
	pointCloud := &datatype.PointCloudInfo{
		PointCloudKind: datatype.PointCloudKindRawPointCloud,
		PointCount:     int64Ptr(header.VertexCount),
		PointFormat:    "ply_vertex",
		DimensionCount: &dimensionCount,
		Dimensions:     append([]string(nil), header.VertexProperties...),
		HasColor:       boolPtr(header.HasColor),
		HasIntensity:   boolPtr(header.HasIntensity),
	}
	return &format.PointCloudDescribeResult{
		PointCloud: pointCloud,
		FormatInfo: plyFormatInfo(header),
	}, nil
}

func (p *Plugin) DescribeGaussianSplat(ctx context.Context, input format.GaussianSplatDescribeInput, options *format.ParseOptions) (*format.GaussianSplatDescribeResult, error) {
	reader := input.Reader
	if reader == nil && input.RangeReader != nil {
		rc, err := input.RangeReader.Open(ctx, input.Ref)
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		reader = rc
	}
	if reader == nil {
		return nil, nil
	}
	header, err := readPLYHeader(ctx, reader)
	if err != nil {
		return nil, err
	}
	if header.Layout != "gaussian_splat" {
		return &format.GaussianSplatDescribeResult{FormatInfo: plyFormatInfo(header)}, nil
	}
	gaussian := &datatype.GaussianSplatInfo{
		Representation:        datatype.GaussianSplatRepresentation3DGS,
		SplatCount:            int64Ptr(header.VertexCount),
		HasOpacity:            boolPtr(header.HasOpacity),
		HasScale:              boolPtr(header.HasScale),
		HasRotation:           boolPtr(header.HasRotation),
		HasSphericalHarmonics: boolPtr(header.HasSphericalHarmonics),
	}
	if header.SHDegree >= 0 {
		gaussian.SHDegree = intPtr(header.SHDegree)
	}
	if bounds, err := readPLYGaussianBounds(ctx, reader, header); err == nil {
		gaussian.Bounds3D = bounds
	} else if ctx.Err() != nil {
		return nil, err
	}
	if gaussian.Bounds3D == nil {
		sampled, err := describePLYGaussianSampledBounds(ctx, input.RangeReader, input.Ref, input.SizeBytes, header)
		if err != nil {
			return nil, err
		}
		if sampled != nil {
			gaussian.SampledBounds3D = sampled.SampledBounds3D
			gaussian.SampledBoundsMethod = sampled.SampledBoundsMethod
			gaussian.SampledBoundsSampleCount = sampled.SampledBoundsSampleCount
		}
	}
	return &format.GaussianSplatDescribeResult{
		GaussianSplat: gaussian,
		FormatInfo:    plyFormatInfo(header),
	}, nil
}

func describePLYGaussianSampledBounds(ctx context.Context, reader contentio.RangeReader, ref contentio.Ref, sizeBytes int64, header *plyHeader) (*datatype.GaussianSplatInfo, error) {
	if reader == nil || sizeBytes <= 0 {
		return nil, nil
	}
	if header == nil || header.Layout != "gaussian_splat" {
		return nil, nil
	}
	if header.IsCompressedSplat {
		return nil, nil
	}
	if strings.ToLower(header.Encoding) != "binary_little_endian" || header.VertexCount <= 0 {
		return nil, nil
	}
	properties := plyVertexProperties(header)
	offsets, rowSize, ok := plyBinaryVertexLayout(properties)
	if !ok || rowSize <= 0 {
		return nil, nil
	}
	xOffset, okX := offsets["x"]
	yOffset, okY := offsets["y"]
	zOffset, okZ := offsets["z"]
	if !okX || !okY || !okZ {
		return nil, nil
	}
	sampleIndexes := uniformPLYSampleIndexes(header.VertexCount, plySampledBoundsSampleCount)
	if len(sampleIndexes) == 0 {
		return nil, nil
	}
	xType := plyPropertyType(properties, "x")
	yType := plyPropertyType(properties, "y")
	zType := plyPropertyType(properties, "z")
	var bounds plyBounds
	for _, index := range sampleIndexes {
		offset := header.HeaderBytes + index*int64(rowSize)
		if offset < 0 || offset >= sizeBytes {
			continue
		}
		rc, err := reader.OpenRange(ctx, ref, offset, int64(rowSize))
		if err != nil {
			return nil, err
		}
		row, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(row) < rowSize {
			continue
		}
		x, okX := readPLYBinaryFloat(row[xOffset:], xType)
		y, okY := readPLYBinaryFloat(row[yOffset:], yType)
		z, okZ := readPLYBinaryFloat(row[zOffset:], zType)
		if okX && okY && okZ {
			bounds.add(x, y, z)
		}
	}
	result := bounds.result()
	if result == nil {
		return nil, nil
	}
	return &datatype.GaussianSplatInfo{
		SampledBounds3D:          result,
		SampledBoundsMethod:      "sampled_binary_vertices",
		SampledBoundsSampleCount: int64Ptr(int64(len(sampleIndexes))),
	}, nil
}

type plyHeader struct {
	Encoding               string
	Version                string
	VertexCount            int64
	FaceCount              int64
	HeaderLineCount        int64
	HeaderBytes            int64
	Comments               []string
	ElementCounts          map[string]int64
	ElementProperties      map[string][]string
	ElementPropertyDetails map[string][]plyProperty
	VertexProperties       []string
	VertexPropertyDetails  []plyProperty
	IsGaussianSplat        bool
	IsCompressedSplat      bool
	HasColor               bool
	HasIntensity           bool
	HasOpacity             bool
	HasScale               bool
	HasRotation            bool
	HasSphericalHarmonics  bool
	SHDegree               int
	Layout                 string
	currentElementName     string
}

type plyProperty struct {
	Name     string
	TypeName string
	IsList   bool
}

func readPLYHeader(ctx context.Context, input io.Reader) (*plyHeader, error) {
	header := &plyHeader{
		ElementCounts:          map[string]int64{},
		ElementProperties:      map[string][]string{},
		ElementPropertyDetails: map[string][]plyProperty{},
	}
	for {
		rawLine, err := readPLYHeaderLine(input)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		line := strings.TrimSpace(rawLine)
		header.HeaderLineCount++
		header.HeaderBytes += int64(len(rawLine))
		if header.HeaderLineCount > maxPLYHeaderLines {
			return nil, fmt.Errorf("PLY header exceeds %d lines", maxPLYHeaderLines)
		}
		if header.HeaderLineCount%256 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		if header.HeaderLineCount == 1 {
			if line != "ply" {
				return nil, fmt.Errorf("invalid PLY magic")
			}
			continue
		}
		if line == "end_header" {
			if header.Encoding == "" {
				return nil, fmt.Errorf("PLY header missing format")
			}
			return finalizePLYHeader(header), nil
		}
		parsePLYHeaderLine(header, line)
	}
	return nil, fmt.Errorf("PLY header missing end_header")
}

func readPLYHeaderLine(input io.Reader) (string, error) {
	var buffer bytes.Buffer
	one := make([]byte, 1)
	for {
		n, err := input.Read(one)
		if n > 0 {
			buffer.WriteByte(one[0])
			if one[0] == '\n' {
				return buffer.String(), nil
			}
		}
		if err != nil {
			if err == io.EOF && buffer.Len() > 0 {
				return buffer.String(), nil
			}
			return "", err
		}
	}
}

func parsePLYHeaderLine(header *plyHeader, line string) {
	if line == "" {
		return
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return
	}
	switch fields[0] {
	case "format":
		if len(fields) >= 3 {
			header.Encoding = fields[1]
			header.Version = fields[2]
		}
	case "comment":
		comment := strings.TrimSpace(strings.TrimPrefix(line, "comment"))
		if comment != "" {
			header.Comments = append(header.Comments, comment)
		}
	case "element":
		if len(fields) >= 3 {
			count, err := strconv.ParseInt(fields[2], 10, 64)
			if err != nil || count < 0 {
				return
			}
			name := strings.TrimSpace(fields[1])
			header.currentElementName = name
			header.ElementCounts[name] = count
			if _, ok := header.ElementProperties[name]; !ok {
				header.ElementProperties[name] = nil
			}
			if _, ok := header.ElementPropertyDetails[name]; !ok {
				header.ElementPropertyDetails[name] = nil
			}
			switch name {
			case "vertex":
				header.VertexCount = count
			case "face":
				header.FaceCount = count
			}
		}
	case "property":
		if header.currentElementName != "" && len(fields) >= 3 {
			propertyName := fields[len(fields)-1]
			header.ElementProperties[header.currentElementName] = append(header.ElementProperties[header.currentElementName], propertyName)
			property := plyProperty{
				Name: strings.ToLower(strings.TrimSpace(propertyName)),
			}
			if len(fields) >= 5 && fields[1] == "list" {
				property.IsList = true
				property.TypeName = strings.ToLower(strings.TrimSpace(fields[3]))
			} else {
				property.TypeName = strings.ToLower(strings.TrimSpace(fields[1]))
			}
			header.ElementPropertyDetails[header.currentElementName] = append(header.ElementPropertyDetails[header.currentElementName], property)
			if header.currentElementName == "vertex" {
				header.VertexProperties = append(header.VertexProperties, propertyName)
				header.VertexPropertyDetails = append(header.VertexPropertyDetails, property)
			}
		}
	}
}

func finalizePLYHeader(header *plyHeader) *plyHeader {
	if header == nil {
		return nil
	}
	header.SHDegree = -1
	props := plyPropertySet(header.VertexProperties)
	allProps := plyPropertySet(plyAllProperties(header.ElementProperties))
	header.IsCompressedSplat = plyLooksLikeCompressedGaussianSplat(header, props)
	header.HasColor = plyHasColor(props)
	if header.IsCompressedSplat && props["packed_color"] {
		header.HasColor = true
	}
	header.HasIntensity = props["intensity"]
	header.HasOpacity = props["opacity"] || header.IsCompressedSplat
	header.HasScale = (props["scale_0"] && props["scale_1"] && props["scale_2"]) || header.IsCompressedSplat
	header.HasRotation = (props["rot_0"] && props["rot_1"] && props["rot_2"] && props["rot_3"]) || header.IsCompressedSplat
	header.HasSphericalHarmonics = plyHasSphericalHarmonics(allProps) || header.ElementCounts["sh"] > 0
	header.SHDegree = plySphericalHarmonicDegree(allProps)
	header.IsGaussianSplat = plyLooksLikeGaussianSplat(header, props) || header.IsCompressedSplat
	header.Layout = plyLayout(header)
	return header
}

func plyFormatInfo(header *plyHeader) map[string]interface{} {
	if header == nil {
		return nil
	}
	formatInfo := map[string]interface{}{
		"encoding":                header.Encoding,
		"version":                 header.Version,
		"layout":                  header.Layout,
		"vertex_count":            header.VertexCount,
		"header_line_count":       header.HeaderLineCount,
		"is_gaussian_splat":       header.IsGaussianSplat,
		"is_compressed_splat":     header.IsCompressedSplat,
		"vertex_property_count":   len(header.VertexProperties),
		"has_color":               header.HasColor,
		"has_intensity":           header.HasIntensity,
		"has_opacity":             header.HasOpacity,
		"has_scale":               header.HasScale,
		"has_rotation":            header.HasRotation,
		"has_spherical_harmonics": header.HasSphericalHarmonics,
	}
	if header.SHDegree >= 0 {
		formatInfo["sh_degree"] = header.SHDegree
	}
	if header.FaceCount > 0 {
		formatInfo["face_count"] = header.FaceCount
	}
	if len(header.Comments) > 0 {
		formatInfo["comments"] = header.Comments
	}
	if len(header.ElementCounts) > 0 {
		formatInfo["element_counts"] = header.ElementCounts
	}
	if len(header.VertexProperties) > 0 {
		formatInfo["vertex_properties"] = header.VertexProperties
	}
	return formatInfo
}

func plyAllProperties(elementProperties map[string][]string) []string {
	if len(elementProperties) == 0 {
		return nil
	}
	properties := make([]string, 0)
	for _, names := range elementProperties {
		properties = append(properties, names...)
	}
	return properties
}

func plyPropertySet(properties []string) map[string]bool {
	props := map[string]bool{}
	for _, prop := range properties {
		props[strings.ToLower(strings.TrimSpace(prop))] = true
	}
	return props
}

func plyLooksLikeGaussianSplat(header *plyHeader, props map[string]bool) bool {
	if header == nil || header.FaceCount > 0 {
		return false
	}
	if !(props["x"] && props["y"] && props["z"]) {
		return false
	}
	if !(header.HasOpacity && header.HasScale && header.HasRotation) {
		return false
	}
	return header.HasSphericalHarmonics || header.HasColor
}

func readPLYGaussianBounds(ctx context.Context, input io.Reader, header *plyHeader) (*datatype.Bounds3D, error) {
	if input == nil || header == nil || header.VertexCount <= 0 {
		return nil, nil
	}
	if header.IsCompressedSplat {
		return readCompressedPLYChunkBounds(ctx, input, header)
	}
	if header.VertexCount > maxExactPLYBoundsVertices {
		return nil, nil
	}
	switch strings.ToLower(header.Encoding) {
	case "ascii":
		return readASCIIPLYVertexBounds(ctx, input, header)
	case "binary_little_endian":
		return readBinaryLittleEndianPLYVertexBounds(ctx, input, header)
	default:
		return nil, nil
	}
}

func readASCIIPLYVertexBounds(ctx context.Context, input io.Reader, header *plyHeader) (*datatype.Bounds3D, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	xIndex, yIndex, zIndex := plyXYZIndexes(header.VertexProperties)
	if xIndex < 0 || yIndex < 0 || zIndex < 0 {
		return nil, nil
	}
	var bounds plyBounds
	for row := int64(0); row < header.VertexCount && scanner.Scan(); row++ {
		if row%65536 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) <= zIndex {
			continue
		}
		x, errX := strconv.ParseFloat(fields[xIndex], 64)
		y, errY := strconv.ParseFloat(fields[yIndex], 64)
		z, errZ := strconv.ParseFloat(fields[zIndex], 64)
		if errX == nil && errY == nil && errZ == nil {
			bounds.add(x, y, z)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return bounds.result(), nil
}

func readBinaryLittleEndianPLYVertexBounds(ctx context.Context, input io.Reader, header *plyHeader) (*datatype.Bounds3D, error) {
	properties := plyVertexProperties(header)
	offsets, rowSize, ok := plyBinaryVertexLayout(properties)
	if !ok || rowSize <= 0 {
		return nil, nil
	}
	xOffset, okX := offsets["x"]
	yOffset, okY := offsets["y"]
	zOffset, okZ := offsets["z"]
	if !okX || !okY || !okZ {
		return nil, nil
	}
	xType := plyPropertyType(properties, "x")
	yType := plyPropertyType(properties, "y")
	zType := plyPropertyType(properties, "z")
	row := make([]byte, rowSize)
	var bounds plyBounds
	for i := int64(0); i < header.VertexCount; i++ {
		if i%65536 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		if _, err := io.ReadFull(input, row); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return bounds.result(), nil
			}
			return nil, err
		}
		x, okX := readPLYBinaryFloat(row[xOffset:], xType)
		y, okY := readPLYBinaryFloat(row[yOffset:], yType)
		z, okZ := readPLYBinaryFloat(row[zOffset:], zType)
		if okX && okY && okZ {
			bounds.add(x, y, z)
		}
	}
	return bounds.result(), nil
}

func readCompressedPLYChunkBounds(ctx context.Context, input io.Reader, header *plyHeader) (*datatype.Bounds3D, error) {
	if strings.ToLower(header.Encoding) != "binary_little_endian" || header.ElementCounts["chunk"] <= 0 {
		return nil, nil
	}
	chunkProps := plyElementPropertyDetails(header, "chunk")
	offsets, rowSize, ok := plyBinaryVertexLayout(chunkProps)
	if !ok || rowSize <= 0 {
		return nil, nil
	}
	minXOffset, okMinX := offsets["min_x"]
	minYOffset, okMinY := offsets["min_y"]
	minZOffset, okMinZ := offsets["min_z"]
	maxXOffset, okMaxX := offsets["max_x"]
	maxYOffset, okMaxY := offsets["max_y"]
	maxZOffset, okMaxZ := offsets["max_z"]
	if !(okMinX && okMinY && okMinZ && okMaxX && okMaxY && okMaxZ) {
		return nil, nil
	}
	minXType := plyPropertyType(chunkProps, "min_x")
	minYType := plyPropertyType(chunkProps, "min_y")
	minZType := plyPropertyType(chunkProps, "min_z")
	maxXType := plyPropertyType(chunkProps, "max_x")
	maxYType := plyPropertyType(chunkProps, "max_y")
	maxZType := plyPropertyType(chunkProps, "max_z")
	row := make([]byte, rowSize)
	var bounds plyBounds
	for i := int64(0); i < header.ElementCounts["chunk"]; i++ {
		if i%65536 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		if _, err := io.ReadFull(input, row); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return bounds.result(), nil
			}
			return nil, err
		}
		minX, ok1 := readPLYBinaryFloat(row[minXOffset:], minXType)
		minY, ok2 := readPLYBinaryFloat(row[minYOffset:], minYType)
		minZ, ok3 := readPLYBinaryFloat(row[minZOffset:], minZType)
		maxX, ok4 := readPLYBinaryFloat(row[maxXOffset:], maxXType)
		maxY, ok5 := readPLYBinaryFloat(row[maxYOffset:], maxYType)
		maxZ, ok6 := readPLYBinaryFloat(row[maxZOffset:], maxZType)
		if ok1 && ok2 && ok3 {
			bounds.add(minX, minY, minZ)
		}
		if ok4 && ok5 && ok6 {
			bounds.add(maxX, maxY, maxZ)
		}
	}
	return bounds.result(), nil
}

func sampleCompressedPLYSampledBounds(ctx context.Context, reader contentio.RangeReader, ref contentio.Ref, header *plyHeader) (*datatype.Bounds3D, error) {
	chunkProps := plyElementPropertyDetails(header, "chunk")
	_, rowSize, ok := plyBinaryVertexLayout(chunkProps)
	if !ok || rowSize <= 0 || header.ElementCounts["chunk"] <= 0 {
		return nil, nil
	}
	length := header.ElementCounts["chunk"] * int64(rowSize)
	rc, err := reader.OpenRange(ctx, ref, header.HeaderBytes, length)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return readCompressedPLYChunkBounds(ctx, rc, header)
}

func uniformPLYSampleIndexes(count int64, maxSamples int) []int64 {
	if count <= 0 || maxSamples <= 0 {
		return nil
	}
	if count <= int64(maxSamples) {
		indexes := make([]int64, 0, count)
		for i := int64(0); i < count; i++ {
			indexes = append(indexes, i)
		}
		return indexes
	}
	indexes := make([]int64, 0, maxSamples)
	last := int64(-1)
	for i := 0; i < maxSamples; i++ {
		index := int64(math.Round(float64(i) * float64(count-1) / float64(maxSamples-1)))
		if index == last {
			continue
		}
		indexes = append(indexes, index)
		last = index
	}
	return indexes
}

func singlePLYSampledBoundsHeaderReadLimit(sizeBytes int64) int64 {
	limit := int64(1 << 20)
	if sizeBytes > 0 && sizeBytes < limit {
		return sizeBytes
	}
	return limit
}

func plyXYZIndexes(properties []string) (int, int, int) {
	xIndex, yIndex, zIndex := -1, -1, -1
	for index, name := range properties {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "x":
			xIndex = index
		case "y":
			yIndex = index
		case "z":
			zIndex = index
		}
	}
	return xIndex, yIndex, zIndex
}

func plyVertexProperties(header *plyHeader) []plyProperty {
	if header == nil {
		return nil
	}
	return header.VertexPropertyDetails
}

func plyElementPropertyDetails(header *plyHeader, elementName string) []plyProperty {
	if header == nil {
		return nil
	}
	return header.ElementPropertyDetails[elementName]
}

func plyBinaryVertexLayout(properties []plyProperty) (map[string]int, int, bool) {
	offsets := map[string]int{}
	offset := 0
	for _, prop := range properties {
		if prop.IsList {
			return nil, 0, false
		}
		size := plyScalarSize(prop.TypeName)
		if size <= 0 {
			return nil, 0, false
		}
		offsets[prop.Name] = offset
		offset += size
	}
	return offsets, offset, true
}

func plyPropertyType(properties []plyProperty, name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, prop := range properties {
		if prop.Name == normalized {
			return prop.TypeName
		}
	}
	return ""
}

func readPLYBinaryFloat(data []byte, typeName string) (float64, bool) {
	switch strings.ToLower(typeName) {
	case "float", "float32":
		if len(data) < 4 {
			return 0, false
		}
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(data[:4]))), true
	case "double", "float64":
		if len(data) < 8 {
			return 0, false
		}
		return math.Float64frombits(binary.LittleEndian.Uint64(data[:8])), true
	default:
		return 0, false
	}
}

func plyScalarSize(typeName string) int {
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "char", "uchar", "int8", "uint8":
		return 1
	case "short", "ushort", "int16", "uint16":
		return 2
	case "int", "uint", "float", "int32", "uint32", "float32":
		return 4
	case "double", "int64", "uint64", "float64":
		return 8
	default:
		return 0
	}
}

type plyBounds struct {
	minX float64
	minY float64
	minZ float64
	maxX float64
	maxY float64
	maxZ float64
	seen bool
}

func (b *plyBounds) add(x, y, z float64) {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsNaN(z) ||
		math.IsInf(x, 0) || math.IsInf(y, 0) || math.IsInf(z, 0) {
		return
	}
	if !b.seen {
		b.minX, b.maxX = x, x
		b.minY, b.maxY = y, y
		b.minZ, b.maxZ = z, z
		b.seen = true
		return
	}
	if x < b.minX {
		b.minX = x
	}
	if y < b.minY {
		b.minY = y
	}
	if z < b.minZ {
		b.minZ = z
	}
	if x > b.maxX {
		b.maxX = x
	}
	if y > b.maxY {
		b.maxY = y
	}
	if z > b.maxZ {
		b.maxZ = z
	}
}

func (b *plyBounds) result() *datatype.Bounds3D {
	if b == nil || !b.seen {
		return nil
	}
	return &datatype.Bounds3D{
		MinX: float64Ptr(b.minX),
		MinY: float64Ptr(b.minY),
		MinZ: float64Ptr(b.minZ),
		MaxX: float64Ptr(b.maxX),
		MaxY: float64Ptr(b.maxY),
		MaxZ: float64Ptr(b.maxZ),
	}
}

func plyLooksLikeCompressedGaussianSplat(header *plyHeader, vertexProps map[string]bool) bool {
	if header == nil || header.FaceCount > 0 || header.VertexCount <= 0 {
		return false
	}
	if header.ElementCounts["chunk"] <= 0 {
		return false
	}
	return vertexProps["packed_position"] &&
		vertexProps["packed_rotation"] &&
		vertexProps["packed_scale"] &&
		vertexProps["packed_color"]
}

func plyLayout(header *plyHeader) string {
	if header == nil {
		return ""
	}
	if header.FaceCount > 0 {
		return "mesh"
	}
	if header.IsGaussianSplat {
		return "gaussian_splat"
	}
	return "point_cloud"
}

func plyHasColor(props map[string]bool) bool {
	return (props["red"] && props["green"] && props["blue"]) || (props["r"] && props["g"] && props["b"])
}

func plyHasSphericalHarmonics(props map[string]bool) bool {
	return (props["f_dc_0"] && props["f_dc_1"] && props["f_dc_2"]) || props["f_rest_0"]
}

func plySphericalHarmonicDegree(props map[string]bool) int {
	restCount := 0
	for name := range props {
		if strings.HasPrefix(name, "f_rest_") {
			restCount++
		}
	}
	if restCount == 0 && !(props["f_dc_0"] && props["f_dc_1"] && props["f_dc_2"]) {
		return -1
	}
	if restCount >= 45 {
		return 3
	}
	if restCount >= 24 {
		return 2
	}
	if restCount >= 9 {
		return 1
	}
	return 0
}

func int64Ptr(value int64) *int64 {
	if value < 0 {
		return nil
	}
	return &value
}

func intPtr(value int) *int {
	if value < 0 {
		return nil
	}
	return &value
}

func float64Ptr(value float64) *float64 {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}
