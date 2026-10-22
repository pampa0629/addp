package dae

import (
	"bufio"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

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
func (*Plugin) Format() format.FormatType { return format.FormatDAE }
func (*Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{ID: "builtin-dae", Format: format.FormatDAE, I18nKey: "format.dae", DataType: datatype.Model3D, Layouts: []string{format.LayoutSingle}, Identification: format.FormatIdentification{Extensions: []string{".dae"}, MimeTypes: []string{"model/vnd.collada+xml"}}}
}

func (p *Plugin) DescribeModel3D(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	return p.describe(ctx, input, false)
}

// ReadModel3DComposition consumes exactly one caller-bound primary XML file.
// It resolves no dependencies and publishes no partial declaration set.
func (p *Plugin) ReadModel3DComposition(ctx context.Context, input io.ReaderAt, size int64) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input == nil || size <= 0 || size > MaxSummaryBytes {
		return nil, fmt.Errorf("DAE composition size is outside its budget")
	}
	section := io.NewSectionReader(input, 0, size)
	result, err := p.describe(ctx, bufio.NewReaderSize(section, 64<<10), true)
	if err != nil {
		return nil, err
	}
	consumed, err := section.Seek(0, io.SeekCurrent)
	if err != nil || consumed != size {
		return nil, io.ErrUnexpectedEOF
	}
	return result.FormatInfo["texture_refs"].([]string), nil
}

func (*Plugin) describe(ctx context.Context, input io.Reader, composition bool) (*format.Model3DDescribeResult, error) {
	limited := &io.LimitedReader{R: input, N: MaxSummaryBytes + 1}
	decoder := xml.NewDecoder(limited)
	var stack []string
	var version, upAxis string
	rootSeen := false
	var unitMeter *float64
	var meshes, materials, animations, controllers int64
	declarations := 0
	imageInitials := 0
	var capture string
	var text strings.Builder
	refs := []string{}
	seen := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := decoder.Token()
		if limited.N <= 0 {
			return nil, fmt.Errorf("DAE summary exceeds 64 MiB")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid DAE XML: %w", err)
		}
		switch node := token.(type) {
		case xml.Directive:
			return nil, fmt.Errorf("DAE XML directives are unsupported")
		case xml.StartElement:
			if capture != "" {
				return nil, fmt.Errorf("nested DAE image URI or axis")
			}
			if len(stack) == 0 {
				if rootSeen {
					return nil, fmt.Errorf("multiple Collada roots")
				}
				rootSeen = true
				if node.Name.Local != "COLLADA" || node.Name.Space != "http://www.collada.org/2005/11/COLLADASchema" {
					return nil, fmt.Errorf("invalid Collada root")
				}
				for _, attr := range node.Attr {
					if attr.Name.Local == "version" {
						version = attr.Value
					}
				}
				if version != "1.4.0" && version != "1.4.1" {
					return nil, fmt.Errorf("unsupported Collada version %q", version)
				}
			}
			if len(stack) > 64 {
				return nil, fmt.Errorf("Collada nesting exceeds limit")
			}
			if node.Name.Space != "http://www.collada.org/2005/11/COLLADASchema" {
				return nil, fmt.Errorf("unsupported foreign Collada element")
			}
			if composition {
				for _, attr := range node.Attr {
					if (attr.Name.Local == "url" || attr.Name.Local == "source") && attr.Value != "" && !strings.HasPrefix(attr.Value, "#") {
						return nil, fmt.Errorf("external DAE model references are unsupported")
					}
				}
			}
			stack = append(stack, node.Name.Local)
			scope := strings.Join(stack, "/")
			if composition && scope == "COLLADA/library_images/image" {
				imageInitials = 0
			}
			switch scope {
			case "COLLADA/asset/unit":
				for _, attr := range node.Attr {
					if attr.Name.Local == "meter" {
						v, err := strconv.ParseFloat(attr.Value, 64)
						if err != nil || v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
							return nil, fmt.Errorf("invalid Collada unit meter")
						}
						unitMeter = &v
					}
				}
			case "COLLADA/asset/up_axis", "COLLADA/library_images/image/init_from":
				capture = scope
				text.Reset()
			case "COLLADA/library_geometries/geometry/mesh":
				meshes++
			case "COLLADA/library_materials/material":
				materials++
			case "COLLADA/library_animations/animation":
				animations++
			case "COLLADA/library_controllers/controller":
				controllers++
			}
		case xml.CharData:
			if capture != "" {
				if text.Len()+len(node) > 4096 {
					return nil, fmt.Errorf("DAE URI or axis exceeds its budget")
				}
				text.Write(node)
			} else if len(stack) == 0 && strings.TrimSpace(string(node)) != "" {
				return nil, fmt.Errorf("text outside Collada root")
			}
		case xml.EndElement:
			if capture != "" {
				value := strings.TrimSpace(text.String())
				if capture == "COLLADA/asset/up_axis" {
					if value != "X_UP" && value != "Y_UP" && value != "Z_UP" {
						return nil, fmt.Errorf("invalid Collada up_axis")
					}
					upAxis = value
				} else {
					imageInitials++
					declarations++
					if composition && (value == "" || imageInitials != 1 || declarations > 200) {
						return nil, fmt.Errorf("invalid or excessive DAE image declarations")
					}
					if value != "" && !seen[value] {
						refs = append(refs, value)
						seen[value] = true
					}
				}
				capture = ""
			}
			if composition && strings.Join(stack, "/") == "COLLADA/library_images/image" && imageInitials != 1 {
				return nil, fmt.Errorf("DAE image requires one init_from URI")
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if version == "" || len(stack) != 0 {
		return nil, fmt.Errorf("incomplete Collada document")
	}
	info := map[string]interface{}{"version": version, "texture_refs": refs, "mesh_count": meshes, "material_count": materials, "animation_count": animations, "controller_count": controllers, "scan_complete": true}
	if unitMeter != nil {
		info["unit_meter"] = *unitMeter
	}
	if upAxis != "" {
		info["up_axis"] = upAxis
	}
	if composition {
		return &format.Model3DDescribeResult{FormatInfo: map[string]interface{}{"texture_refs": refs, "scan_complete": true}}, nil
	}
	return &format.Model3DDescribeResult{Model3D: &datatype.Model3DInfo{ModelKind: datatype.Model3DKindMeshScene, MeshCount: &meshes, MaterialCount: &materials}, FormatInfo: info}, nil
}
