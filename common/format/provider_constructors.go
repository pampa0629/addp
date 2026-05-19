package format

import (
	"context"
	"fmt"
	"io"

	"github.com/addp/common/contentio"
)

func NewFormatInfoProvider(
	formatType FormatType,
	describe func(context.Context, io.Reader, *ParseOptions) (map[string]interface{}, error),
) FormatInfoProvider {
	if describe == nil {
		describe = missingFormatInfoProvider(formatType)
	}
	return formatInfoProviderView{
		formatType: formatType,
		describe:   describe,
	}
}

func NewTableInfoProvider(
	formatType FormatType,
	describe func(context.Context, io.Reader, *ParseOptions) (*TableInfo, error),
) TableInfoProvider {
	if describe == nil {
		describe = func(context.Context, io.Reader, *ParseOptions) (*TableInfo, error) {
			return nil, fmt.Errorf("table info provider %s does not implement DescribeTable", formatType)
		}
	}
	return tableInfoProviderView{
		formatType: formatType,
		describe:   describe,
	}
}

func NewTableSampleReader(
	formatType FormatType,
	sample func(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error),
) TableSampleReader {
	if sample == nil {
		sample = func(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error) {
			return nil, fmt.Errorf("table sample reader %s does not implement SampleTable", formatType)
		}
	}
	return tableSampleReaderView{
		formatType: formatType,
		sample:     sample,
	}
}

func NewMediaInfoProvider(
	formatType FormatType,
	describe func(context.Context, io.Reader, *ParseOptions) (*MediaInfo, error),
) MediaInfoProvider {
	if describe == nil {
		describe = func(context.Context, io.Reader, *ParseOptions) (*MediaInfo, error) {
			return nil, fmt.Errorf("media provider %s does not implement DescribeMedia", formatType)
		}
	}
	return mediaProviderView{
		formatType: formatType,
		describe:   describe,
	}
}

func NewContainerInfoProvider(
	formatType FormatType,
	describe func(context.Context, io.Reader, *ParseOptions) (*ContainerInfo, error),
) ContainerInfoProvider {
	if describe == nil {
		describe = func(context.Context, io.Reader, *ParseOptions) (*ContainerInfo, error) {
			return nil, fmt.Errorf("container info provider %s does not implement DescribeContainer", formatType)
		}
	}
	return containerProviderView{
		formatType: formatType,
		describe:   describe,
	}
}

func NewContainerChildResolver(
	formatType FormatType,
	resolve func(context.Context, contentio.Reader, contentio.Ref, ContainerChildInfo, *ParseOptions) (*ContainerChildResource, error),
) ContainerChildResolver {
	if resolve == nil {
		resolve = func(context.Context, contentio.Reader, contentio.Ref, ContainerChildInfo, *ParseOptions) (*ContainerChildResource, error) {
			return nil, fmt.Errorf("container child resolver %s does not implement ResolveContainerChild", formatType)
		}
	}
	return containerChildResolverView{
		formatType: formatType,
		resolve:    resolve,
	}
}

func NewDocumentInfoProvider(
	formatType FormatType,
	describe func(context.Context, io.Reader, *ParseOptions) (*DocumentInfo, error),
) DocumentInfoProvider {
	if describe == nil {
		describe = func(context.Context, io.Reader, *ParseOptions) (*DocumentInfo, error) {
			return nil, fmt.Errorf("document info provider %s does not implement DescribeDocument", formatType)
		}
	}
	return documentInfoProviderView{
		formatType: formatType,
		describe:   describe,
	}
}

func NewDocumentTextReader(
	formatType FormatType,
	extract func(context.Context, io.Reader, int64, *ParseOptions) (string, bool, error),
) DocumentTextReader {
	if extract == nil {
		extract = func(context.Context, io.Reader, int64, *ParseOptions) (string, bool, error) {
			return "", false, fmt.Errorf("document text reader %s does not implement ReadDocumentText", formatType)
		}
	}
	return documentTextReaderView{
		formatType: formatType,
		extract:    extract,
	}
}
