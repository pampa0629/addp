package executor

import (
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

func isCopyWriteMethod(method string) bool {
	switch method {
	case "copy", "postgres_copy":
		return true
	default:
		return false
	}
}

func normalizeImportPrepareMode(mode string) string {
	switch mode {
	case "append":
		return "append"
	case "overwrite":
		return "overwrite"
	case "":
		return ""
	default:
		return mode
	}
}

func tableInfoFields(info *format.TableInfo) []engineplugin.FieldInfo {
	if info == nil {
		return nil
	}
	fields := make([]engineplugin.FieldInfo, 0, len(info.Fields))
	for _, field := range info.Fields {
		if field.Name == "" {
			continue
		}
		fields = append(fields, engineplugin.FieldInfo{
			Name:       field.Name,
			Type:       string(field.Type),
			NativeType: field.OriginalType,
			Nullable:   field.Nullable,
			PrimaryKey: field.IsPrimaryKey,
			Comment:    field.Comment,
		})
	}
	return fields
}
