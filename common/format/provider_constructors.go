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

func NewTableProvider(
	formatType FormatType,
	describe func(context.Context, io.Reader, *ParseOptions) (*TableInfo, error),
	sample func(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error),
) TableProvider {
	if describe == nil {
		describe = func(context.Context, io.Reader, *ParseOptions) (*TableInfo, error) {
			return nil, fmt.Errorf("table provider %s does not implement DescribeTable", formatType)
		}
	}
	if sample == nil {
		sample = func(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error) {
			return nil, fmt.Errorf("table provider %s does not implement SampleTable", formatType)
		}
	}
	return tableProviderView{
		formatType: formatType,
		describe:   describe,
		sample:     sample,
	}
}

func NewMediaProvider(
	formatType FormatType,
	describe func(context.Context, io.Reader, *ParseOptions) (*MediaInfo, error),
) MediaProvider {
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

func NewDocumentProvider(
	formatType FormatType,
	describe func(context.Context, io.Reader, *ParseOptions) (*DocumentInfo, error),
	extract func(context.Context, io.Reader, int64, *ParseOptions) (string, bool, error),
) DocumentProvider {
	if describe == nil {
		describe = func(context.Context, io.Reader, *ParseOptions) (*DocumentInfo, error) {
			return nil, fmt.Errorf("document provider %s does not implement DescribeDocument", formatType)
		}
	}
	if extract == nil {
		extract = func(context.Context, io.Reader, int64, *ParseOptions) (string, bool, error) {
			return "", false, fmt.Errorf("document provider %s does not implement ReadDocumentText", formatType)
		}
	}
	return documentProviderView{
		formatType: formatType,
		describe:   describe,
		extract:    extract,
	}
}
