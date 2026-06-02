package unknown

import (
	"context"
	"io"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

type Plugin struct {
	reader format.BinaryContentReader
}

func NewPlugin() *Plugin {
	return &Plugin{reader: format.NewBinaryContentReader()}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatUnknown
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-unknown",
		Format:   format.FormatUnknown,
		I18nKey:  "format.unknown",
		DataType: datatype.DataTypeUnknown,
		Layouts:  []string{format.LayoutSingle},
		ContentReaders: []string{
			string(format.ContentReaderBinaryContent),
		},
	}
}

func (p *Plugin) ReadBinaryContent(ctx context.Context, input io.Reader, limit int64, options *format.ParseOptions) (*format.BinaryContent, error) {
	return p.reader.ReadBinaryContent(ctx, input, limit, options)
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
