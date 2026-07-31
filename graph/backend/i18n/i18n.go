package i18n

import (
	"embed"

	commoni18n "github.com/addp/common/middleware/i18n"
)

//go:embed locales/*.toml
var localeFS embed.FS

// Graph 模块消息 key 常量
const (
	// 通用
	MsgLoadFailed   = "graph.err.load_failed"
	MsgSaveFailed   = "graph.err.save_failed"
	MsgDeleteFailed = "graph.err.delete_failed"
	MsgNotFound     = "graph.err.not_found"
	MsgUnauthorized = "graph.err.unauthorized"

	// 本体
	MsgOntologyCreated          = "graph.ontology.created"
	MsgOntologyUpdated          = "graph.ontology.updated"
	MsgOntologyDeleted          = "graph.ontology.deleted"
	MsgDisplayPropertyNotFound  = "graph.ontology.display_property_not_found"
	MsgDisplayPropertyNotString = "graph.ontology.display_property_not_string"

	// 图谱
	MsgGraphCreated        = "graph.kg.created"
	MsgGraphUpdated        = "graph.kg.updated"
	MsgGraphDeleted        = "graph.kg.deleted"
	MsgExpandTargetInvalid = "graph.browse.expand_target_invalid"

	// 构建
	MsgTaskStarted         = "graph.build.task_started"
	MsgTaskCancelled       = "graph.build.task_cancelled"
	MsgTaskRestarted       = "graph.build.task_restarted"
	MsgTaskActive          = "graph.build.task_active"
	MsgTaskRuntimeNotOwned = "graph.build.task_runtime_not_owned"
	MsgTaskRunFailed       = "graph.build.task_run_failed"

	// 审核
	MsgApproved      = "graph.review.approved"
	MsgRejected      = "graph.review.rejected"
	MsgModified      = "graph.review.modified"
	MsgBatchApproved = "graph.review.batch_approved"
	MsgBatchRejected = "graph.review.batch_rejected"

	// 分析
	MsgAlgoFailed = "graph.analysis.algo_failed"

	// 知识服务
	MsgGraphNotPublic = "graph.service.not_public"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
