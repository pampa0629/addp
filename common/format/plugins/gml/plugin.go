package gml

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/gdalvector"
)

func NewPlugin() *gdalvector.ReadOnlyPlugin {
	return gdalvector.NewReadOnlyPlugin(format.FormatDescriptor{
		ID: "builtin-gml", Format: format.FormatGML, I18nKey: "format.gml",
		DataType: datatype.Container, Layouts: []string{format.LayoutSingle},
		Identification: format.FormatIdentification{Extensions: []string{".gml"}, MimeTypes: []string{"application/gml+xml"}},
	})
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
