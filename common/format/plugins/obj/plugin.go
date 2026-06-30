package obj

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

const maxOBJScanLines = 2_000_000

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register OBJ format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatOBJ
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-obj",
		Format:   format.FormatOBJ,
		I18nKey:  "format.obj",
		DataType: datatype.Model3D,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".obj"},
			MimeTypes:  []string{"model/obj"},
		},
	}
}

func (p *Plugin) DescribeModel3D(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	summary, err := scanOBJ(ctx, input)
	if err != nil {
		return nil, err
	}
	meshCount := int64(1)
	if summary.objectCount > 0 || summary.groupCount > 0 {
		meshCount = maxInt64(1, summary.objectCount+summary.groupCount)
	}
	model := &datatype.Model3DInfo{
		ModelKind:     datatype.Model3DKindMeshScene,
		MeshCount:     &meshCount,
		VertexCount:   int64Ptr(summary.vertexCount),
		TriangleCount: int64Ptr(summary.triangleCount),
		Bounds3D:      summary.bounds,
	}
	formatInfo := map[string]interface{}{
		"vertex_count":           summary.vertexCount,
		"face_count":             summary.faceCount,
		"triangle_count":         summary.triangleCount,
		"object_count":           summary.objectCount,
		"group_count":            summary.groupCount,
		"material_library_count": summary.materialLibraryCount,
		"uses_material":          summary.usesMaterial,
		"scan_complete":          summary.scanComplete,
		"scanned_line_count":     summary.scannedLineCount,
	}
	if summary.declaredVertexCount != nil {
		formatInfo["declared_vertex_count"] = *summary.declaredVertexCount
	}
	if summary.declaredFaceCount != nil {
		formatInfo["declared_face_count"] = *summary.declaredFaceCount
	}
	return &format.Model3DDescribeResult{
		Model3D:    model,
		FormatInfo: formatInfo,
	}, nil
}

type objSummary struct {
	vertexCount          int64
	faceCount            int64
	triangleCount        int64
	objectCount          int64
	groupCount           int64
	materialLibraryCount int64
	usesMaterial         bool
	bounds               *datatype.Bounds3D
	declaredVertexCount  *int64
	declaredFaceCount    *int64
	scannedLineCount     int64
	scanComplete         bool
}

func scanOBJ(ctx context.Context, input io.Reader) (objSummary, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	summary := objSummary{scanComplete: true}
	var bounds boundsAccumulator
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		if lineCount > maxOBJScanLines {
			summary.scanComplete = false
			break
		}
		if lineCount%4096 == 0 {
			select {
			case <-ctx.Done():
				return summary, ctx.Err()
			default:
			}
		}
		line := scanner.Text()
		if parseOBJCommentFacts(line, &summary) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "v":
			if len(fields) < 4 {
				continue
			}
			x, okX := parseOBJFloat(fields[1])
			y, okY := parseOBJFloat(fields[2])
			z, okZ := parseOBJFloat(fields[3])
			if okX && okY && okZ {
				bounds.add(x, y, z)
			}
			if summary.declaredVertexCount == nil {
				summary.vertexCount++
			}
		case "f":
			if len(fields) >= 4 {
				if summary.declaredFaceCount == nil {
					summary.faceCount++
					summary.triangleCount += int64(len(fields) - 3)
				}
			}
		case "o":
			summary.objectCount++
		case "g":
			summary.groupCount++
		case "mtllib":
			if len(fields) > 1 {
				summary.materialLibraryCount++
			}
		case "usemtl":
			if len(fields) > 1 {
				summary.usesMaterial = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return summary, err
	}
	summary.scannedLineCount = int64(minInt(lineCount, maxOBJScanLines))
	if summary.bounds == nil {
		summary.bounds = bounds.result()
	}
	return summary, nil
}

func parseOBJCommentFacts(line string, summary *objSummary) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return false
	}
	comment := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
	lower := strings.ToLower(comment)
	switch {
	case strings.HasPrefix(lower, "vertices:"):
		if value, ok := parseOBJInt(strings.TrimSpace(comment[len("vertices:"):])); ok {
			summary.declaredVertexCount = int64Ptr(value)
			summary.vertexCount = value
		}
	case strings.HasPrefix(lower, "faces"):
		if index := strings.Index(comment, ":"); index >= 0 {
			if value, ok := parseOBJInt(strings.TrimSpace(comment[index+1:])); ok {
				summary.declaredFaceCount = int64Ptr(value)
				summary.faceCount = value
				summary.triangleCount = value
			}
		}
	case strings.HasPrefix(lower, "boundingbox("):
		if bounds := parseOBJBoundingBox(comment); bounds != nil {
			summary.bounds = bounds
		}
	}
	return true
}

func parseOBJBoundingBox(comment string) *datatype.Bounds3D {
	start := strings.Index(comment, "(")
	end := strings.LastIndex(comment, ")")
	if start < 0 || end <= start {
		return nil
	}
	parts := strings.Fields(comment[start+1 : end])
	if len(parts) != 6 {
		return nil
	}
	values := make([]float64, 6)
	for i, part := range parts {
		parsed, ok := parseOBJFloat(part)
		if !ok {
			return nil
		}
		values[i] = parsed
	}
	return &datatype.Bounds3D{
		MinX: float64Ptr(values[0]),
		MinY: float64Ptr(values[1]),
		MinZ: float64Ptr(values[2]),
		MaxX: float64Ptr(values[3]),
		MaxY: float64Ptr(values[4]),
		MaxZ: float64Ptr(values[5]),
	}
}

func parseOBJInt(value string) (int64, bool) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	return parsed, err == nil
}

func parseOBJFloat(value string) (float64, bool) {
	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, err == nil
}

type boundsAccumulator struct {
	initialized bool
	minX        float64
	minY        float64
	minZ        float64
	maxX        float64
	maxY        float64
	maxZ        float64
}

func (b *boundsAccumulator) add(x, y, z float64) {
	if !b.initialized {
		b.initialized = true
		b.minX, b.maxX = x, x
		b.minY, b.maxY = y, y
		b.minZ, b.maxZ = z, z
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

func (b *boundsAccumulator) result() *datatype.Bounds3D {
	if !b.initialized {
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

func int64Ptr(value int64) *int64 {
	return &value
}

func float64Ptr(value float64) *float64 {
	return &value
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
