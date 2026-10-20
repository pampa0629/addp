package service

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/jsonmap"
	"github.com/addp/common/resourcetree"
	"github.com/google/uuid"
)

// The private producer adapter freezes this value only after verifying its
// actual receipt. Projection validates the same stored row, without live IO.
// System must still independently validate the producer receipt and eligibility.
func projectInitialProductionScanScope(row scanPlanExecutionRow, config map[string]any, facts *execution.MetaScanPlanFacts) {
	if row.SourceTaskID != nil || row.TriggerType != execution.TriggerTypeManual || row.Source != execution.ModuleTransfer ||
		facts.ScanDepth != "basic" || config["source"] != execution.ModuleTransfer || config["force"] != true || config["engine_type"] != "postgresql" || config["storage_type"] != "postgresql" ||
		row.ParentExecutionID == nil || row.ActorPrincipalID == nil || *row.ActorPrincipalID <= 0 ||
		row.ActorTenantMembershipID == nil || *row.ActorTenantMembershipID <= 0 || row.IssuedAuthorizationVersion == nil || *row.IssuedAuthorizationVersion <= 0 ||
		row.TriggeredBy == nil || int64(*row.TriggeredBy) != *row.ActorPrincipalID {
		return
	}
	parent, err := uuid.Parse(*row.ParentExecutionID)
	if err != nil || parent == uuid.Nil || parent.String() != *row.ParentExecutionID || parent.String() == row.ExecutionID {
		return
	}
	metadata, err := jsonmap.DecodeObjectStrict([]byte(row.Metadata), execution.MaxExecutionConfigBytes, 64)
	if err != nil || metadata["initial_production_execution_id"] != parent.String() {
		return
	}
	for _, key := range []string{"item_id", "node_id", "task_definition_digest", "planned_run_at", "encoding", "has_header"} {
		if _, exists := config[key]; exists {
			return
		}
	}
	for _, key := range []string{"catalog_paths", "ref_groups"} {
		if value := config[key]; value != nil {
			values, ok := value.([]any)
			if !ok || len(values) != 0 {
				return
			}
		}
	}
	targets, ok := config["targets"].([]any)
	if !ok || len(targets) != 1 {
		return
	}
	uri, ok := targets[0].(string)
	if !ok {
		return
	}
	raw, err := json.Marshal(config["initial_production_target"])
	if err != nil {
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var target plugin.EngineCatalogPath
	if decoder.Decode(&target) != nil {
		return
	}
	scope := &execution.MetaScanSourceScope{Mode: "exact", Anchors: []plugin.EngineCatalogPath{target}}
	model := plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)
	if scope.Validate(facts.EngineID) != nil || !scanPlanNativePathMatchesModel(target, model) || target.Segments[2].Kind != plugin.EngineCatalogKindTable {
		return
	}
	locator, err := resourcetree.ParseURI(uri)
	if err != nil || locator.Type != resourcetree.TypeTable || locator.NodeID != nil || locator.ItemID != nil {
		return
	}
	resolved, err := resourcetree.EngineCatalogPathFromLocator(model, locator)
	if err != nil || !reflect.DeepEqual(resolved, target) {
		return
	}
	facts.SourceScope = scope
	facts.BasisState = authorizationfacts.AuthorizationBasisComplete
}
