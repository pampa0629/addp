package stl

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

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const (
	maxSTLAsciiLines      = 4_000_000
	maxSTLBinaryTriangles = 200_000
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register STL format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatSTL
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-stl",
		Format:   format.FormatSTL,
		I18nKey:  "format.stl",
		DataType: datatype.Model3D,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".stl"},
			MimeTypes:  []string{"model/stl", "application/sla", "application/vnd.ms-pki.stl"},
		},
	}
}

func (p *Plugin) DescribeModel3D(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	summary, err := scanSTL(ctx, input)
	if err != nil {
		return nil, err
	}
	meshCount := int64(1)
	model := &datatype.Model3DInfo{
		ModelKind:     datatype.Model3DKindMeshScene,
		MeshCount:     &meshCount,
		VertexCount:   int64Ptr(summary.vertexCount),
		TriangleCount: int64Ptr(summary.triangleCount),
		Bounds3D:      summary.bounds,
	}
	formatInfo := map[string]interface{}{
		"encoding":       summary.encoding,
		"vertex_count":   summary.vertexCount,
		"triangle_count": summary.triangleCount,
		"scan_complete":  summary.scanComplete,
	}
	if summary.scannedLineCount > 0 {
		formatInfo["scanned_line_count"] = summary.scannedLineCount
	}
	if summary.scannedTriangleCount > 0 {
		formatInfo["scanned_triangle_count"] = summary.scannedTriangleCount
	}
	return &format.Model3DDescribeResult{
		Model3D:    model,
		FormatInfo: formatInfo,
	}, nil
}

type stlSummary struct {
	encoding             string
	vertexCount          int64
	triangleCount        int64
	bounds               *datatype.Bounds3D
	scanComplete         bool
	scannedLineCount     int64
	scannedTriangleCount int64
}

func scanSTL(ctx context.Context, input io.Reader) (stlSummary, error) {
	reader := bufio.NewReader(input)
	peek, _ := reader.Peek(512)
	lowerPeek := bytes.ToLower(bytes.TrimSpace(peek))
	if bytes.HasPrefix(lowerPeek, []byte("solid")) && bytes.Contains(lowerPeek, []byte("facet")) {
		return scanASCIISTL(ctx, reader)
	}
	return scanBinarySTL(ctx, reader)
}

func scanASCIISTL(ctx context.Context, input io.Reader) (stlSummary, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	summary := stlSummary{encoding: "ascii", scanComplete: true}
	var bounds boundsAccumulator
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		if lineCount > maxSTLAsciiLines {
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
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "facet":
			summary.triangleCount++
		case "vertex":
			if len(fields) < 4 {
				continue
			}
			x, okX := parseFloat(fields[1])
			y, okY := parseFloat(fields[2])
			z, okZ := parseFloat(fields[3])
			if okX && okY && okZ {
				bounds.add(x, y, z)
				summary.vertexCount++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return summary, err
	}
	summary.scannedLineCount = int64(minInt(lineCount, maxSTLAsciiLines))
	if summary.triangleCount == 0 && summary.vertexCount > 0 {
		summary.triangleCount = summary.vertexCount / 3
	}
	summary.bounds = bounds.result()
	return summary, nil
}

func scanBinarySTL(ctx context.Context, input io.Reader) (stlSummary, error) {
	header := make([]byte, 84)
	if _, err := io.ReadFull(input, header); err != nil {
		return stlSummary{}, fmt.Errorf("read STL binary header: %w", err)
	}
	triangleCount := int64(binary.LittleEndian.Uint32(header[80:84]))
	summary := stlSummary{
		encoding:      "binary",
		vertexCount:   triangleCount * 3,
		triangleCount: triangleCount,
		scanComplete:  triangleCount <= maxSTLBinaryTriangles,
	}
	var bounds boundsAccumulator
	record := make([]byte, 50)
	trianglesToScan := triangleCount
	if trianglesToScan > maxSTLBinaryTriangles {
		trianglesToScan = maxSTLBinaryTriangles
	}
	for i := int64(0); i < trianglesToScan; i++ {
		if i%4096 == 0 {
			select {
			case <-ctx.Done():
				return summary, ctx.Err()
			default:
			}
		}
		if _, err := io.ReadFull(input, record); err != nil {
			return summary, fmt.Errorf("read STL triangle record %d: %w", i, err)
		}
		for vertex := 0; vertex < 3; vertex++ {
			offset := 12 + vertex*12
			x := float64(math.Float32frombits(binary.LittleEndian.Uint32(record[offset : offset+4])))
			y := float64(math.Float32frombits(binary.LittleEndian.Uint32(record[offset+4 : offset+8])))
			z := float64(math.Float32frombits(binary.LittleEndian.Uint32(record[offset+8 : offset+12])))
			bounds.add(x, y, z)
		}
	}
	summary.scannedTriangleCount = trianglesToScan
	summary.bounds = bounds.result()
	return summary, nil
}

func parseFloat(value string) (float64, bool) {
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
