package glb

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const (
	glbMagic           = "glTF"
	glbJSONChunkType   = 0x4E4F534A
	maxGLBJSONChunkLen = 16 * 1024 * 1024
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register GLB format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatGLB
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-glb",
		Format:   format.FormatGLB,
		I18nKey:  "format.glb",
		DataType: datatype.Model3D,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions:        []string{".glb"},
			MimeTypes:         []string{"model/gltf-binary", "application/octet-stream"},
			ContentSignatures: []string{"hex:676c5446"},
		},
	}
}

func (p *Plugin) SniffFormat(peek []byte) bool {
	return len(peek) >= 4 && string(peek[:4]) == glbMagic
}

func (p *Plugin) DescribeModel3D(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	doc, version, length, err := readGLBJSON(input)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	modelInfo := format.BuildGLTFModel3DInfo(doc)
	formatInfo := buildGLBFormatInfo(doc, version, length)
	return &format.Model3DDescribeResult{
		Model3D:    modelInfo,
		FormatInfo: formatInfo,
	}, nil
}

func readGLBJSON(input io.Reader) (*format.GLTFDocument, uint32, uint32, error) {
	header := make([]byte, 12)
	if _, err := io.ReadFull(input, header); err != nil {
		return nil, 0, 0, fmt.Errorf("read GLB header: %w", err)
	}
	if string(header[:4]) != glbMagic {
		return nil, 0, 0, fmt.Errorf("invalid GLB magic")
	}
	version := binary.LittleEndian.Uint32(header[4:8])
	length := binary.LittleEndian.Uint32(header[8:12])
	chunkHeader := make([]byte, 8)
	if _, err := io.ReadFull(input, chunkHeader); err != nil {
		return nil, 0, 0, fmt.Errorf("read GLB JSON chunk header: %w", err)
	}
	chunkLen := binary.LittleEndian.Uint32(chunkHeader[:4])
	chunkType := binary.LittleEndian.Uint32(chunkHeader[4:8])
	if chunkType != glbJSONChunkType {
		return nil, 0, 0, fmt.Errorf("GLB first chunk is not JSON")
	}
	if chunkLen > maxGLBJSONChunkLen {
		return nil, 0, 0, fmt.Errorf("GLB JSON chunk too large: %d", chunkLen)
	}
	chunk := make([]byte, int(chunkLen))
	if _, err := io.ReadFull(input, chunk); err != nil {
		return nil, 0, 0, fmt.Errorf("read GLB JSON chunk: %w", err)
	}
	chunk = bytes.TrimRight(chunk, " \t\r\n\x00")
	var doc format.GLTFDocument
	if err := json.Unmarshal(chunk, &doc); err != nil {
		return nil, 0, 0, fmt.Errorf("parse GLB JSON chunk: %w", err)
	}
	return &doc, version, length, nil
}

func buildGLBFormatInfo(doc *format.GLTFDocument, version, length uint32) map[string]interface{} {
	info := format.BuildGLTFFormatInfo(doc)
	info["glb_version"] = version
	info["byte_length"] = length
	return info
}
