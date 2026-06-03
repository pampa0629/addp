package text

import (
	"context"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const defaultTextReadLimit int64 = 16 * 1024

type Plugin struct {
	formatType format.FormatType
}

func NewPlugin(formatType format.FormatType) Plugin {
	return Plugin{formatType: formatType}
}

func (p Plugin) Format() format.FormatType {
	return p.formatType
}

func (p Plugin) Descriptor() format.FormatDescriptor {
	descriptor := format.FormatDescriptor{
		ID:       "builtin-" + string(p.formatType),
		Format:   p.formatType,
		DataType: datatype.Document,
		Layouts:  []string{format.LayoutSingle},
	}
	switch p.formatType {
	case format.FormatMarkdown:
		descriptor.I18nKey = "format.markdown"
		descriptor.Identification = format.FormatIdentification{
			Extensions: []string{".md", ".markdown"},
			MimeTypes:  []string{"text/markdown", "text/x-markdown"},
		}
	default:
		descriptor.I18nKey = "format.text"
		descriptor.Format = format.FormatText
		descriptor.ID = "builtin-text"
		descriptor.Identification = format.FormatIdentification{
			Extensions: []string{".txt"},
			MimeTypes:  []string{"text/plain"},
		}
	}
	return descriptor
}

func (p Plugin) DescribeDocument(ctx context.Context, input io.Reader, options *format.ParseOptions) (*datatype.DocumentInfo, error) {
	return &datatype.DocumentInfo{
		Encoding: "utf-8",
	}, nil
}

func (p Plugin) ReadDocumentText(ctx context.Context, input io.Reader, limit int64, _ *format.ParseOptions) (string, bool, error) {
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
	if err := format.RegisterFormatPlugin(NewPlugin(format.FormatText)); err != nil {
		panic(err)
	}
	if err := format.RegisterFormatPlugin(NewPlugin(format.FormatMarkdown)); err != nil {
		panic(err)
	}
}
