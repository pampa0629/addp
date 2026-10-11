package main

// Preparation and acceptance are different Users. The acceptance User cannot
// create or approve the permanent-for-this-run Model fixture definitions.
var qualityPermissions = []string{
	"orchestrator.workflow.read", "orchestrator.workflow.create", "orchestrator.workflow.delete", "orchestrator.workflow.execute",
	"model.logical_model.read", "model.materialization.execute", "monitor.execution.read",
	"quality.rule.read", "quality.rule.create", "quality.rule.update", "quality.rule.delete",
	"quality.plan.read", "quality.plan.create", "quality.plan.update", "quality.plan.delete", "quality.plan.execute", "quality.issue.read",
	"system.execution_authorization.create", "develop.data_read.execute", "develop.data_ddl.execute",
}

var qualityPreparationPermissions = []string{
	"model.dw_layer.create", "model.logical_model.create", "model.logical_model.read", "model.logical_model.update",
	"model.materialization.execute", "monitor.execution.read",
	"meta.scan_task.execute", "meta.scan_task.read", "meta.catalog.read",
	"system.execution_authorization.create", "develop.data_read.execute", "develop.data_ddl.execute",
}
