package access

import (
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/gdalvector"
)

func NewPlugin() *gdalvector.DetectionOnlyPlugin {
	return gdalvector.NewDetectionOnlyPlugin(format.FormatDescriptor{
		ID:       "builtin-access",
		Format:   format.FormatAccess,
		I18nKey:  "format.access",
		DataType: datatype.Container,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".mdb"},
			MimeTypes:  []string{"application/x-msaccess"},
		},
	})
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register Access format plugin: %v", err))
	}
}
