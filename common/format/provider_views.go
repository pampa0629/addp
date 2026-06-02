package format

import (
	"context"
	"fmt"
	"io"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
)

type tableInfoProviderView struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (*TableDescribeResult, error)
}

type tableSampleReaderView struct {
	formatType FormatType
	sample     func(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error)
}

type formatInfoProviderView struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (map[string]interface{}, error)
}

type documentInfoProviderView struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (*datatype.DocumentInfo, error)
}

type documentTextReaderView struct {
	formatType FormatType
	extract    func(context.Context, io.Reader, int64, *ParseOptions) (string, bool, error)
}

type mediaProviderView struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (*MediaDescribeResult, error)
}

type containerProviderView struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (*datatype.ContainerInfo, error)
}

type containerChildResolverView struct {
	formatType FormatType
	resolve    func(context.Context, contentio.Reader, contentio.Ref, datatype.ContainerChildInfo, *ParseOptions) (*ContainerChildResource, error)
}

func (p tableInfoProviderView) Format() FormatType {
	return p.formatType
}

func (p tableInfoProviderView) DescribeTable(ctx context.Context, input io.Reader, options *ParseOptions) (*TableDescribeResult, error) {
	return p.describe(ctx, input, options)
}

func (p tableSampleReaderView) Format() FormatType {
	return p.formatType
}

func (p tableSampleReaderView) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error) {
	return p.sample(ctx, input, offset, limit, options)
}

func (p formatInfoProviderView) Format() FormatType {
	return p.formatType
}

func (p formatInfoProviderView) DescribeFormat(ctx context.Context, input io.Reader, options *ParseOptions) (map[string]interface{}, error) {
	return p.describe(ctx, input, options)
}

func (p documentInfoProviderView) Format() FormatType {
	return p.formatType
}

func (p documentInfoProviderView) DescribeDocument(ctx context.Context, input io.Reader, options *ParseOptions) (*datatype.DocumentInfo, error) {
	return p.describe(ctx, input, options)
}

func (p documentTextReaderView) Format() FormatType {
	return p.formatType
}

func (p documentTextReaderView) ReadDocumentText(ctx context.Context, input io.Reader, limit int64, options *ParseOptions) (string, bool, error) {
	return p.extract(ctx, input, limit, options)
}

func (p mediaProviderView) Format() FormatType {
	return p.formatType
}

func (p mediaProviderView) DescribeMedia(ctx context.Context, input io.Reader, options *ParseOptions) (*MediaDescribeResult, error) {
	return p.describe(ctx, input, options)
}

func (p containerProviderView) Format() FormatType {
	return p.formatType
}

func (p containerProviderView) DescribeContainer(ctx context.Context, input io.Reader, options *ParseOptions) (*datatype.ContainerInfo, error) {
	return p.describe(ctx, input, options)
}

func (p containerChildResolverView) Format() FormatType {
	return p.formatType
}

func (p containerChildResolverView) ResolveContainerChild(ctx context.Context, parent contentio.Reader, parentRef contentio.Ref, child datatype.ContainerChildInfo, options *ParseOptions) (*ContainerChildResource, error) {
	return p.resolve(ctx, parent, parentRef, child, options)
}

func missingFormatInfoProvider(formatType FormatType) func(context.Context, io.Reader, *ParseOptions) (map[string]interface{}, error) {
	return func(context.Context, io.Reader, *ParseOptions) (map[string]interface{}, error) {
		return nil, fmt.Errorf("format info provider %s does not implement DescribeFormat", formatType)
	}
}
