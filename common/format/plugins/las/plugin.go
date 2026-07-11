package las

import (
	"context"
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/shared/lasfamily"
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register LAS format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatLAS
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-las",
		Format:   format.FormatLAS,
		I18nKey:  "format.las",
		DataType: datatype.PointCloud,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions:        []string{".las"},
			MimeTypes:         []string{"application/vnd.las", "application/octet-stream"},
			ContentSignatures: []string{"hex:4c415346"},
		},
	}
}

func (p *Plugin) SniffFormat(peek []byte) bool {
	return len(peek) >= 4 && string(peek[:4]) == lasfamily.Magic
}

func (p *Plugin) DescribePointCloud(ctx context.Context, input format.PointCloudDescribeInput, options *format.ParseOptions) (*format.PointCloudDescribeResult, error) {
	header, err := lasfamily.ReadHeader(input.Reader)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return &format.PointCloudDescribeResult{
		PointCloud: lasfamily.BuildPointCloudInfo(header, datatype.PointCloudKindRawPointCloud),
		Spatial:    lasfamily.BuildSpatialInfo(header),
		FormatInfo: lasfamily.BuildHeaderFormatInfo(header),
	}, nil
}
