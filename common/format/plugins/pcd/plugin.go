package pcd

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

const maxHeaderLines = 128

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register PCD format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatPCD
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-pcd",
		Format:   format.FormatPCD,
		I18nKey:  "format.pcd",
		DataType: datatype.PointCloud,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions:        []string{".pcd"},
			MimeTypes:         []string{"application/vnd.pointcloud.pcd"},
			ContentSignatures: []string{"# .pcd"},
		},
	}
}

func (p *Plugin) SniffFormat(peek []byte) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(peek[:minInt(len(peek), 64)]))), "# .pcd")
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
	return &format.PointCloudDescribeResult{
		PointCloud: buildPointCloudInfo(header),
		FormatInfo: buildFormatInfo(header),
	}, nil
}

type header struct {
	Version     string
	Fields      []string
	Sizes       []int
	Types       []string
	Counts      []int
	Width       int
	Height      int
	Viewpoint   []float64
	Points      int64
	Data        string
	HeaderLines int
}

func readHeader(input io.Reader) (*header, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	result := &header{}
	for scanner.Scan() {
		result.HeaderLines++
		line := strings.TrimSpace(scanner.Text())
		if result.HeaderLines > maxHeaderLines {
			return nil, fmt.Errorf("PCD header exceeds %d lines", maxHeaderLines)
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, rest, ok := strings.Cut(line, " ")
		if !ok {
			key, rest, ok = strings.Cut(line, "\t")
		}
		key = strings.ToUpper(strings.TrimSpace(key))
		rest = strings.TrimSpace(rest)
		switch key {
		case "VERSION":
			result.Version = rest
		case "FIELDS":
			result.Fields = splitWords(rest)
		case "SIZE":
			result.Sizes = parseInts(splitWords(rest))
		case "TYPE":
			result.Types = splitWords(rest)
		case "COUNT":
			result.Counts = parseInts(splitWords(rest))
		case "WIDTH":
			result.Width = parseInt(rest)
		case "HEIGHT":
			result.Height = parseInt(rest)
		case "VIEWPOINT":
			result.Viewpoint = parseFloats(splitWords(rest))
		case "POINTS":
			result.Points = parseInt64(rest)
		case "DATA":
			result.Data = strings.ToLower(rest)
			return result, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read PCD header: %w", err)
	}
	if result.Data == "" {
		return nil, fmt.Errorf("PCD header missing DATA line")
	}
	return result, nil
}

func buildPointCloudInfo(header *header) *datatype.PointCloudInfo {
	if header == nil {
		return nil
	}
	pointCount := header.Points
	if pointCount <= 0 && header.Width > 0 && header.Height > 0 {
		pointCount = int64(header.Width * header.Height)
	}
	dimensions := dimensionsFromFields(header.Fields)
	dimensionCount := len(dimensions)
	hasColor := optionalTrue(hasAnyField(header.Fields, "rgb", "rgba", "red", "green", "blue"))
	hasIntensity := optionalTrue(hasAnyField(header.Fields, "intensity"))
	return datatype.NormalizePointCloudInfo(&datatype.PointCloudInfo{
		PointCloudKind: datatype.PointCloudKindRawPointCloud,
		PointCount:     optionalInt64(pointCount),
		PointFormat:    "pcd_" + header.Data,
		DimensionCount: optionalInt(dimensionCount),
		Dimensions:     dimensions,
		HasColor:       hasColor,
		HasIntensity:   hasIntensity,
	})
}

func buildFormatInfo(header *header) map[string]interface{} {
	if header == nil {
		return nil
	}
	info := map[string]interface{}{
		"header_line_count": header.HeaderLines,
	}
	if header.Version != "" {
		info["version"] = header.Version
	}
	if len(header.Fields) > 0 {
		info["fields"] = header.Fields
	}
	if len(header.Sizes) > 0 {
		info["size"] = header.Sizes
	}
	if len(header.Types) > 0 {
		info["type"] = header.Types
	}
	if len(header.Counts) > 0 {
		info["count"] = header.Counts
	}
	if header.Width > 0 {
		info["width"] = header.Width
	}
	if header.Height > 0 {
		info["height"] = header.Height
	}
	if len(header.Viewpoint) > 0 {
		info["viewpoint"] = header.Viewpoint
	}
	if header.Points > 0 {
		info["points"] = header.Points
	}
	if header.Data != "" {
		info["data"] = header.Data
	}
	return info
}

func dimensionsFromFields(fields []string) []string {
	output := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.ToLower(strings.TrimSpace(field))
		if field == "" {
			continue
		}
		switch field {
		case "rgb", "rgba":
			output = append(output, field)
		default:
			output = append(output, field)
		}
	}
	return output
}

func hasAnyField(fields []string, candidates ...string) bool {
	lookup := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		lookup[strings.ToLower(strings.TrimSpace(field))] = struct{}{}
	}
	for _, candidate := range candidates {
		if _, ok := lookup[candidate]; ok {
			return true
		}
	}
	return false
}

func splitWords(value string) []string {
	return strings.Fields(strings.TrimSpace(value))
}

func parseInts(values []string) []int {
	output := make([]int, 0, len(values))
	for _, value := range values {
		output = append(output, parseInt(value))
	}
	return output
}

func parseFloats(values []string) []float64 {
	output := make([]float64, 0, len(values))
	for _, value := range values {
		parsed, err := strconv.ParseFloat(value, 64)
		if err == nil {
			output = append(output, parsed)
		}
	}
	return output
}

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
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

func optionalTrue(value bool) *bool {
	if !value {
		return nil
	}
	return &value
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
