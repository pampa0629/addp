package kml

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/gdalvector"
)

func NewPlugin() *gdalvector.ReadOnlyPlugin {
	return gdalvector.NewReadOnlyPlugin(format.FormatDescriptor{
		ID: "builtin-kml", Format: format.FormatKML, I18nKey: "format.kml",
		DataType: datatype.Container, Layouts: []string{format.LayoutSingle},
		Identification: format.FormatIdentification{Extensions: []string{".kml"}, MimeTypes: []string{"application/vnd.google-earth.kml+xml"}},
	})
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
