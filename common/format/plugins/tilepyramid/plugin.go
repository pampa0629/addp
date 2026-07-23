package tilepyramid

import (
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	manifest "github.com/addp/common/tilepyramid"
)

type Plugin struct{}

func init() {
	if err := format.RegisterFormatPlugin(&Plugin{}); err != nil {
		panic(fmt.Sprintf("failed to register tile pyramid format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType { return format.FormatTilePyramid }

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-tile-pyramid",
		Format:   format.FormatTilePyramid,
		I18nKey:  "format.tile_pyramid",
		DataType: datatype.Media,
		Layouts:  []string{format.LayoutWhole},
		Identification: format.FormatIdentification{
			FileNames: []string{manifest.ManifestFileName},
			MimeTypes: []string{"application/vnd.addp.tile-pyramid+json"},
		},
	}
}
