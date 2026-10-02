package service

import (
	"github.com/addp/common/models"
	orchi18n "github.com/addp/orchestrator/i18n"
)

func executionFailureMessage(code string) string {
	message, _ := orchi18n.ExecutionFailureMessage("zh-cn", code)
	return message
}

func executionFailureDetails(code string) models.JSONMap {
	return models.JSONMap{"code": code, "message": executionFailureMessage(code)}
}
