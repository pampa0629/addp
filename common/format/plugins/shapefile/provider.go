package shapefile

import (
	"context"
	"io"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
)

type tableProvider struct {
	parser *Parser
}

func newTableProvider(parser *Parser) format.TableProvider {
	return tableProvider{parser: parser}
}

func (p tableProvider) Format() format.FormatType {
	return format.FormatShapefile
}

func (p tableProvider) Capabilities() format.FormatCapability {
	capability, _ := format.GetFormatCapability(format.FormatShapefile)
	return capability
}

func (p tableProvider) DescribeTable(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableInfo, error) {
	return p.parser.ParseTableInfo(ctx, input, options)
}

func (p tableProvider) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	return p.parser.SampleTable(ctx, input, offset, limit, options)
}

func (p tableProvider) DescribeTableComponents(ctx context.Context, components resource.ComponentReader, options *format.ParseOptions) (*format.TableInfo, error) {
	return p.parser.DescribeTableComponents(ctx, components, options)
}

func (p tableProvider) SampleTableComponents(ctx context.Context, components resource.ComponentReader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	return p.parser.SampleTableComponents(ctx, components, offset, limit, options)
}
