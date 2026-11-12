package service

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/production"
	"github.com/addp/develop/backend/internal/repository"
)

type privateProductionReader interface {
	ReadPrivateProduction(context.Context, uint, string, string) (*repository.PrivateFileProduction, error)
}

type ProductionReceiptService struct{ repository privateProductionReader }

func NewProductionReceiptService(repository privateProductionReader) *ProductionReceiptService {
	return &ProductionReceiptService{repository: repository}
}

// ReadCommittedProduction projects persisted Owner evidence only. Config and
// DAG select contributing nodes; they never manufacture an actual ReadSet.
func (s *ProductionReceiptService) ReadCommittedProduction(ctx context.Context, tenant uint, ref production.Reference) (*production.Receipt, error) {
	if tenant == 0 || ref.Validate() != nil || ref.Owner != "develop" {
		return nil, production.ErrEvidence
	}
	original, err := s.repository.ReadPrivateProduction(ctx, tenant, ref.ExecutionID, ref.ResultID)
	if err != nil {
		return nil, err
	}
	facts := &original.Facts
	workflow, err := fileWorkflowRuntimeDefinition(facts)
	if err != nil || facts.Intent.Target == nil || facts.BoundScope == nil || facts.BoundScope.Target == nil || original.OperationID != ref.ResultID || facts.ExecutionID != ref.ExecutionID || facts.TenantID != strconv.FormatUint(uint64(tenant), 10) {
		return nil, production.ErrEvidence
	}
	var definition map[string]interface{}
	if json.Unmarshal([]byte(workflow), &definition) != nil {
		return nil, production.ErrEvidence
	}
	tasks, _ := workflowTasksFromInterface(definition["tasks"])
	byID := make(map[string]map[string]interface{}, len(tasks))
	for _, task := range tasks {
		byID[task["id"].(string)] = task
	}
	direct := make(map[string]bool, len(facts.Intent.Sources))
	for _, source := range facts.Intent.Sources {
		direct[source.NodeID] = true
	}
	dependencies, valid := workflowDataSources(byID, direct, []string{facts.Intent.Target.NodeID})
	if !valid {
		return nil, production.ErrEvidence
	}
	sources := production.SourceEvidence{Kind: production.SourcesUnknown}
	if original.SourceReads != nil {
		if len(original.SourceReads) != len(facts.Intent.Sources) {
			return nil, production.ErrEvidence
		}
		reads := make(map[string]repository.PrivateFileSourceRead, len(original.SourceReads))
		allVersionsKnown := true
		for i, source := range facts.Intent.Sources {
			read := original.SourceReads[i]
			if read.NodeID != source.NodeID || read.ReadSet == nil || len(read.ReadSet.Paths) != 1 || !reflect.DeepEqual(read.ReadSet.Paths[0], source.Target) {
				return nil, production.ErrEvidence
			}
			if read.EngineVersion <= 0 {
				allVersionsKnown = false
			}
			reads[source.NodeID] = read
		}
		var paths []plugin.EngineCatalogPath
		versions := map[int64]int64{}
		completeVersions := true
		for _, node := range dependencies[facts.Intent.Target.NodeID] {
			read := reads[node]
			paths = append(paths, read.ReadSet.Paths...)
			id := int64(read.ReadSet.Paths[0].EngineID)
			if read.EngineVersion <= 0 {
				completeVersions = false
			}
			if prior, exists := versions[id]; exists && prior != read.EngineVersion {
				return nil, production.ErrEvidence
			}
			versions[id] = read.EngineVersion
		}
		if len(paths) == 0 {
			if allVersionsKnown {
				sources.Kind = production.SourcesNone
			}
		} else {
			set, err := plugin.NewQueryReadSet(paths...)
			if err != nil {
				return nil, err
			}
			if completeVersions {
				frozen := make([]production.EngineVersion, 0, len(versions))
				for id, version := range versions {
					frozen = append(frozen, production.EngineVersion{EngineID: id, Version: version})
				}
				sort.Slice(frozen, func(i, j int) bool { return frozen[i].EngineID < frozen[j].EngineID })
				sources = production.SourceEvidence{Kind: production.SourcesDataItems, ReadSet: set, EngineVersions: frozen}
			}
		}
	}
	version, _ := strconv.ParseInt(facts.BoundScope.Target.EngineVersion, 10, 64)
	principal, _ := strconv.ParseInt(facts.Actor.PrincipalID, 10, 64)
	membership, _ := strconv.ParseInt(facts.Actor.TenantMembershipID, 10, 64)
	authorizationVersion, _ := strconv.ParseInt(facts.Actor.AuthorizationVersion, 10, 64)
	result := &production.Receipt{Reference: ref, TenantID: int64(tenant), TargetEngineVersion: version,
		RetryOfExecutionID: original.RetryOfExecutionID, Attempt: original.Attempt, OperationID: original.OperationID,
		CommittedAt: original.CommittedAt, Origin: &production.UserOrigin{PrincipalID: principal, MembershipID: membership, AuthorizationVersion: authorizationVersion},
		Primary: facts.Intent.Target.Target, Objects: []production.CommittedObject{{Target: facts.Intent.Target.Target, Created: true}}, Sources: sources}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return result, nil
}
