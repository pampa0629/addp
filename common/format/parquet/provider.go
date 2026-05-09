package parquet

import (
	"context"
	"fmt"
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
	return format.FormatParquet
}

func (p tableProvider) Capabilities() format.FormatCapability {
	capability, _ := format.GetFormatCapability(format.FormatParquet)
	return capability
}

func (p tableProvider) DescribeTable(ctx context.Context, input io.Reader, options *format.ParseOptions) (*format.TableInfo, error) {
	return p.parser.ParseTableInfo(ctx, input, options)
}

func (p tableProvider) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	return p.parser.ReadPreview(ctx, input, offset, limit, options)
}

func (p tableProvider) DescribeTableScope(ctx context.Context, reader resource.ResourceReader, scope resource.ResourceRef, options *format.ParseOptions) (*format.TableInfo, error) {
	input, err := openFirstParquet(ctx, reader, scope)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	return p.DescribeTable(ctx, input, options)
}

func (p tableProvider) SampleTableScope(ctx context.Context, reader resource.ResourceReader, scope resource.ResourceRef, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	input, err := openFirstParquet(ctx, reader, scope)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	return p.SampleTable(ctx, input, offset, limit, options)
}

func openFirstParquet(ctx context.Context, reader resource.ResourceReader, scope resource.ResourceRef) (io.ReadCloser, error) {
	ref, err := resource.FirstResourceByExtension(ctx, reader, scope, ".parquet")
	if err != nil {
		return nil, fmt.Errorf("failed to find parquet file in scope %s: %w", scope.Path, err)
	}
	input, err := reader.Open(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("failed to open parquet file %s: %w", ref.Path, err)
	}
	return input, nil
}
