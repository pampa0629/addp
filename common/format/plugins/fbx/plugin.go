package fbx

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

var fbxBinaryMagic = []byte("Kaydara FBX Binary  \x00\x1a\x00")

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register FBX format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatFBX
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-fbx",
		Format:   format.FormatFBX,
		I18nKey:  "format.fbx",
		DataType: datatype.Model3D,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions:        []string{".fbx"},
			MimeTypes:         []string{"application/vnd.autodesk.fbx"},
			ContentSignatures: []string{"text:Kaydara FBX Binary"},
		},
	}
}

func (p *Plugin) SniffFormat(peek []byte) bool {
	trimmed := bytes.TrimSpace(peek)
	return bytes.HasPrefix(peek, fbxBinaryMagic) ||
		bytes.HasPrefix(trimmed, []byte("; FBX")) ||
		bytes.HasPrefix(trimmed, []byte("FBXHeaderExtension"))
}

func (p *Plugin) DescribeModel3D(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.Model3DDescribeResult, error) {
	header := make([]byte, 64)
	n, err := input.Read(header)
	if err != nil && err != io.EOF {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	encoding := "ascii"
	if bytes.HasPrefix(header[:n], fbxBinaryMagic) {
		encoding = "binary"
	}
	meshCount := int64(1)
	return &format.Model3DDescribeResult{
		Model3D: &datatype.Model3DInfo{
			ModelKind: datatype.Model3DKindMeshScene,
			MeshCount: &meshCount,
		},
		FormatInfo: map[string]interface{}{
			"encoding": encoding,
		},
	}, nil
}
