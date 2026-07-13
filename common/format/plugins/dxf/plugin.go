package dxf

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

type Plugin struct{}

func NewPlugin() *Plugin { return &Plugin{} }

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register DXF format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType { return format.FormatDXF }

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID: "builtin-dxf", Format: format.FormatDXF, I18nKey: "format.dxf", DataType: datatype.CAD,
		Layouts: []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".dxf"},
			MimeTypes:  []string{"application/dxf", "application/x-dxf", "image/vnd.dxf", "image/x-dxf"},
		},
	}
}

func (p *Plugin) SniffFormat(peek []byte) bool {
	if bytes.HasPrefix(peek, []byte("AutoCAD Binary DXF\r\n\x1a\x00")) {
		return true
	}
	normalized := bytes.ReplaceAll(bytes.TrimSpace(bytes.TrimPrefix(peek, []byte{0xEF, 0xBB, 0xBF})), []byte("\r\n"), []byte("\n"))
	lines := bytes.Split(normalized, []byte("\n"))
	return len(lines) >= 2 && string(bytes.TrimSpace(lines[0])) == "0" && strings.EqualFold(string(bytes.TrimSpace(lines[1])), "SECTION")
}
