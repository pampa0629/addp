package ksplat

import (
	"context"
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register KSPLAT format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatKSplat
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-ksplat",
		Format:   format.FormatKSplat,
		I18nKey:  "format.ksplat",
		DataType: datatype.GaussianSplat,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".ksplat"},
			MimeTypes:  []string{"application/vnd.gaussian-ksplat"},
		},
	}
}

func (p *Plugin) DescribeGaussianSplat(ctx context.Context, _ format.GaussianSplatDescribeInput, _ *format.ParseOptions) (*format.GaussianSplatDescribeResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return &format.GaussianSplatDescribeResult{
		GaussianSplat: &datatype.GaussianSplatInfo{
			Representation: datatype.GaussianSplatRepresentation3DGS,
		},
		FormatInfo: map[string]interface{}{
			"encoding": "ksplat",
		},
	}, nil
}
