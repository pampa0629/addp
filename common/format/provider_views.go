package format

import (
	"context"
	"fmt"
	"io"

	"github.com/addp/common/resource"
)

type tableProviderView struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (*TableInfo, error)
	sample     func(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error)
}

type formatInfoProviderView struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (map[string]interface{}, error)
}

type documentProviderView struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (*DocumentInfo, error)
	extract    func(context.Context, io.Reader, int64, *ParseOptions) (string, bool, error)
}

type mediaProviderView struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (*MediaInfo, error)
}

type containerProviderView struct {
	formatType FormatType
	describe   func(context.Context, io.Reader, *ParseOptions) (*ContainerInfo, error)
}

type containerChildResolverView struct {
	formatType FormatType
	resolve    func(context.Context, resource.ResourceReader, resource.ResourceRef, ContainerChildInfo, *ParseOptions) (*ContainerChildResource, error)
}

func (p tableProviderView) Format() FormatType {
	return p.formatType
}

func (p tableProviderView) Descriptor() FormatDescriptor {
	descriptor, ok := GetFormatDescriptor(p.formatType)
	if ok {
		return descriptor
	}
	return FormatDescriptor{
		ID:       "inline-" + string(p.formatType),
		Format:   p.formatType,
		DataType: FormatDataTypeTable,
		Layouts:  []string{FormatLayoutSingle},
	}
}

func (p tableProviderView) Capabilities() FormatCapability {
	capability, ok := GetFormatCapability(p.formatType)
	if ok {
		return capability
	}
	return FormatCapability{
		Format:        p.formatType,
		DataType:      FormatDataTypeTable,
		Layouts:       []string{FormatLayoutSingle},
		ProviderHints: []string{FormatProviderTable},
		Parse:         true,
	}
}

func (p tableProviderView) DescribeTable(ctx context.Context, input io.Reader, options *ParseOptions) (*TableInfo, error) {
	return p.describe(ctx, input, options)
}

func (p tableProviderView) SampleTable(ctx context.Context, input io.Reader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error) {
	return p.sample(ctx, input, offset, limit, options)
}

func (p formatInfoProviderView) Format() FormatType {
	return p.formatType
}

func (p formatInfoProviderView) Capabilities() FormatCapability {
	capability, ok := GetFormatCapability(p.formatType)
	if ok {
		return capability
	}
	return FormatCapability{
		Format:  p.formatType,
		Layouts: []string{FormatLayoutSingle},
	}
}

func (p formatInfoProviderView) DescribeFormat(ctx context.Context, input io.Reader, options *ParseOptions) (map[string]interface{}, error) {
	return p.describe(ctx, input, options)
}

func (p documentProviderView) Format() FormatType {
	return p.formatType
}

func (p documentProviderView) Capabilities() FormatCapability {
	capability, ok := GetFormatCapability(p.formatType)
	if ok {
		return capability
	}
	return FormatCapability{
		Format:        p.formatType,
		DataType:      FormatDataTypeDocument,
		Layouts:       []string{FormatLayoutSingle},
		ProviderHints: []string{FormatProviderDocument},
	}
}

func (p documentProviderView) DescribeDocument(ctx context.Context, input io.Reader, options *ParseOptions) (*DocumentInfo, error) {
	return p.describe(ctx, input, options)
}

func (p documentProviderView) ReadDocumentText(ctx context.Context, input io.Reader, limit int64, options *ParseOptions) (string, bool, error) {
	return p.extract(ctx, input, limit, options)
}

func (p mediaProviderView) Format() FormatType {
	return p.formatType
}

func (p mediaProviderView) Capabilities() FormatCapability {
	capability, ok := GetFormatCapability(p.formatType)
	if ok {
		return capability
	}
	return FormatCapability{
		Format:        p.formatType,
		DataType:      FormatDataTypeMedia,
		Layouts:       []string{FormatLayoutSingle},
		ProviderHints: []string{FormatProviderMedia},
	}
}

func (p mediaProviderView) DescribeMedia(ctx context.Context, input io.Reader, options *ParseOptions) (*MediaInfo, error) {
	return p.describe(ctx, input, options)
}

func (p containerProviderView) Format() FormatType {
	return p.formatType
}

func (p containerProviderView) Capabilities() FormatCapability {
	capability, ok := GetFormatCapability(p.formatType)
	if ok {
		return capability
	}
	return FormatCapability{
		Format:        p.formatType,
		DataType:      FormatDataTypeContainer,
		Layouts:       []string{FormatLayoutSingle},
		ProviderHints: []string{FormatProviderContainer},
	}
}

func (p containerProviderView) DescribeContainer(ctx context.Context, input io.Reader, options *ParseOptions) (*ContainerInfo, error) {
	return p.describe(ctx, input, options)
}

func (p containerChildResolverView) Format() FormatType {
	return p.formatType
}

func (p containerChildResolverView) Capabilities() FormatCapability {
	capability, ok := GetFormatCapability(p.formatType)
	if ok {
		return capability
	}
	return FormatCapability{
		Format:         p.formatType,
		DataType:       FormatDataTypeContainer,
		Layouts:        []string{FormatLayoutSingle},
		ProviderHints:  []string{FormatProviderContainer},
		ContentReaders: []string{string(ContentReaderContainerEntry)},
	}
}

func (p containerChildResolverView) ResolveContainerChild(ctx context.Context, parent resource.ResourceReader, parentRef resource.ResourceRef, child ContainerChildInfo, options *ParseOptions) (*ContainerChildResource, error) {
	return p.resolve(ctx, parent, parentRef, child, options)
}

func missingFormatInfoProvider(formatType FormatType) func(context.Context, io.Reader, *ParseOptions) (map[string]interface{}, error) {
	return func(context.Context, io.Reader, *ParseOptions) (map[string]interface{}, error) {
		return nil, fmt.Errorf("format info provider %s does not implement DescribeFormat", formatType)
	}
}
