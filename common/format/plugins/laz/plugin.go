package laz

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
		panic(fmt.Sprintf("failed to register LAZ format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatLAZ
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-laz",
		Format:   format.FormatLAZ,
		I18nKey:  "format.laz",
		DataType: datatype.PointCloud,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions:        []string{".laz"},
			MimeTypes:         []string{"application/vnd.laszip", "application/vnd.laz", "application/octet-stream"},
			ContentSignatures: []string{"hex:4c415346"},
		},
	}
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
	formatInfo := lasfamily.BuildHeaderFormatInfo(header)
	formatInfo["compression"] = "laszip"
	return &format.PointCloudDescribeResult{
		PointCloud: lasfamily.BuildPointCloudInfo(header, datatype.PointCloudKindRawPointCloud),
		Spatial:    lasfamily.BuildSpatialInfo(header),
		FormatInfo: formatInfo,
	}, nil
}
