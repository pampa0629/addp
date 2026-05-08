package format

import formatcap "github.com/addp/common/format/capability"

const (
	// FormatTable 是 ADDP 内部用于描述表格型引擎传输能力的逻辑格式。
	FormatTable FormatType = FormatType(formatcap.FormatTable)
	// FormatDocument 是 ADDP 内部用于描述文档型引擎传输能力的逻辑格式。
	FormatDocument FormatType = FormatType(formatcap.FormatDocument)
)

const (
	FormatDataTypeTabular  = formatcap.DataTypeTabular
	FormatDataTypeDocument = formatcap.DataTypeDocument
	FormatDataTypeFile     = formatcap.DataTypeFile
)

const (
	EngineFamilyTabular  = formatcap.EngineFamilyTabular
	EngineFamilyObject   = formatcap.EngineFamilyObject
	EngineFamilyFile     = formatcap.EngineFamilyFile
	EngineFamilyDocument = formatcap.EngineFamilyDocument
)

// FormatCapability 声明一个格式在 ADDP 中可被哪些平台能力消费。
type FormatCapability struct {
	Format         FormatType
	I18nKey        string
	Extensions     []string
	DataType       string
	TransferRead   bool
	TransferWrite  bool
	Preview        bool
	Parse          bool
	EngineFamilies []string
}

func RegisterFormatCapability(capability FormatCapability) error {
	return formatcap.Register(toFormatCap(capability))
}

func GetFormatCapability(format FormatType) (FormatCapability, bool) {
	capability, ok := formatcap.Get(formatcap.Format(format))
	if !ok {
		return FormatCapability{}, false
	}
	return fromFormatCap(capability), true
}

func ListFormatCapabilities() []FormatCapability {
	capabilities := formatcap.List()
	result := make([]FormatCapability, 0, len(capabilities))
	for _, capability := range capabilities {
		result = append(result, fromFormatCap(capability))
	}
	return result
}

func ListTransferFormatsForEngineFamily(engineFamily string) []string {
	return formatcap.ListTransferFormatsForEngineFamily(engineFamily)
}

func toFormatCap(capability FormatCapability) formatcap.Capability {
	return formatcap.Capability{
		Format:         formatcap.Format(capability.Format),
		I18nKey:        capability.I18nKey,
		Extensions:     capability.Extensions,
		DataType:       capability.DataType,
		TransferRead:   capability.TransferRead,
		TransferWrite:  capability.TransferWrite,
		Preview:        capability.Preview,
		Parse:          capability.Parse,
		EngineFamilies: capability.EngineFamilies,
	}
}

func fromFormatCap(capability formatcap.Capability) FormatCapability {
	return FormatCapability{
		Format:         FormatType(capability.Format),
		I18nKey:        capability.I18nKey,
		Extensions:     capability.Extensions,
		DataType:       capability.DataType,
		TransferRead:   capability.TransferRead,
		TransferWrite:  capability.TransferWrite,
		Preview:        capability.Preview,
		Parse:          capability.Parse,
		EngineFamilies: capability.EngineFamilies,
	}
}
