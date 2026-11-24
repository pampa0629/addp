package repository

import (
	"gorm.io/gorm"
	"strings"
)

// previewJSONText reads scalar fields of Manager definitions and results.
// Column names and member paths come from the repository, never request text.
func previewJSONText(db *gorm.DB, column, path string) string {
	if db.Dialector.Name() == "mysql" {
		return "CAST(JSON_UNQUOTE(NULLIF(JSON_EXTRACT(" + column + ", '$." + path + "'), CAST('null' AS JSON))) AS BINARY)"
	}
	members := strings.Split(path, ".")
	last := len(members) - 1
	for _, member := range members[:last] {
		column += "->'" + member + "'"
	}
	return column + "->>'" + members[last] + "'"
}
