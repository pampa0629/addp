package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// Develop 模块消息 key 常量
const (
	MsgExecutionStarted    = "develop.execution.started"
	MsgExecutionCancelled  = "develop.execution.cancelled"
	MsgRetryStarted        = "develop.execution.retry_started"
	MsgParamExecStarted    = "develop.execution.param_exec_started"
	MsgLogsNotReady        = "develop.execution.logs_not_ready"
	MsgDeleteSuccess       = "develop.item.delete_success"
	MsgUseExecuteEndpoint  = "develop.item.use_execute_endpoint"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
