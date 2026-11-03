package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

type metadataRefreshScopeClient interface {
	GetMetadataRefreshScope(context.Context, string, authorization.MetadataRefreshQuery) (*authorization.MetadataRefreshScope, error)
}

type refreshCredentialKey struct{}

// WithScanRefreshCredential keeps the current User credential in this request
// context only. It is never copied to an execution, log, client or Worker.
func WithScanRefreshCredential(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, refreshCredentialKey{}, token)
}

func (s *ScanExecutionService) SetMetadataRefreshScopeClient(client metadataRefreshScopeClient) {
	s.refreshScopes = client
}

func (s *ScanExecutionService) prepareManualRefreshScope(ctx context.Context, origin *execution.TaskExecution, resource *commonmodels.Engine, req *models.ScanRequest, scope scanflow.Scope) (*authorization.MetadataRefreshScope, error) {
	if origin == nil || (resource.EngineType != "postgresql" && resource.EngineType != "nfs") {
		return nil, nil
	}
	if s.refreshScopes == nil {
		return nil, ErrScanPlanFactsUnavailable
	}
	credential, _ := ctx.Value(refreshCredentialKey{}).(string)
	if credential == "" {
		return nil, commonapi.ErrForbidden
	}
	query, err := s.manualRefreshQuery(ctx, uint(origin.TenantID), resource, req, scope)
	if err != nil {
		return nil, err
	}
	observed, err := s.refreshScopes.GetMetadataRefreshScope(ctx, credential, query)
	if err != nil {
		return nil, err
	}
	if observed == nil || observed.Validate() != nil || observed.TenantID != int64(origin.TenantID) || observed.EngineVersion != strconv.FormatInt(resource.Version, 10) || !reflect.DeepEqual(observed.Query, query) ||
		origin.ActorPrincipalID == nil || observed.Operator.PrincipalID != *origin.ActorPrincipalID || origin.ActorTenantMembershipID == nil || observed.Operator.MembershipID != *origin.ActorTenantMembershipID ||
		origin.IssuedAuthorizationVersion == nil || observed.Operator.AuthorizationVersion != *origin.IssuedAuthorizationVersion {
		return nil, commonapi.ErrForbidden
	}
	return observed, nil
}

func (s *ScanExecutionService) manualRefreshQuery(ctx context.Context, tenant uint, resource *commonmodels.Engine, req *models.ScanRequest, scope scanflow.Scope) (authorization.MetadataRefreshQuery, error) {
	invalid := authorization.MetadataRefreshQuery{}
	itemID, nodeID := req.ItemID, req.NodeID
	if len(req.Targets) == 1 {
		loc, err := resourcetree.ParseURI(req.Targets[0])
		if err != nil || loc.EngineID != resource.ID {
			return invalid, commonapi.ErrBadRequest
		}
		if loc.ItemID != nil {
			itemID = *loc.ItemID
		}
		if loc.NodeID != nil {
			nodeID = *loc.NodeID
		}
	}
	if itemID > 0 {
		var item models.MetaItem
		if err := s.db.WithContext(ctx).Where("tenant_id=? AND engine_id=? AND id=?", tenant, resource.ID, itemID).First(&item).Error; err != nil {
			return invalid, err
		}
		lookup, err := loadMetadataIdentityNodes(s.db.WithContext(ctx), tenant, []models.MetaItem{item})
		if err != nil {
			return invalid, err
		}
		target, err := metadataItemTarget(lookup, item)
		if err != nil {
			return invalid, err
		}
		return authorization.MetadataRefreshQuery{Kind: "item", Target: target}, nil
	}
	if nodeID > 0 {
		var node models.MetaNode
		if err := s.db.WithContext(ctx).Where("tenant_id=? AND engine_id=? AND id=?", tenant, resource.ID, nodeID).First(&node).Error; err != nil {
			return invalid, err
		}
		var target plugin.EngineCatalogPath
		if resource.EngineType == "postgresql" && node.NodeType == "schema" {
			target = plugin.TabularNamespacePath(resource.ID, "schema", node.Name)
		}
		if resource.EngineType == "nfs" && node.NodeType == "dir" {
			target = plugin.FileDirectoryPath(resource.ID, node.FullName)
		}
		if (authorizationfacts.NodeIdentityQuery{Target: target}).Validate() != nil || len(target.Segments) == 1 {
			return invalid, commonapi.ErrForbidden
		}
		return authorization.MetadataRefreshQuery{Kind: "node", Target: target}, nil
	}
	// Explicit centralized selectors need engine management authority, not a
	// forged source label. Their immutable raw selectors still limit execution.
	if len(req.Targets) > 0 || len(scope.CatalogPaths) > 0 || len(scope.RefGroups) > 0 || req.EngineID > 0 {
		root := plugin.FileRootPath(resource.ID)
		if resource.EngineType == "postgresql" {
			root = plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), resource.ID)
		}
		return authorization.MetadataRefreshQuery{Kind: "engine", Target: root}, nil
	}
	return invalid, commonapi.ErrBadRequest
}

func freezeManualRefreshScope(run *execution.TaskExecution, scope *authorization.MetadataRefreshScope) error {
	if scope == nil {
		return nil
	}
	raw, err := json.Marshal(scope)
	if err != nil {
		return err
	}
	// Round through JSON to detach pointers while preserving integer IDs as strings.
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return commonapi.ErrBadRequest
	}
	run.ExecutionConfig["refresh_scope"] = value
	encoded, err := json.Marshal(run.ExecutionConfig)
	if err != nil || len(encoded) > execution.MaxExecutionConfigBytes {
		return commonapi.ErrBadRequest
	}
	return nil
}
