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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(input, format.MaxGLTFManifestBytes+1))
	if err != nil {
		return nil, err
	}
	composition, err := format.ParseGLTFComposition(ctx, raw)
	if err != nil {
		return nil, err
	}
	doc := composition.Document
	return &format.Model3DDescribeResult{
		Model3D:    format.BuildGLTFModel3DInfo(doc),
		FormatInfo: format.BuildGLTFFormatInfo(doc),
	}, nil
}

var _ format.Model3DCompositionProvider = (*Plugin)(nil)

func (*Plugin) ReadModel3DComposition(ctx context.Context, input io.ReaderAt, size int64) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input == nil || size <= 0 || size > format.MaxGLTFManifestBytes {
		return nil, fmt.Errorf("glTF manifest size outside budget")
	}
	raw, err := io.ReadAll(io.NewSectionReader(input, 0, size))
	if err != nil || int64(len(raw)) != size {
		return nil, fmt.Errorf("glTF manifest could not be read completely")
	}
	composition, err := format.ParseGLTFComposition(ctx, raw)
	if err != nil {
		return nil, err
	}
	return composition.Paths(), nil
}
