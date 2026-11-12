package service

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"strings"

	"github.com/addp/common/client"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/format"
	"github.com/addp/common/resourcetree"
)

func (e *DevExecutor) privateFileSource(ctx context.Context, tenant uint, node, locator string) (*execution.DevelopFileSourceIntent, error) {
	if locator == "" {
		return nil, nil
	}
	loc, err := resourcetree.ParseURI(locator)
	if err != nil {
		return nil, &ExecutionParametersError{Cause: errFileWorkflowInputs}
	}
	if loc.Type != resourcetree.TypeFile && loc.Type != resourcetree.TypeObject {
		return nil, nil
	}
	model := plugin.FileCatalogModel()
	if loc.Type == resourcetree.TypeObject {
		model = plugin.ObjectCatalogModel()
	}
	target, err := resourcetree.EngineCatalogPathFromLocator(model, loc)
	if err != nil || e.metaClient == nil {
		return nil, &ExecutionParametersError{Cause: errFileWorkflowInputs}
	}
	// The locator item ID is display data. Meta must answer for the exact leaf.
	item, err := e.metaClient.GetItemSnapshot(ctx, tenant, target.EngineID, 0, target.StringPath())
	if err != nil || item.ItemType != string(loc.Type) {
		return nil, &ExecutionParametersError{Cause: errFileWorkflowInputs}
	}
	descriptor := dataitem.DescriptorFromAttributes(item.Attributes)
	switch descriptor.Format {
	case "", string(format.FormatUnknown):
		return nil, &ExecutionParametersError{Cause: errFileWorkflowInputs}
	case string(format.FormatGeoJSON):
		if loc.Type != resourcetree.TypeObject {
			if strings.EqualFold(path.Ext(target.StringPath()), ".gpkg") {
				return nil, &ExecutionParametersError{Cause: errFileWorkflowInputs}
			}
			return nil, nil
		}
		if descriptor.Layout != format.LayoutSingle || descriptor.DataType != datatype.Table {
			return nil, &ExecutionParametersError{Cause: errFileWorkflowInputs}
		}
		return &execution.DevelopFileSourceIntent{NodeID: node, Target: target}, nil
	case string(format.FormatGeoPackage):
		if loc.Type != resourcetree.TypeFile || descriptor.Layout != format.LayoutSingle || descriptor.DataType != datatype.Container {
			return nil, &ExecutionParametersError{Cause: errFileWorkflowInputs}
		}
		return &execution.DevelopFileSourceIntent{NodeID: node, Target: target}, nil
	default:
		// A generic Runtime request must not select the GeoPackage driver on
		// a pathname whose trusted classification says otherwise.
		if strings.EqualFold(path.Ext(target.StringPath()), ".gpkg") {
			return nil, &ExecutionParametersError{Cause: errFileWorkflowInputs}
		}
		return nil, nil
	}
}

func (e *DevExecutor) submitPrivateFileWorkflow(ctx context.Context, item *execution.TaskExecution, sources []execution.DevelopFileSourceIntent, credential string) error {
	tenant := item.TenantID
	if item.ParentExecutionID == nil {
		facts, ok := executionUserFacts(ctx)
		userTenant, principal, membership, version := executionActorIDs(facts)
		if !ok || (facts.Token.Type != "first_party_access_token" && facts.Token.Type != "oauth_access_token") || userTenant != tenant || principal <= 0 || membership <= 0 || version <= 0 || item.TriggeredBy == nil || int64(*item.TriggeredBy) != principal || item.Source != execution.ModuleDevelop || item.TriggerType != execution.TriggerTypeManual || e.fileExecutions == nil {
			return &ExecutionParametersError{Cause: errFileWorkflowInputs}
		}
		item.ActorPrincipalID, item.ActorTenantMembershipID, item.IssuedAuthorizationVersion = &principal, &membership, &version
	} else {
		if item.Source != execution.ModuleOrchestrator || item.TriggerType != execution.TriggerTypeManual || e.fileExecutions == nil || e.sqlEngine == nil || e.sqlEngine.systemService == nil {
			return &ExecutionParametersError{Cause: errFileWorkflowInputs}
		}
		var err error
		credential, err = e.sqlEngine.systemService.TenantServiceAccessToken(ctx, uint(tenant))
		if err != nil {
			return err
		}
	}
	delete(item.ExecutionConfig, execution.ObservabilityKey)

	content, _ := mapValue(item.ExecutionConfig["content"])
	definition, _ := mapValue(content["workflow_definition"])
	tasks, _ := workflowTasksFromInterface(definition["tasks"])
	var target *execution.DevelopFileTargetIntent
	for _, task := range tasks {
		if stringParam(task, "operator") != "save" {
			continue
		}
		if target != nil {
			return &ExecutionParametersError{Cause: errFileWorkflowInputs}
		}
		params, _ := task["params"].(map[string]interface{})
		var err error
		target, err = fileWorkflowSaveTarget(stringParam(task, "id"), params)
		if err != nil {
			return &ExecutionParametersError{Cause: err}
		}
	}
	if target != nil {
		for _, source := range sources {
			if _, err := plugin.RequireObjectLeafPath(source.Target); err == nil {
				return &ExecutionParametersError{Cause: errFileWorkflowInputs}
			}
		}
	}
	if item.ParentExecutionID != nil && target != nil {
		return &ExecutionParametersError{Cause: errFileWorkflowInputs}
	}
	// The same projection proves full source/target coverage before persistence;
	// mixed sources and caller-supplied connections never enter a pending row.
	raw, err := json.Marshal(item.ExecutionConfig)
	if err != nil {
		return &ExecutionParametersError{Cause: errFileWorkflowInputs}
	}
	if _, err := fileWorkflowRuntimeDefinition(&client.DevelopFileExecutionFacts{
		ConfigJSON: raw, Intent: execution.DevelopFileIntent{Sources: sources, Target: target},
	}); err != nil {
		return &ExecutionParametersError{Cause: errFileWorkflowInputs}
	}
	pending, err := e.fileExecutions.CreatePending(ctx, item, sources, target)
	if err != nil {
		return err
	}
	issued, err := e.sqlEngine.executionAuthorizations.IssueDevelopFile(ctx, credential, item.ExecutionID, pending.Intent)
	if err != nil {
		status, _ := client.SystemAPIStatusCode(err)
		switch status {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
			if rejectErr := e.fileExecutions.RejectUnissued(ctx, uint(tenant), item.ExecutionID); rejectErr != nil {
				return rejectErr
			}
		}
		return err
	}
	return e.fileExecutions.BindAuthorization(ctx, uint(tenant), item.ExecutionID, *issued)
}
