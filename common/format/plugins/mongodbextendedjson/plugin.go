package mongodbextendedjson

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

// Plugin registers the stable identity of MongoDB Canonical Extended JSON
// Lines. Encoding is source-engine work and is intentionally not exposed as a
// common table or document writer.
type Plugin struct{}

func (Plugin) Format() format.FormatType {
	return format.FormatMongoDBExtendedJSONL
}

func (Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-mongodb-extended-jsonl",
		Version:  "2",
		Format:   format.FormatMongoDBExtendedJSONL,
		I18nKey:  "format.mongodb_extended_jsonl",
		DataType: datatype.Unknown,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".ejsonl"},
			MimeTypes:  []string{"application/vnd.mongodb.extended-jsonl"},
		},
	}
}

func init() {
	if err := format.RegisterFormatPlugin(Plugin{}); err != nil {
		panic(err)
	}
}
