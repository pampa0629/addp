package metricsdiscovery

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/models"
)

func TestGeoPythonProjectionRequiresLiveLeaseAndProtectedSource(t *testing.T) {
	pki := testSourcePKI(t, true)
	var respond atomic.Bool
	respond.Store(true)
	server, port := testSourceServer(t, pki, tls.RequireAndVerifyClientCert, tls.VersionTLS13, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !respond.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "addp_geopython_raster_running 0\n")
	}))
	now := time.Now().UTC()
	snapshot := testSnapshot()
	snapshot.ObservedAt = now
	geo := &models.RuntimeMetricsIdentity{
		RuntimeMetricsDeclaration: models.RuntimeMetricsDeclaration{
			InstanceID: "54a30000-0000-4000-8000-000000000001",
			StartedAt:  now.Add(-time.Minute),
			Endpoint:   server.URL + "/metrics",
		},
		EngineID: 36, LeaseExpiresAt: now.Add(time.Minute),
	}
	geo.Kind = "geopython"
	snapshot.RuntimeMetrics = []models.RuntimeMetricsIdentity{*geo}
	geo = &snapshot.RuntimeMetrics[0]
	policy := testPolicy(t, pki, port)
	projector := NewNodeProjector(true, policy, identityReaderFunc(func(context.Context) (*models.ObservabilityIdentitySnapshot, error) {
		return snapshot, nil
	}))
	projector.now = func() time.Time { return now }
	ctx := context.Background()
	groups, err := projector.Project(ctx, nil)
	if err != nil || len(groups) != 1 || groups[0].Labels["addp_monitor_kind"] != "compute_resources" ||
		groups[0].Labels["addp_runtime_engine_id"] != "36" || groups[0].Labels["addp_instance_id"] != geo.InstanceID {
		t.Fatal(groups, err)
	}
	paused := map[string]bool{RuntimeCollectionScope(geo).ID: true}
	projector.WithCollectionPauses(func(context.Context) (map[string]bool, error) { return paused, nil })
	geo.InstanceID = "54a30000-0000-4000-8000-000000000002"
	if groups, err := projector.Project(ctx, nil); err != nil || len(groups) != 0 {
		t.Fatal("replacement GeoPython instance ignored the engine pause", groups, err)
	}
	delete(paused, RuntimeCollectionScope(geo).ID)
	if groups, err := projector.Project(ctx, nil); err != nil || len(groups) != 1 || groups[0].Labels[CollectionScopeLabel] != RuntimeCollectionScope(geo).ID {
		t.Fatal("GeoPython did not return after resume", groups, err)
	}
	policy.targetLimit = 0
	if _, err := projector.Project(ctx, nil); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatal("GeoPython escaped the shared target budget", err)
	}
	policy.targetLimit = 1
	respond.Store(false)
	if groups, err := projector.Project(ctx, nil); err != nil || len(groups) != 0 {
		t.Fatal("stopped source was admitted", groups, err)
	}
	respond.Store(true)
	geo.LeaseExpiresAt = now
	policy.targetLimit = 0
	if groups, err := projector.Project(ctx, nil); err != nil || len(groups) != 0 {
		t.Fatal("expired lease was reserved or published", groups, err)
	}
}
