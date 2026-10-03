package threeds

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const MaxSummaryBytes = 64 << 20

type Plugin struct{}

func NewPlugin() *Plugin { return &Plugin{} }
func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
func (*Plugin) Format() format.FormatType { return format.Format3DS }
func (*Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{ID: "builtin-3ds", Format: format.Format3DS, I18nKey: "format.3ds", DataType: datatype.Model3D, Layouts: []string{format.LayoutSingle}, Identification: format.FormatIdentification{Extensions: []string{".3ds"}, MimeTypes: []string{"image/x-3ds", "application/x-3ds"}}}
}

type summary struct {
	meshes, vertices, triangles, materials int64
	version                                *uint32
	scale                                  *float64
	animated                               bool
	refs                                   []string
	seen                                   map[string]bool
	maps                                   []uint16
}

func (*Plugin) DescribeModel3D(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	data, err := io.ReadAll(io.LimitReader(input, MaxSummaryBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxSummaryBytes {
		return nil, fmt.Errorf("3DS summary exceeds 64 MiB")
	}
	if len(data) < 6 || binary.LittleEndian.Uint16(data) != 0x4d4d || int(binary.LittleEndian.Uint32(data[2:])) != len(data) {
		return nil, fmt.Errorf("invalid 3DS main chunk")
	}
	facts := summary{refs: []string{}, seen: map[string]bool{}, maps: []uint16{}}
	if err := facts.scan(ctx, data[6:], 0, 0); err != nil {
		return nil, err
	}
	info := map[string]interface{}{"texture_refs": facts.refs, "texture_map_chunks": facts.maps, "has_animation": facts.animated, "scan_complete": true}
	if facts.version != nil {
		info["version"] = *facts.version
	}
	if facts.scale != nil {
		info["master_scale"] = *facts.scale
	}
	return &format.Model3DDescribeResult{Model3D: &datatype.Model3DInfo{ModelKind: datatype.Model3DKindMeshScene, MeshCount: &facts.meshes, VertexCount: &facts.vertices, TriangleCount: &facts.triangles, MaterialCount: &facts.materials}, FormatInfo: info}, nil
}

func (s *summary) scan(ctx context.Context, data []byte, depth int, textureMap uint16) error {
	if depth > 64 {
		return fmt.Errorf("3DS chunk nesting exceeds limit")
	}
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(data) < 6 {
			return fmt.Errorf("truncated 3DS chunk header")
		}
		id := binary.LittleEndian.Uint16(data)
		size := int(binary.LittleEndian.Uint32(data[2:]))
		if size < 6 || size > len(data) {
			return fmt.Errorf("invalid 3DS chunk length")
		}
		body := data[6:size]
		data = data[size:]
		child := false
		childMap := textureMap
		switch id {
		case 0x0002:
			if len(body) != 4 {
				return fmt.Errorf("invalid 3DS version chunk")
			}
			v := binary.LittleEndian.Uint32(body)
			s.version = &v
		case 0x0100:
			if len(body) != 4 {
				return fmt.Errorf("invalid 3DS master scale")
			}
			v := float64(math.Float32frombits(binary.LittleEndian.Uint32(body)))
			if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("invalid 3DS master scale")
			}
			s.scale = &v
		case 0x3d3d, 0xb000, 0xb002, 0xb003, 0xb004, 0xb005, 0xb006, 0xb007:
			child = true
		case 0x4000:
			n := bytes.IndexByte(body, 0)
			if n < 0 {
				return fmt.Errorf("unterminated 3DS object name")
			}
			body = body[n+1:]
			child = true
		case 0x4100:
			s.meshes++
			child = true
		case 0xafff:
			s.materials++
			child = true
		case 0x4110:
			if len(body) < 2 {
				return fmt.Errorf("truncated 3DS vertices")
			}
			count := int(binary.LittleEndian.Uint16(body))
			if len(body) != 2+count*12 {
				return fmt.Errorf("invalid 3DS vertex count")
			}
			s.vertices += int64(count)
		case 0x4120:
			if len(body) < 2 {
				return fmt.Errorf("truncated 3DS faces")
			}
			count := int(binary.LittleEndian.Uint16(body))
			n := 2 + count*8
			if len(body) < n {
				return fmt.Errorf("invalid 3DS face count")
			}
			s.triangles += int64(count)
			body = body[n:]
			child = true
		case 0xa200, 0xa204, 0xa210, 0xa220, 0xa230, 0xa33a, 0xa33c, 0xa33d, 0xa33e, 0xa340, 0xa342, 0xa344, 0xa346, 0xa348, 0xa34a:
			child = true
			childMap = id
		case 0xa300:
			n := bytes.IndexByte(body, 0)
			if n < 0 || n != len(body)-1 || !utf8.Valid(body[:n]) {
				return fmt.Errorf("invalid UTF-8 3DS texture filename")
			}
			ref := string(body[:n])
			if ref != "" && !s.seen[ref] {
				s.refs = append(s.refs, ref)
				s.seen[ref] = true
			}
			if ref != "" {
				s.maps = append(s.maps, textureMap)
			}
		case 0xb020, 0xb021, 0xb022, 0xb023, 0xb024, 0xb025, 0xb026, 0xb027, 0xb028, 0xb029:
			if len(body) < 14 {
				return fmt.Errorf("truncated 3DS keyframe track")
			}
			if binary.LittleEndian.Uint32(body[10:14]) > 1 {
				s.animated = true
			}
		}
		if child {
			if err := s.scan(ctx, body, depth+1, childMap); err != nil {
				return err
			}
		}
	}
	return nil
}
