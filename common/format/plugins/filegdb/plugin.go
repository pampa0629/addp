package filegdb

import (
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/gdalvector"
)

func NewPlugin() *gdalvector.ReadWritePlugin {
	return gdalvector.NewReadWritePlugin(format.FormatDescriptor{
		ID:       "builtin-filegdb",
		Format:   format.FormatFileGDB,
		I18nKey:  "format.filegdb",
		DataType: datatype.Container,
		Layouts:  []string{format.LayoutWhole},
		Identification: format.FormatIdentification{
			Extensions:    []string{".gdb"},
			RelativePaths: []string{"a00000001.gdbtable", "a00000001.gdbtablx"},
			MimeTypes:     []string{"application/x-esri-file-geodatabase"},
		},
	})
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register FileGDB format plugin: %v", err))
	}
}
