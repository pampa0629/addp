package service

import "github.com/addp/common/sqleffect"

type SQLExecutionEffect = sqleffect.Effect

const (
	SQLExecutionEffectRead           = sqleffect.Read
	SQLExecutionEffectWrite          = sqleffect.Write
	SQLExecutionEffectDDL            = sqleffect.DDL
	SQLExecutionEffectExternalEffect = sqleffect.ExternalEffect
)

func ClassifySQLExecutionEffect(sql string) (SQLExecutionEffect, error) {
	return sqleffect.Classify(sql)
}
