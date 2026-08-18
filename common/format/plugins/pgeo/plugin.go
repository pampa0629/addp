package pgeo

import (
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/gdalvector"
)

func NewPlugin() *gdalvector.ReadOnlyPlugin {
	return gdalvector.NewReadOnlyPlugin(format.FormatDescriptor{
		ID:       "builtin-pgeo",
		Format:   format.FormatPGeo,
		I18nKey:  "format.pgeo",
		DataType: datatype.Container,
		Layouts:  []string{format.LayoutSingle},
	})
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register PGeo format plugin: %v", err))
	}
}
