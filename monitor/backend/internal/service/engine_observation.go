package service

import (
	"context"
	"reflect"
	"time"

	"github.com/addp/common/client"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/google/uuid"
)

type UserEngineDeploymentReader interface {
	GetEngineDeploymentsForUser(context.Context, []string, string) ([]client.EngineDeploymentReference, error)
}
type EngineResourceBackend interface {
	PostgreSQLQuery(context.Context, resourcequery.Plan, resourcequery.PostgreSQLScope, resourcequery.Budget) (resourcequery.Summary, error)
	PostgreSQLSummaries(context.Context, []resourcequery.PostgreSQLScope, time.Time, resourcequery.Budget) (map[string]resourcequery.Summary, error)
}
type EngineObservationDependencies struct {
	Users      UserEngineDeploymentReader
	Identities metricsdiscovery.IdentityReader
	Backend    EngineResourceBackend
}

func (s *ResourceObservationService) WithEngines(deps EngineObservationDependencies) *ResourceObservationService {
	s.engines = deps
	return s
}

type EngineResourceSubject struct {
	Kind         string `json:"kind"`
	DeploymentID string `json:"deployment_id"`
	DisplayName  string `json:"display_name"`
	EngineType   string `json:"engine_type"`
}
type EngineResourceObservation struct {
	Subject    EngineResourceSubject  `json:"subject"`
	NodeID     string                 `json:"node_id"`
	Collection ProcessCollection      `json:"collection"`
	Series     []resourcequery.Series `json:"series"`
}
type EngineResourceResponse struct {
	Data            []EngineResourceObservation `json:"data"`
	QueriedAt       time.Time                   `json:"queried_at"`
	Start           time.Time                   `json:"start"`
	End             time.Time                   `json:"end"`
	StepSeconds     int64                       `json:"step_seconds"`
	PolicyVersion   uint64                      `json:"policy_version"`
	LookbackSeconds int64                       `json:"lookback_seconds"`
}

// EngineResources is the single path for current-page summaries, a complete
// instant observation and trends. Human owner reads precede private discovery,
// and both human authorization and source identity are checked after querying.
func (s *ResourceObservationService) EngineResources(ctx context.Context, principal, token string, ids, keys []string, start, end time.Time, trend, summary bool) (EngineResourceResponse, error) {
	result := EngineResourceResponse{}
	if len(ids) == 0 || len(ids) > 100 || (!summary && len(ids) != 1) || (summary && trend) {
		return result, resourcequery.ErrInvalid
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed == uuid.Nil || parsed.String() != id || wanted[id] {
			return result, resourcequery.ErrInvalid
		}
		wanted[id] = true
	}
	if s == nil || s.engines.Users == nil {
		return result, metricsdiscovery.ErrUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release, err := s.limiter.Acquire(principal, resourcequery.DefaultBudget())
	if err != nil {
		return result, err
	}
	defer release()
	cfg, err := s.Policy(ctx)
	if err != nil {
		return result, err
	}
	ctx, budgetCancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer budgetCancel()
	now := s.now().UTC()
	var plan resourcequery.Plan
	if summary {
		plan, err = resourcequery.NewPostgreSQLSummaryPlan(len(ids), now.Truncate(time.Second), cfg.Budget)
	} else {
		if !trend {
			start = now.Truncate(time.Second)
			end = start
		}
		plan, err = resourcequery.NewPostgreSQLPlan(keys, start, end, now, trend, cfg.Budget)
	}
	if err != nil {
		return result, err
	}
	if err = s.limiter.Check(principal, cfg.Budget); err != nil {
		return result, err
	}
	references, err := s.engines.Users.GetEngineDeploymentsForUser(ctx, ids, token)
	if err != nil {
		return result, err
	}
	if len(references) != len(ids) {
		return result, metricsdiscovery.ErrIdentityUnavailable
	}
	users := map[string]client.EngineDeploymentReference{}
	for _, ref := range references {
		if !wanted[ref.DeploymentID] || users[ref.DeploymentID].DeploymentID != "" || ref.Enabled == nil || ref.Version < 1 || ref.EngineType != "postgresql" {
			return result, metricsdiscovery.ErrIdentityUnavailable
		}
		users[ref.DeploymentID] = ref
	}
	if !s.enabled {
		return result, metricsdiscovery.ErrDisabled
	}
	if s.engines.Identities == nil || s.engines.Backend == nil || s.policy == nil {
		return result, metricsdiscovery.ErrUnconfigured
	}
	current, err := s.currentEngineIdentities(ctx)
	if err != nil {
		return result, err
	}
	scopes := []resourcequery.PostgreSQLScope{}
	physical := map[string]bool{}
	for _, id := range ids {
		ref := users[id]
		state := "not_active"
		if *ref.Enabled && ref.HostBindingState == "bound" {
			state = "not_connected"
			owner, ok := current[id]
			if !ok || owner.Version != ref.Version || owner.NodeID != ref.NodeID {
				return result, metricsdiscovery.ErrIdentityUnavailable
			}
			if owner.Metrics != nil {
				if owner.Metrics.State == "unavailable" {
					state = "source_unavailable"
				}
				if owner.Metrics.State == "available" {
					resolved, err := s.policy.Resolve(ctx, owner.Metrics.Endpoint)
					if err != nil {
						return result, err
					}
					if physical[resolved.Address] {
						return result, resourcequery.ErrUnavailable
					}
					physical[resolved.Address] = true
					scope := resourcequery.PostgreSQLScope{DeploymentID: id, Instance: resolved.Address, Source: *owner.Metrics}
					if scope.Validate() != nil {
						return result, metricsdiscovery.ErrIdentityUnavailable
					}
					if trend {
						plan = resourcequery.LimitTrendStart(plan, *scope.Source.StartedAt)
					}
					scopes = append(scopes, scope)
					state = "no_sample"
				}
			}
		}
		result.Data = append(result.Data, EngineResourceObservation{Subject: EngineResourceSubject{Kind: "engine_deployment", DeploymentID: id, DisplayName: ref.DisplayName, EngineType: ref.EngineType}, NodeID: ref.NodeID, Collection: ProcessCollection{State: state}, Series: resourcequery.Empty(plan, state)})
	}
	observed := map[string]resourcequery.Summary{}
	if len(scopes) > 0 {
		if summary {
			observed, err = s.engines.Backend.PostgreSQLSummaries(ctx, scopes, plan.End, cfg.Budget)
		} else {
			var row resourcequery.Summary
			row, err = s.engines.Backend.PostgreSQLQuery(ctx, plan, scopes[0], cfg.Budget)
			observed[scopes[0].DeploymentID] = row
		}
		if err != nil {
			return EngineResourceResponse{}, err
		}
		if len(observed) != len(scopes) {
			return EngineResourceResponse{}, resourcequery.ErrUnavailable
		}
	}
	// Never return values after a revocation, management change, database
	// restart, source switch or policy update that occurred during the query.
	after, err := s.engines.Users.GetEngineDeploymentsForUser(ctx, ids, token)
	if err != nil {
		return EngineResourceResponse{}, err
	}
	if len(after) != len(references) {
		return EngineResourceResponse{}, metricsdiscovery.ErrIdentityUnavailable
	}
	checked := map[string]bool{}
	for _, ref := range after {
		if !reflect.DeepEqual(ref, users[ref.DeploymentID]) || checked[ref.DeploymentID] {
			return EngineResourceResponse{}, metricsdiscovery.ErrIdentityUnavailable
		}
		checked[ref.DeploymentID] = true
	}
	latest, err := s.currentEngineIdentities(ctx)
	if err != nil {
		return EngineResourceResponse{}, err
	}
	for _, id := range ids {
		if !reflect.DeepEqual(current[id], latest[id]) {
			return EngineResourceResponse{}, metricsdiscovery.ErrIdentityUnavailable
		}
	}
	policy, err := s.Policy(ctx)
	if err != nil {
		return EngineResourceResponse{}, err
	}
	if policy.Version != cfg.Version {
		return EngineResourceResponse{}, resourcequery.ErrConflict
	}
	if ctx.Err() != nil {
		return EngineResourceResponse{}, ctx.Err()
	}
	completed := s.now().UTC()
	if completed.Before(now) {
		return EngineResourceResponse{}, resourcequery.ErrUnavailable
	}
	for i := range result.Data {
		row := &result.Data[i]
		data, ok := observed[row.Subject.DeploymentID]
		if !ok {
			continue
		}
		row.Series = data.Series
		if !trend {
			adapted := ResourceObservationResponse{Series: row.Series, QueriedAt: completed, Collection: &data.Collection}
			markCurrentFreshness(&adapted)
			row.Series = adapted.Series
			row.Collection = ProcessCollection{State: adapted.Collection.State, SampledAt: adapted.Collection.SampledAt}
		}
	}
	result.QueriedAt, result.Start, result.End, result.StepSeconds, result.PolicyVersion, result.LookbackSeconds = completed, plan.Start, plan.End, plan.StepSeconds, cfg.Version, resourcequery.LookbackSeconds
	return result, nil
}

func (s *ResourceObservationService) currentEngineIdentities(ctx context.Context) (map[string]commonmodels.ObservabilityEngineIdentity, error) {
	snapshot, err := s.engines.Identities.GetObservabilityIdentities(ctx)
	now := s.now().UTC()
	if err != nil || snapshot == nil || snapshot.EngineDeployments == nil || len(snapshot.EngineDeployments) > commonmodels.ObservabilityIdentityEngineLimit || snapshot.ObservedAt.Before(now.Add(-metricsdiscovery.Timeout)) || snapshot.ObservedAt.After(now.Add(metricsdiscovery.Timeout)) {
		return nil, metricsdiscovery.ErrIdentityUnavailable
	}
	result := map[string]commonmodels.ObservabilityEngineIdentity{}
	sources := map[commonmodels.ObservabilityEngineSourceKey]bool{}
	nodes := map[string]bool{}
	for _, node := range snapshot.Nodes {
		if node.Version < 1 || nodes[node.NodeID] {
			return nil, metricsdiscovery.ErrIdentityUnavailable
		}
		nodes[node.NodeID] = true
	}
	for _, engine := range snapshot.EngineDeployments {
		if engine.Validate() != nil || !nodes[engine.NodeID] || result[engine.DeploymentID].DeploymentID != "" || sources[engine.SourceKey] || (engine.Metrics != nil && engine.Metrics.StartedAt != nil && engine.Metrics.StartedAt.After(snapshot.ObservedAt)) {
			return nil, metricsdiscovery.ErrIdentityUnavailable
		}
		result[engine.DeploymentID] = engine
		sources[engine.SourceKey] = true
	}
	return result, nil
}
