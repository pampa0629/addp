package e57

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const (
	e57Magic              = "ASTM-E57"
	e57HeaderSize         = 48
	e57DefaultPageSize    = 1024
	e57PageChecksumBytes  = 4
	e57MaxXMLSeekBytes    = 16 * 1024 * 1024
	e57MaxXMLLogicalBytes = 4 * 1024 * 1024
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register E57 format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatE57
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-e57",
		Format:   format.FormatE57,
		I18nKey:  "format.e57",
		DataType: datatype.PointCloud,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions:        []string{".e57"},
			MimeTypes:         []string{"model/e57", "application/vnd.astm-e57", "application/octet-stream"},
			ContentSignatures: []string{"hex:4153544d2d453537"},
		},
	}
}

func (p *Plugin) DescribePointCloud(ctx context.Context, input format.PointCloudDescribeInput, options *format.ParseOptions) (*format.PointCloudDescribeResult, error) {
	header, err := readHeader(input.Reader)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	summary, xmlRead, err := readXMLSummary(ctx, input, header)
	if err != nil {
		return nil, err
	}
	pointCloud := buildPointCloudInfo(summary)
	formatInfo := buildFormatInfo(header, summary, xmlRead)
	return &format.PointCloudDescribeResult{
		PointCloud: pointCloud,
		Spatial:    buildSpatialInfo(pointCloud),
		FormatInfo: formatInfo,
	}, nil
}

type e57Header struct {
	VersionMajor      uint32
	VersionMinor      uint32
	PhysicalLength    uint64
	PhysicalXMLOffset uint64
	XMLLength         uint64
	PageSize          uint64
}

type e57XMLSummary struct {
	FormatName                string
	GUID                      string
	VersionMajor              *int
	VersionMinor              *int
	LibraryVersion            string
	CoordinateMetadataPresent bool
	ScanCount                 int
	ScanNames                 []string
	PointCount                int64
	Bounds                    *datatype.Bounds3D
	Dimensions                []string
	HasColor                  *bool
	HasIntensity              *bool
}

type e57ScanSummary struct {
	Name         string
	PointCount   int64
	Bounds       *datatype.Bounds3D
	Dimensions   map[string]struct{}
	HasColor     bool
	HasIntensity bool
}

func readHeader(input io.Reader) (*e57Header, error) {
	buf := make([]byte, e57HeaderSize)
	if _, err := io.ReadFull(input, buf); err != nil {
		return nil, fmt.Errorf("read E57 header: %w", err)
	}
	if string(buf[:8]) != e57Magic {
		return nil, fmt.Errorf("invalid E57 magic")
	}
	header := &e57Header{
		VersionMajor:      binary.LittleEndian.Uint32(buf[8:12]),
		VersionMinor:      binary.LittleEndian.Uint32(buf[12:16]),
		PhysicalLength:    binary.LittleEndian.Uint64(buf[16:24]),
		PhysicalXMLOffset: binary.LittleEndian.Uint64(buf[24:32]),
		XMLLength:         binary.LittleEndian.Uint64(buf[32:40]),
		PageSize:          binary.LittleEndian.Uint64(buf[40:48]),
	}
	if header.PageSize == 0 {
		header.PageSize = e57DefaultPageSize
	}
	if header.PageSize <= e57PageChecksumBytes {
		return nil, fmt.Errorf("invalid E57 page size: %d", header.PageSize)
	}
	return header, nil
}

func readXMLSummary(ctx context.Context, input format.PointCloudDescribeInput, header *e57Header) (*e57XMLSummary, bool, error) {
	if header == nil || header.PhysicalXMLOffset < e57HeaderSize || header.XMLLength == 0 {
		return nil, false, nil
	}
	if header.XMLLength > e57MaxXMLLogicalBytes {
		return nil, false, nil
	}
	xmlBytes, err := readXMLBytes(ctx, input, header)
	if err != nil {
		return nil, false, err
	}
	if len(xmlBytes) == 0 {
		return nil, false, nil
	}
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}
	summary, err := parseXMLSummary(xmlBytes)
	if err != nil {
		return nil, false, err
	}
	return summary, true, nil
}

func readXMLBytes(ctx context.Context, input format.PointCloudDescribeInput, header *e57Header) ([]byte, error) {
	if input.RangeReader != nil {
		return readXMLBytesWithRange(ctx, input.RangeReader, input.Ref, header)
	}
	if header.PhysicalXMLOffset > e57MaxXMLSeekBytes {
		return nil, nil
	}
	toDiscard := int64(header.PhysicalXMLOffset - e57HeaderSize)
	if _, err := io.CopyN(io.Discard, input.Reader, toDiscard); err != nil {
		return nil, fmt.Errorf("seek E57 XML section: %w", err)
	}
	return readLogicalPagedBytes(input.Reader, header)
}

func readXMLBytesWithRange(ctx context.Context, reader contentio.RangeReader, ref contentio.Ref, header *e57Header) ([]byte, error) {
	pageSize := int64(header.PageSize)
	payloadSize := pageSize - e57PageChecksumBytes
	remaining := int64(header.XMLLength)
	offset := int64(header.PhysicalXMLOffset)
	output := bytes.NewBuffer(make([]byte, 0, minInt64(remaining, 64*1024)))
	for remaining > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		pageOffset := offset % pageSize
		payloadRemaining := payloadSize - pageOffset
		if payloadRemaining <= 0 {
			return nil, fmt.Errorf("invalid E57 XML offset inside page checksum")
		}
		chunkSize := payloadRemaining
		if chunkSize > remaining {
			chunkSize = remaining
		}
		rc, err := reader.OpenRange(ctx, ref, offset, chunkSize)
		if err != nil {
			return nil, fmt.Errorf("read E57 XML range: %w", err)
		}
		if _, err := io.Copy(output, rc); err != nil {
			rc.Close()
			return nil, fmt.Errorf("read E57 XML range: %w", err)
		}
		if err := rc.Close(); err != nil {
			return nil, fmt.Errorf("close E57 XML range: %w", err)
		}
		remaining -= chunkSize
		offset += chunkSize
		if offset%pageSize == payloadSize {
			offset += e57PageChecksumBytes
		}
	}
	return output.Bytes(), nil
}

func readLogicalPagedBytes(input io.Reader, header *e57Header) ([]byte, error) {
	pageSize := int(header.PageSize)
	payloadSize := pageSize - e57PageChecksumBytes
	remaining := int64(header.XMLLength)
	output := bytes.NewBuffer(make([]byte, 0, minInt64(remaining, 64*1024)))
	pageOffset := int(header.PhysicalXMLOffset % header.PageSize)
	for remaining > 0 {
		payloadRemaining := payloadSize - pageOffset
		if payloadRemaining <= 0 {
			return nil, fmt.Errorf("invalid E57 XML offset inside page checksum")
		}
		chunkSize := int64(payloadRemaining)
		if chunkSize > remaining {
			chunkSize = remaining
		}
		if _, err := io.CopyN(output, input, chunkSize); err != nil {
			return nil, fmt.Errorf("read E57 XML payload: %w", err)
		}
		remaining -= chunkSize
		pageOffset += int(chunkSize)
		if pageOffset == payloadSize {
			if _, err := io.CopyN(io.Discard, input, e57PageChecksumBytes); err != nil && remaining > 0 {
				return nil, fmt.Errorf("skip E57 page checksum: %w", err)
			}
			pageOffset = 0
		}
	}
	return output.Bytes(), nil
}

func parseXMLSummary(xmlBytes []byte) (*e57XMLSummary, error) {
	decoder := xml.NewDecoder(bytes.NewReader(xmlBytes))
	summary := &e57XMLSummary{}
	var path []string
	var text strings.Builder
	var current *e57ScanSummary
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse E57 XML summary: %w", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			name := typed.Name.Local
			path = append(path, name)
			text.Reset()
			if name == "vectorChild" && parentPath(path, "data3D") {
				current = &e57ScanSummary{Dimensions: map[string]struct{}{}}
			}
			if current != nil && name == "points" {
				current.PointCount = attrInt64(typed.Attr, "recordCount")
			}
			if current != nil && isPointDimensionElement(name) {
				current.Dimensions[pointDimensionName(name)] = struct{}{}
				if isColorDimension(name) {
					current.HasColor = true
				}
				if name == "intensity" {
					current.HasIntensity = true
				}
			}
		case xml.CharData:
			text.Write([]byte(typed))
		case xml.EndElement:
			value := strings.TrimSpace(text.String())
			if current != nil {
				applyScanValue(current, path, value)
			} else {
				applyRootValue(summary, path, value)
			}
			if typed.Name.Local == "vectorChild" && current != nil && parentPath(path, "data3D") {
				mergeScanSummary(summary, current)
				current = nil
			}
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
			text.Reset()
		}
	}
	if summary.ScanCount == 0 && summary.PointCount == 0 && summary.Bounds == nil && summary.FormatName == "" && summary.LibraryVersion == "" {
		return nil, nil
	}
	return summary, nil
}

func applyRootValue(summary *e57XMLSummary, path []string, value string) {
	if summary == nil || len(path) < 2 {
		return
	}
	if path[len(path)-2] != "e57Root" {
		return
	}
	switch path[len(path)-1] {
	case "formatName":
		summary.FormatName = value
	case "guid":
		summary.GUID = value
	case "versionMajor":
		summary.VersionMajor = intPtrFromString(value)
	case "versionMinor":
		summary.VersionMinor = intPtrFromString(value)
	case "e57LibraryVersion":
		summary.LibraryVersion = value
	case "coordinateMetadata":
		summary.CoordinateMetadataPresent = value != ""
	}
}

func applyScanValue(scan *e57ScanSummary, path []string, value string) {
	if scan == nil || len(path) == 0 {
		return
	}
	switch path[len(path)-1] {
	case "name":
		if len(path) >= 3 && path[len(path)-2] == "vectorChild" {
			scan.Name = value
		}
	case "xMinimum", "xMaximum", "yMinimum", "yMaximum", "zMinimum", "zMaximum":
		if len(path) >= 2 && path[len(path)-2] == "cartesianBounds" {
			applyBoundsValue(scan, path[len(path)-1], value)
		}
	}
}

func applyBoundsValue(scan *e57ScanSummary, name, value string) {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return
	}
	if scan.Bounds == nil {
		scan.Bounds = &datatype.Bounds3D{}
	}
	switch name {
	case "xMinimum":
		scan.Bounds.MinX = &parsed
	case "xMaximum":
		scan.Bounds.MaxX = &parsed
	case "yMinimum":
		scan.Bounds.MinY = &parsed
	case "yMaximum":
		scan.Bounds.MaxY = &parsed
	case "zMinimum":
		scan.Bounds.MinZ = &parsed
	case "zMaximum":
		scan.Bounds.MaxZ = &parsed
	}
}

func mergeScanSummary(summary *e57XMLSummary, scan *e57ScanSummary) {
	if summary == nil || scan == nil {
		return
	}
	summary.ScanCount++
	if scan.Name != "" && len(summary.ScanNames) < 10 {
		summary.ScanNames = append(summary.ScanNames, scan.Name)
	}
	if scan.PointCount > 0 {
		summary.PointCount += scan.PointCount
	}
	summary.Bounds = unionBounds(summary.Bounds, scan.Bounds)
	for dimension := range scan.Dimensions {
		if !containsString(summary.Dimensions, dimension) {
			summary.Dimensions = append(summary.Dimensions, dimension)
		}
	}
	if scan.HasColor {
		value := true
		summary.HasColor = &value
	}
	if scan.HasIntensity {
		value := true
		summary.HasIntensity = &value
	}
}

func buildPointCloudInfo(summary *e57XMLSummary) *datatype.PointCloudInfo {
	pointCloud := &datatype.PointCloudInfo{
		PointCloudKind: datatype.PointCloudKindScanCollection,
	}
	if summary != nil {
		if summary.PointCount > 0 {
			pointCloud.PointCount = &summary.PointCount
		}
		if len(summary.Dimensions) > 0 {
			pointCloud.Dimensions = summary.Dimensions
			dimensionCount := len(summary.Dimensions)
			pointCloud.DimensionCount = &dimensionCount
		}
		pointCloud.Bounds3D = summary.Bounds
		pointCloud.HasColor = summary.HasColor
		pointCloud.HasIntensity = summary.HasIntensity
	}
	return datatype.NormalizePointCloudInfo(pointCloud)
}

func buildSpatialInfo(pointCloud *datatype.PointCloudInfo) *datatype.SpatialInfo {
	if pointCloud == nil || pointCloud.Bounds3D == nil ||
		pointCloud.Bounds3D.MinX == nil || pointCloud.Bounds3D.MinY == nil ||
		pointCloud.Bounds3D.MaxX == nil || pointCloud.Bounds3D.MaxY == nil {
		return nil
	}
	extent := datatype.NewBoundingBox(*pointCloud.Bounds3D.MinX, *pointCloud.Bounds3D.MinY, *pointCloud.Bounds3D.MaxX, *pointCloud.Bounds3D.MaxY)
	return &datatype.SpatialInfo{Extent: &extent}
}

func buildFormatInfo(header *e57Header, summary *e57XMLSummary, xmlRead bool) map[string]interface{} {
	info := map[string]interface{}{
		"header_version":      fmt.Sprintf("%d.%d", header.VersionMajor, header.VersionMinor),
		"physical_length":     int64(header.PhysicalLength),
		"physical_xml_offset": int64(header.PhysicalXMLOffset),
		"xml_length":          int64(header.XMLLength),
		"page_size":           int64(header.PageSize),
		"xml_read":            xmlRead,
	}
	if summary == nil {
		return info
	}
	if summary.FormatName != "" {
		info["format_name"] = summary.FormatName
	}
	if summary.GUID != "" {
		info["guid"] = summary.GUID
	}
	if summary.VersionMajor != nil {
		info["xml_version_major"] = *summary.VersionMajor
	}
	if summary.VersionMinor != nil {
		info["xml_version_minor"] = *summary.VersionMinor
	}
	if summary.LibraryVersion != "" {
		info["e57_library_version"] = summary.LibraryVersion
	}
	info["coordinate_metadata_present"] = summary.CoordinateMetadataPresent
	if summary.ScanCount > 0 {
		info["scan_count"] = summary.ScanCount
	}
	if len(summary.ScanNames) > 0 {
		info["scan_names"] = summary.ScanNames
	}
	return info
}

func parentPath(path []string, parent string) bool {
	return len(path) >= 2 && path[len(path)-2] == parent
}

func attrInt64(attrs []xml.Attr, name string) int64 {
	for _, attr := range attrs {
		if attr.Name.Local == name {
			value, _ := strconv.ParseInt(strings.TrimSpace(attr.Value), 10, 64)
			return value
		}
	}
	return 0
}

func intPtrFromString(value string) *int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	return &parsed
}

func isPointDimensionElement(name string) bool {
	switch name {
	case "cartesianX", "cartesianY", "cartesianZ", "sphericalRange", "sphericalAzimuth", "sphericalElevation", "intensity", "colorRed", "colorGreen", "colorBlue":
		return true
	default:
		return false
	}
}

func pointDimensionName(name string) string {
	switch name {
	case "cartesianX":
		return "x"
	case "cartesianY":
		return "y"
	case "cartesianZ":
		return "z"
	case "colorRed":
		return "red"
	case "colorGreen":
		return "green"
	case "colorBlue":
		return "blue"
	default:
		return strings.TrimPrefix(name, "spherical")
	}
}

func isColorDimension(name string) bool {
	return name == "colorRed" || name == "colorGreen" || name == "colorBlue"
}

func unionBounds(left, right *datatype.Bounds3D) *datatype.Bounds3D {
	if right == nil {
		return left
	}
	if left == nil {
		return right.Clone()
	}
	result := left.Clone()
	result.MinX = minFloat64Ptr(result.MinX, right.MinX)
	result.MinY = minFloat64Ptr(result.MinY, right.MinY)
	result.MinZ = minFloat64Ptr(result.MinZ, right.MinZ)
	result.MaxX = maxFloat64Ptr(result.MaxX, right.MaxX)
	result.MaxY = maxFloat64Ptr(result.MaxY, right.MaxY)
	result.MaxZ = maxFloat64Ptr(result.MaxZ, right.MaxZ)
	return result
}

func minFloat64Ptr(left, right *float64) *float64 {
	if left == nil {
		return cloneFloat64Ptr(right)
	}
	if right == nil {
		return cloneFloat64Ptr(left)
	}
	if *right < *left {
		return cloneFloat64Ptr(right)
	}
	return cloneFloat64Ptr(left)
}

func maxFloat64Ptr(left, right *float64) *float64 {
	if left == nil {
		return cloneFloat64Ptr(right)
	}
	if right == nil {
		return cloneFloat64Ptr(left)
	}
	if *right > *left {
		return cloneFloat64Ptr(right)
	}
	return cloneFloat64Ptr(left)
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func minInt64(left, right int64) int {
	if left < right {
		return int(left)
	}
	return int(right)
}
