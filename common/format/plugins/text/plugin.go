package text

import (
	"context"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/format"
)

const defaultTextReadLimit int64 = 16 * 1024

type Provider struct {
	formatType format.FormatType
}

func NewProvider(formatType format.FormatType) format.DocumentProvider {
	return Provider{formatType: formatType}
}

func (p Provider) Format() format.FormatType {
	return p.formatType
}

func (p Provider) Capabilities() format.FormatCapability {
	capability, ok := format.GetFormatCapability(p.formatType)
	if ok {
		return capability
	}
	return format.FormatCapability{
		Format:        p.formatType,
		DataType:      format.FormatDataTypeDocument,
		Layouts:       []string{format.FormatLayoutSingle},
		ProviderHints: []string{format.FormatProviderDocument},
	}
}

func (p Provider) DescribeDocument(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.DocumentInfo, error) {
	return &format.DocumentInfo{
		Format:   p.formatType,
		Encoding: "utf-8",
	}, nil
}

func (p Provider) ReadDocumentText(ctx context.Context, input io.Reader, limit int64, _ *format.ParseOptions) (string, bool, error) {
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
	if err := format.RegisterDocumentProvider(NewProvider(format.FormatText)); err != nil {
		panic(err)
	}
	if err := format.RegisterDocumentProvider(NewProvider(format.FormatMarkdown)); err != nil {
		panic(err)
	}
}
