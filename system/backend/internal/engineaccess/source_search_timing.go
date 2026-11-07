package engineaccess

import (
	"context"
	"log/slog"
	"time"

	"github.com/addp/common/models"
)

type managerSearchTimingKey struct{}

// ManagerSearchTiming holds synchronous, request-local measurements only.
type ManagerSearchTiming struct {
	candidates, wholeCandidates, engineQueries, ruleItems, ruleQueries int
	preMetaCredential, metadataBinding, metaFacts, engineLookup        time.Duration
	rulePreparation, ruleObservation, observedCredential, ruleQuery    time.Duration
}

func ObserveManagerSearchTiming(ctx context.Context) (context.Context, *ManagerSearchTiming) {
	timing := &ManagerSearchTiming{}
	return context.WithValue(ctx, managerSearchTimingKey{}, timing), timing
}

func managerSearchTiming(ctx context.Context) *ManagerSearchTiming {
	timing, _ := ctx.Value(managerSearchTimingKey{}).(*ManagerSearchTiming)
	return timing
}

func (timing *ManagerSearchTiming) LogGroup(routeAuth, requestBinding, response time.Duration) slog.Attr {
	return slog.Group("manager_search",
		slog.Int("candidate_count", timing.candidates),
		slog.Int("whole_candidate_count", timing.wholeCandidates),
		slog.Int("engine_query_count", timing.engineQueries),
		slog.Int("rule_item_count", timing.ruleItems),
		slog.Int("rule_query_count", timing.ruleQueries),
		slog.Int64("route_auth_and_guards_ns", routeAuth.Nanoseconds()),
		slog.Int64("request_binding_ns", requestBinding.Nanoseconds()),
		slog.Int64("pre_meta_auth_ns", timing.preMetaCredential.Nanoseconds()),
		slog.Int64("metadata_binding_ns", timing.metadataBinding.Nanoseconds()),
		slog.Int64("meta_facts_ns", timing.metaFacts.Nanoseconds()),
		slog.Int64("engine_lookup_ns", timing.engineLookup.Nanoseconds()),
		slog.Int64("rule_preparation_ns", timing.rulePreparation.Nanoseconds()),
		slog.Int64("rule_observation_ns", timing.ruleObservation.Nanoseconds()),
		slog.Int64("rule_recheck_auth_ns", timing.observedCredential.Nanoseconds()),
		slog.Int64("rule_query_ns", timing.ruleQuery.Nanoseconds()),
		slog.Int64("response_ns", response.Nanoseconds()),
	)
}

func (r *Repository) metadataBindingEngine(ctx context.Context, tenant, engineID int64) (*models.Engine, error) {
	if timing := managerSearchTiming(ctx); timing != nil {
		started := time.Now()
		defer func() {
			timing.engineQueries++
			timing.engineLookup += time.Since(started)
		}()
	}
	return r.engine(ctx, tenant, engineID, false)
}
