package xyz

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const (
	maxScanLines = 10000
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register XYZ format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatXYZ
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-xyz",
		Format:   format.FormatXYZ,
		I18nKey:  "format.xyz",
		DataType: datatype.PointCloud,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".xyz"},
		},
	}
}

func (p *Plugin) DescribePointCloud(ctx context.Context, input format.PointCloudDescribeInput, options *format.ParseOptions) (*format.PointCloudDescribeResult, error) {
	summary, err := scanXYZ(ctx, input.Reader)
	if err != nil {
		return nil, err
	}
	return &format.PointCloudDescribeResult{
		PointCloud: buildPointCloudInfo(summary),
		Spatial:    buildSpatialInfo(summary),
		FormatInfo: buildFormatInfo(summary),
	}, nil
}

type scanSummary struct {
	ValidPointCount int64
	ScannedLines    int
	ColumnCount     int
	Delimiter       string
	ScanComplete    bool
	Bounds          *datatype.Bounds3D
}

func scanXYZ(ctx context.Context, input io.Reader) (*scanSummary, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	summary := &scanSummary{ScanComplete: true}
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		summary.ScannedLines++
		if summary.ScannedLines > maxScanLines {
			summary.ScanComplete = false
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		values, delimiter := splitPointLine(line)
		if len(values) < 3 {
			continue
		}
		x, okX := parseFloat(values[0])
		y, okY := parseFloat(values[1])
		z, okZ := parseFloat(values[2])
		if !okX || !okY || !okZ {
			continue
		}
		summary.ValidPointCount++
		if summary.ColumnCount == 0 || len(values) > summary.ColumnCount {
			summary.ColumnCount = len(values)
		}
		if summary.Delimiter == "" {
			summary.Delimiter = delimiter
		}
		expandBounds(summary, x, y, z)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan XYZ point cloud: %w", err)
	}
	if summary.ValidPointCount == 0 {
		return nil, fmt.Errorf("XYZ point cloud has no valid x/y/z rows")
	}
	return summary, nil
}

func buildPointCloudInfo(summary *scanSummary) *datatype.PointCloudInfo {
	if summary == nil {
		return nil
	}
	dimensions := []string{"x", "y", "z"}
	dimensionCount := len(dimensions)
	info := &datatype.PointCloudInfo{
		PointCloudKind: datatype.PointCloudKindRawPointCloud,
		DimensionCount: &dimensionCount,
		Dimensions:     dimensions,
	}
	if summary.ScanComplete {
		info.PointCount = &summary.ValidPointCount
		info.Bounds3D = summary.Bounds
	}
	return datatype.NormalizePointCloudInfo(info)
}

func buildSpatialInfo(summary *scanSummary) *datatype.SpatialInfo {
	if summary == nil || !summary.ScanComplete || summary.Bounds == nil ||
		summary.Bounds.MinX == nil || summary.Bounds.MinY == nil ||
		summary.Bounds.MaxX == nil || summary.Bounds.MaxY == nil {
		return nil
	}
	extent := datatype.NewBoundingBox(*summary.Bounds.MinX, *summary.Bounds.MinY, *summary.Bounds.MaxX, *summary.Bounds.MaxY)
	return &datatype.SpatialInfo{Extent: &extent}
}

func buildFormatInfo(summary *scanSummary) map[string]interface{} {
	if summary == nil {
		return nil
	}
	info := map[string]interface{}{
		"sampled_line_count": summary.ScannedLines,
		"valid_point_count":  summary.ValidPointCount,
		"scan_complete":      summary.ScanComplete,
		"comment_prefix":     "#",
	}
	if summary.ColumnCount > 0 {
		info["column_count"] = summary.ColumnCount
	}
	if summary.Delimiter != "" {
		info["delimiter"] = summary.Delimiter
	}
	return info
}

func splitPointLine(line string) ([]string, string) {
	if strings.Contains(line, ",") {
		parts := strings.Split(line, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts, "comma"
	}
	return strings.Fields(line), "whitespace"
}

func parseFloat(value string) (float64, bool) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return parsed, err == nil
}

func expandBounds(summary *scanSummary, x, y, z float64) {
	if summary.Bounds == nil {
		summary.Bounds = &datatype.Bounds3D{
			MinX: &x,
			MinY: &y,
			MinZ: &z,
			MaxX: &x,
			MaxY: &y,
			MaxZ: &z,
		}
		return
	}
	if x < *summary.Bounds.MinX {
		summary.Bounds.MinX = &x
	}
	if y < *summary.Bounds.MinY {
		summary.Bounds.MinY = &y
	}
	if z < *summary.Bounds.MinZ {
		summary.Bounds.MinZ = &z
	}
	if x > *summary.Bounds.MaxX {
		summary.Bounds.MaxX = &x
	}
	if y > *summary.Bounds.MaxY {
		summary.Bounds.MaxY = &y
	}
	if z > *summary.Bounds.MaxZ {
		summary.Bounds.MaxZ = &z
	}
}
