package engineaccess

import (
	"context"
	"log/slog"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/models"
)

type managerSearchTimingKey struct{}

// ManagerSearchTiming holds synchronous, request-local measurements only.
type ManagerSearchTiming struct {
	candidates, wholeCandidates, engineQueries, ruleItems, ruleQueries int
	preMetaCredential, metadataBinding, metaFacts, engineLookup        time.Duration
	rulePreparation, ruleObservation, observedCredential, ruleQuery    time.Duration
	metaClientCalls                                                    int
	metaClient                                                         client.MetaAuthorizationFactsTiming
}

func ObserveManagerSearchTiming(ctx context.Context) (context.Context, *ManagerSearchTiming) {
	timing := &ManagerSearchTiming{}
	ctx = context.WithValue(ctx, managerSearchTimingKey{}, timing)
	ctx = client.WithMetaAuthorizationFactsTiming(ctx, func(value client.MetaAuthorizationFactsTiming) {
		timing.metaClientCalls++
		timing.metaClient.Preparation += value.Preparation
		timing.metaClient.HTTP += value.HTTP
		timing.metaClient.ResponseRead += value.ResponseRead
		timing.metaClient.StrictJSON += value.StrictJSON
		timing.metaClient.Decode += value.Decode
		timing.metaClient.Validation += value.Validation
		timing.metaClient.Total += value.Total
	})
	return ctx, timing
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
		slog.Int("meta_client_call_count", timing.metaClientCalls),
		slog.Int64("meta_client_prepare_ns", timing.metaClient.Preparation.Nanoseconds()),
		slog.Int64("meta_client_http_ns", timing.metaClient.HTTP.Nanoseconds()),
		slog.Int64("meta_client_read_ns", timing.metaClient.ResponseRead.Nanoseconds()),
		slog.Int64("meta_client_json_ns", timing.metaClient.StrictJSON.Nanoseconds()),
		slog.Int64("meta_client_decode_ns", timing.metaClient.Decode.Nanoseconds()),
		slog.Int64("meta_client_validate_ns", timing.metaClient.Validation.Nanoseconds()),
		slog.Int64("meta_client_total_ns", timing.metaClient.Total.Nanoseconds()),
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
