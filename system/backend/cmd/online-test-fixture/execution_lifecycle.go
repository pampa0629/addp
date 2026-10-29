package main

// These roles are issued only by the disposable Online fixture. They do not
// modify default roles or grant Graph reconciliation to existing consumers.
var graphExecutionPermissions = []string{
	"graph.graph.read", "graph.graph.create", "graph.graph.update", "graph.graph.reconcile",
	"graph.ontology.create", "graph.ontology.read", "graph.build_task.create", "graph.build_task.read",
	"graph.build_task.update", "graph.build_task.execute", "graph.build_task.cancel",
}

var cleanupExecutionPermissions = []string{
	"system.cleanup.read", "system.cleanup.execute",
	"asset.cleanup_result.read", "develop.cleanup_result.read", "graph.cleanup_result.read",
	"manager.cleanup_result.read", "meta.cleanup_result.read", "model.cleanup_result.read",
	"quality.cleanup_result.read", "service.cleanup_result.read", "standard.cleanup_result.read", "transfer.cleanup_result.read",
}
