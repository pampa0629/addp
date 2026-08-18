package xml

import (
	"context"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const defaultTextReadLimit int64 = 16 * 1024

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatXML
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-xml",
		Format:   format.FormatXML,
		I18nKey:  "format.xml",
		DataType: datatype.Document,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".xml"},
			MimeTypes:  []string{"application/xml", "text/xml"},
		},
	}
}

func (p *Plugin) DescribeDocument(context.Context, io.Reader, *format.ParseOptions) (*datatype.DocumentInfo, error) {
	return &datatype.DocumentInfo{Encoding: "utf-8"}, nil
}

func (p *Plugin) ReadDocumentText(ctx context.Context, input io.Reader, limit int64, _ *format.ParseOptions) (string, bool, error) {
	if limit <= 0 {
		limit = defaultTextReadLimit
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	data, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil {
		return "", false, err
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}
	if !utf8.Valid(data) {
		data = []byte(string(data))
	}
	return strings.TrimPrefix(string(data), "\ufeff"), truncated, nil
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
