package gltf

import (
	"context"
	"fmt"
	"io"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register glTF format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatGLTF
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-gltf",
		Format:   format.FormatGLTF,
		I18nKey:  "format.gltf",
		DataType: datatype.Model3D,
		Layouts:  []string{format.LayoutMulti},
		Identification: format.FormatIdentification{
			Extensions: []string{".gltf"},
			MimeTypes:  []string{"model/gltf+json"},
		},
	}
}

func (p *Plugin) DescribeModel3D(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	doc, err := format.DecodeGLTFManifest(input, format.MaxGLTFManifestBytes)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return &format.Model3DDescribeResult{
		Model3D:    format.BuildGLTFModel3DInfo(doc),
		FormatInfo: format.BuildGLTFFormatInfo(doc),
	}, nil
}
