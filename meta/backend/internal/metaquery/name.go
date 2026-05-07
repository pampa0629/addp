package metaquery

import "strings"

// ParseTableName 解析表名，支持 schema.table 格式。
func ParseTableName(tableName string) (schema, table string) {
	parts := strings.SplitN(tableName, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", parts[0]
}

func QualifiedName(namespace, itemName string) string {
	if namespace == "" {
		return itemName
	}
	if strings.Contains(itemName, ".") || strings.Contains(itemName, "/") {
		return itemName
	}
	return namespace + "." + itemName
}
