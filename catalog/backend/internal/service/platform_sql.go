package service

import (
	"strings"

	"gorm.io/gorm"
)

// The column and JSON path segments below come from Catalog query definitions.
func jsonText(db *gorm.DB, column string, keys ...string) string {
	if db.Dialector.Name() == "mysql" {
		value := "JSON_EXTRACT(" + column + ", '$." + strings.Join(keys, ".") + "')"
		return "CASE WHEN JSON_TYPE(" + value + ") <> 'NULL' THEN JSON_UNQUOTE(" + value + ") END"
	}
	for _, key := range keys[:len(keys)-1] {
		column += " -> '" + key + "'"
	}
	return column + " ->> '" + keys[len(keys)-1] + "'"
}

func integerExpression(db *gorm.DB, expression string) string {
	if db.Dialector.Name() == "mysql" {
		return "CAST(" + expression + " AS SIGNED)"
	}
	return "CAST(" + expression + " AS BIGINT)"
}

func textExpression(db *gorm.DB, expression string) string {
	if db.Dialector.Name() == "mysql" {
		return "CAST(" + expression + " AS CHAR)"
	}
	return "CAST(" + expression + " AS TEXT)"
}
