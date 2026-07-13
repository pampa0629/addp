package dwg

import (
	"fmt"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

type Plugin struct{}

func NewPlugin() *Plugin { return &Plugin{} }
func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register DWG format plugin: %v", err))
	}
}
func (p *Plugin) Format() format.FormatType { return format.FormatDWG }
func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID: "builtin-dwg", Format: format.FormatDWG, I18nKey: "format.dwg", DataType: datatype.CAD,
		Layouts: []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".dwg"},
			MimeTypes:  []string{"application/acad", "application/x-acad", "application/autocad_dwg", "application/dwg", "image/vnd.dwg", "image/x-dwg"},
		},
	}
}
func (p *Plugin) SniffFormat(peek []byte) bool {
	return len(peek) >= 6 && string(peek[:4]) == "AC10" && peek[4] >= '0' && peek[4] <= '9' && peek[5] >= '0' && peek[5] <= '9'
}
