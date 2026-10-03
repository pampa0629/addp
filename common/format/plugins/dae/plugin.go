package dae

import (
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

func (*Plugin) DescribeModel3D(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	limited := &io.LimitedReader{R: input, N: MaxSummaryBytes + 1}
	decoder := xml.NewDecoder(limited)
	var stack []string
	var version, upAxis string
	rootSeen := false
	var unitMeter *float64
	var meshes, materials, animations, controllers int64
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
			stack = append(stack, node.Name.Local)
			scope := strings.Join(stack, "/")
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
				var value string
				if err := decoder.DecodeElement(&value, &node); err != nil {
					return nil, err
				}
				stack = stack[:len(stack)-1]
				value = strings.TrimSpace(value)
				if scope == "COLLADA/asset/up_axis" {
					if value != "X_UP" && value != "Y_UP" && value != "Z_UP" {
						return nil, fmt.Errorf("invalid Collada up_axis")
					}
					upAxis = value
				} else if value != "" && !seen[value] {
					refs = append(refs, value)
					seen[value] = true
				}
			case "COLLADA/library_geometries/geometry/mesh":
				meshes++
			case "COLLADA/library_materials/material":
				materials++
			case "COLLADA/library_animations/animation":
				animations++
			case "COLLADA/library_controllers/controller":
				controllers++
			}
		case xml.EndElement:
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
	return &format.Model3DDescribeResult{Model3D: &datatype.Model3DInfo{ModelKind: datatype.Model3DKindMeshScene, MeshCount: &meshes, MaterialCount: &materials}, FormatInfo: info}, nil
}
