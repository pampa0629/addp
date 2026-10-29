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

	MsgInvalidRequest           = "graph.err.invalid_request"
	MsgVersionConflict          = "graph.kg.version_conflict"
	MsgPhysicalTargetUnverified = "graph.kg.physical_target_unverified"
	MsgReconciliationIncomplete = "graph.kg.reconciliation_incomplete"
	MsgAdmissionClosed          = "graph.kg.admission_closed"
	// 图谱
	MsgProtectionRequired   = "graph.kg.protection_required"
	MsgTargetInvalid        = "graph.kg.target_invalid"
	MsgTargetConflict       = "graph.kg.target_conflict"
	MsgIndexConflict        = "graph.kg.index_conflict"
	MsgSearchLimit          = "graph.kg.search_limit"
	MsgTraversalLimit       = "graph.kg.traversal_limit"
	MsgGraphOperationFailed = "graph.kg.operation_failed"
	MsgGraphCreated         = "graph.kg.created"
	MsgGraphUpdated         = "graph.kg.updated"
	MsgGraphDeleted         = "graph.kg.deleted"
	MsgExpandTargetInvalid  = "graph.browse.expand_target_invalid"

	// 构建
	MsgTaskStarted              = "graph.build.task_started"
	MsgBuildEntityTypeUnknown   = "graph.build.entity_type_unknown"
	MsgBuildMergeKeyInvalid     = "graph.build.merge_key_invalid"
	MsgBuildMergeKeyEmpty       = "graph.build.merge_key_empty"
	MsgBuildRelationTypeUnknown = "graph.build.relation_type_unknown"
	MsgTaskCancelled            = "graph.build.task_cancelled"
	MsgTaskRestarted            = "graph.build.task_restarted"
	MsgTaskActive               = "graph.build.task_active"
	MsgTaskRunFailed            = "graph.build.task_run_failed"

	// 审核
	MsgReviewAlreadyProcessed = "graph.review.already_processed"
	MsgReviewBatchFailed      = "graph.review.batch_failed"
	MsgApproved               = "graph.review.approved"
	MsgRejected               = "graph.review.rejected"
	MsgModified               = "graph.review.modified"
	MsgBatchApproved          = "graph.review.batch_approved"
	MsgBatchRejected          = "graph.review.batch_rejected"

	// 分析
	MsgAlgoFailed = "graph.analysis.algo_failed"

	// 知识服务
	MsgGraphNotPublic = "graph.service.not_public"
)

func init() {
	commoni18n.RegisterBundle(localeFS, "locales")
}
