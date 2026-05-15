package table

import "github.com/addp/common/format"

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatTable
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:             "builtin-table",
		Format:         p.Format(),
		I18nKey:        "format.table",
		DataType:       format.FormatDataTypeTable,
		Layouts:        []string{format.FormatLayoutWhole},
		ProviderHints:  []string{format.FormatProviderTable},
		Providers:      format.FormatProviderDescriptor{TableInfo: true, TableSample: true, Table: true},
		ContentReaders: []string{string(format.ContentReaderTableSample)},
		TransferRead:   true,
		TransferWrite:  true,
		EngineFamilies: []string{
			format.EngineFamilyTabular,
		},
	}
}

func (p *Plugin) Capabilities() format.FormatCapability {
	capability, ok := format.GetFormatCapability(p.Format())
	if ok {
		return capability
	}
	return format.FormatCapabilityFromDescriptor(p.Descriptor())
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(err)
	}
}
