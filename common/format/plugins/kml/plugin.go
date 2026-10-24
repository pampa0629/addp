package kml

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/gdalvector"
)

type Plugin struct{ *gdalvector.ReadOnlyPlugin }

func NewPlugin() *Plugin {
	return &Plugin{ReadOnlyPlugin: gdalvector.NewReadOnlyPlugin(format.FormatDescriptor{
		ID: "builtin-kml", Format: format.FormatKML, I18nKey: "format.kml",
		DataType: datatype.Container, Layouts: []string{format.LayoutSingle},
		Identification: format.FormatIdentification{Extensions: []string{".kml"}, MimeTypes: []string{"application/vnd.google-earth.kml+xml"}},
	})}
}

func (p *Plugin) SpatialEncodingCapability() format.SpatialEncodingCapability {
	c := p.ReadOnlyPlugin.SpatialEncodingCapability()
	c.RequiredWriteCRS = "EPSG:4326"
	return c
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
