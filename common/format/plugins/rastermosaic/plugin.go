package rastermosaic

import (
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const ManifestFileName = "mosaic.addp.json"

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register raster mosaic format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatRasterMosaic
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-raster-mosaic",
		Format:   format.FormatRasterMosaic,
		I18nKey:  "format.raster_mosaic",
		DataType: datatype.Media,
		Layouts:  []string{format.LayoutWhole},
		Identification: format.FormatIdentification{
			FileNames: []string{ManifestFileName},
			MimeTypes: []string{
				"application/vnd.addp.raster-mosaic+json",
			},
		},
	}
}
