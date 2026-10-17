package topojson

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/gdalvector"
	"github.com/addp/common/format/plugins/shared/jsonrecords"
)

type Plugin struct{ *gdalvector.ReadOnlyPlugin }

func NewPlugin() *Plugin {
	return &Plugin{gdalvector.NewReadOnlyPlugin(format.FormatDescriptor{
		ID: "builtin-topojson", Format: format.FormatTopoJSON, I18nKey: "format.topojson",
		DataType: datatype.Container, Layouts: []string{format.LayoutSingle},
		Identification: format.FormatIdentification{Extensions: []string{".topojson"}},
	})}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}

func (p *Plugin) SniffFormat(peek []byte) bool { return jsonrecords.LooksLikeTopology(peek) }
