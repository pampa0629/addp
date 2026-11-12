package service

import (
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
)

// Project only the exact persisted plan, without live owner IO or credentials.
func projectInitialProductionScanScope(row scanPlanExecutionRow, config map[string]any, facts *execution.MetaScanPlanFacts) {
	if facts.ScanDepth != "basic" {
		return
	}
	run := &execution.TaskExecution{TenantID: row.TenantID, ExecutionID: row.ExecutionID, Source: row.Source, SourceTaskID: row.SourceTaskID, TriggerType: row.TriggerType, TriggeredBy: row.TriggeredBy, ActorPrincipalID: row.ActorPrincipalID, ActorTenantMembershipID: row.ActorTenantMembershipID, IssuedAuthorizationVersion: row.IssuedAuthorizationVersion, ParentExecutionID: row.ParentExecutionID, ExecutionConfig: commonmodels.JSONMap(config)}
	receipt, err := frozenProductionReceipt(run)
	if err != nil {
		return
	}
	paths := make([]plugin.EngineCatalogPath, len(receipt.Objects))
	for i, object := range receipt.Objects {
		paths[i] = object.Target
	}
	normalized, err := plugin.NewQueryReadSet(paths...)
	if err != nil {
		return
	}
	scope := &execution.MetaScanSourceScope{Mode: "exact", Anchors: normalized.Paths}
	if scope.Validate(facts.EngineID) != nil {
		return
	}
	facts.SourceScope = scope
	facts.BasisState = authorizationfacts.AuthorizationBasisComplete
}
