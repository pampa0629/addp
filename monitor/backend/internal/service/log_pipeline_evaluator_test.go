package service

import (
	"github.com/addp/common/logpipeline"
	"github.com/addp/monitor/internal/models"
	"testing"
	"time"
)

func healthyLogObservation(now time.Time, seq uint64) logpipeline.Observation {
	return logpipeline.Observation{Schema: logpipeline.Schema, Node: "test-node", BootID: "boot-a", Sequence: seq, SampledAt: now, APIReady: true, ProbeDelivered: true, ProbeDelayMS: 10, Collector: logpipeline.Collector{Valid: true, StartedAt: 123, Sent: 100}, SourcesValid: true, SourceLimitBytes: 1000, HousekeepingValid: true, HousekeepingAt: now}
}

func TestLogPipelineHealthShowsFreshFailureAndStaleUnknown(t *testing.T) {
	now := time.Now().UTC()
	node := models.LogPipelineNode{ReceivedAt: &now, RegistryValid: true, Observation: healthyLogObservation(now, 1)}
	p := defaultLogPolicy()
	if got := logPipelineHealth(node, p, 0, now); got != "healthy" {
		t.Fatalf("healthy evidence: %s", got)
	}
	node.Observation.APIReady = false
	if got := logPipelineHealth(node, p, 1, now); got != "alert" {
		t.Fatalf("known log API incident hidden: %s", got)
	}
	if got := logPipelineHealth(node, p, 1, now.Add(121*time.Second)); got != "unknown" {
		t.Fatalf("expired evidence reported current health: %s", got)
	}
	node.Observation = healthyLogObservation(now, 1)
	node.Observation.Collector.Valid = false
	if got := logPipelineHealth(node, p, 0, now); got != "unknown" {
		t.Fatalf("missing metrics reported healthy: %s", got)
	}
}

func TestLogPipelineCounterBaselinesAndUnknownRecovery(t *testing.T) {
	now := time.Now().UTC()
	p := defaultLogPolicy()
	node := models.LogPipelineNode{}
	o := healthyLogObservation(now, 1)
	o.Collector.Dropped = 700
	evaluateLogObservation(&node, o, p, nil, true, now)
	if node.Signals["collector_dropped"].Active {
		t.Fatal("historical counter became a new drop")
	}
	o.Sequence++
	o.SampledAt = now.Add(30 * time.Second)
	o.Collector.Dropped++
	evaluateLogObservation(&node, o, p, nil, true, o.SampledAt)
	if !node.Signals["collector_dropped"].Active {
		t.Fatal("new drop not detected")
	}
	for n := 0; n < 4; n++ {
		o.Sequence++
		o.SampledAt = o.SampledAt.Add(30 * time.Second)
		o.Collector.Valid = false
		evaluateLogObservation(&node, o, p, nil, true, o.SampledAt)
	}
	if !node.Signals["collector_dropped"].Active {
		t.Fatal("missing metrics resolved drop alert")
	}
	o.Collector.Valid = true
	o.Collector.StartedAt++
	o.Collector.Dropped = 0
	o.Sequence++
	o.SampledAt = o.SampledAt.Add(30 * time.Second)
	evaluateLogObservation(&node, o, p, nil, true, o.SampledAt)
	if !node.Signals["collector_dropped"].Active {
		t.Fatal("restart alone resolved alert")
	}
	for n := 0; n < 3; n++ {
		o.Sequence++
		o.SampledAt = o.SampledAt.Add(30 * time.Second)
		evaluateLogObservation(&node, o, p, nil, true, o.SampledAt)
	}
	if node.Signals["collector_dropped"].Active {
		t.Fatal("valid zero deltas did not resolve alert")
	}
}
func TestLogPipelineReceiverNeedsCurrentRegistryEvidence(t *testing.T) {
	now := time.Now().UTC()
	p := defaultLogPolicy()
	node := models.LogPipelineNode{}
	o := healthyLogObservation(now, 1)
	o.Receivers = []logpipeline.Receiver{{Module: "copilot", InstanceID: "instance", ObservedAt: now, Dropped: 100}}
	for n := 0; n < 3; n++ {
		o.Sequence++
		o.SampledAt = now.Add(time.Duration(n) * 30 * time.Second)
		evaluateLogObservation(&node, o, p, map[string]string{"instance": "copilot"}, true, o.SampledAt)
	}
	if node.Signals["receiver_loss:instance"].Active {
		t.Fatal("existing drop falsely opened incident")
	}
	if node.Signals["receiver_loss:instance"].InstanceID != "instance" {
		t.Fatal("receiver identity lost while counter evidence is incomparable")
	}
	for n := 0; n < 3; n++ {
		o.Sequence++
		o.SampledAt = o.SampledAt.Add(30 * time.Second)
		o.Receivers = nil
		evaluateLogObservation(&node, o, p, map[string]string{"instance": "copilot"}, true, o.SampledAt)
	}
	if !node.Signals["receiver_missing:instance"].Active {
		t.Fatal("online receiver missing not detected")
	}
	for n := 0; n < 3; n++ {
		o.Sequence++
		o.SampledAt = o.SampledAt.Add(30 * time.Second)
		evaluateLogObservation(&node, o, p, nil, false, o.SampledAt)
	}
	if !node.Signals["receiver_missing:instance"].Active {
		t.Fatal("unavailable registry resolved missing receiver")
	}
	for n := 0; n < 3; n++ {
		o.Sequence++
		o.SampledAt = o.SampledAt.Add(30 * time.Second)
		evaluateLogObservation(&node, o, p, nil, true, o.SampledAt)
	}
	if node.Signals["receiver_missing:instance"].Active {
		t.Fatal("ended instance cannot resolve missing receiver")
	}
}
func TestLogPipelineCapacityHysteresisAndProbeFailure(t *testing.T) {
	now := time.Now().UTC()
	p := defaultLogPolicy()
	node := models.LogPipelineNode{}
	o := healthyLogObservation(now, 1)
	o.SourceBytes = 850
	o.ProbeDelivered = false
	sample := func() {
		o.Sequence++
		o.SampledAt = o.SampledAt.Add(30 * time.Second)
		evaluateLogObservation(&node, o, p, nil, true, o.SampledAt)
	}
	for n := 0; n < 3; n++ {
		sample()
	}
	if !node.Signals["source_capacity"].Active || !node.Signals["delivery_probe"].Active {
		t.Fatal("sustained failure not detected")
	}
	o.SourceBytes = 750
	for n := 0; n < 3; n++ {
		sample()
	}
	if !node.Signals["source_capacity"].Active {
		t.Fatal("capacity flapped inside hysteresis")
	}
	o.SourceBytes = 650
	o.ProbeDelivered = true
	for n := 0; n < 3; n++ {
		sample()
	}
	if node.Signals["source_capacity"].Active || node.Signals["delivery_probe"].Active {
		t.Fatal("valid recovery not detected")
	}
}

func TestLogPipelineEarlyCleaningUsesNewEvidenceAndRecovers(t *testing.T) {
	now := time.Now().UTC()
	node := models.LogPipelineNode{}
	p := defaultLogPolicy()
	obs := healthyLogObservation(now, 1)
	obs.EarlyCleaned = 4
	evaluateLogObservation(&node, obs, p, nil, true, now)
	if node.Signals["source_early_cleaning"].Active {
		t.Fatal("first retained cleanup observation is not a new event")
	}
	obs.Sequence++
	obs.SampledAt = now.Add(30 * time.Second)
	obs.HousekeepingAt = obs.SampledAt
	evaluateLogObservation(&node, obs, p, nil, true, obs.SampledAt)
	if !node.Signals["source_early_cleaning"].Active {
		t.Fatal("new housekeeping cleanup risk was not detected")
	}
	for i := 0; i < 3; i++ {
		obs.Sequence++
		obs.SampledAt = obs.SampledAt.Add(30 * time.Second)
		obs.HousekeepingValid = false
		evaluateLogObservation(&node, obs, p, nil, true, obs.SampledAt)
	}
	if !node.Signals["source_early_cleaning"].Active {
		t.Fatal("unknown cleanup evidence resolved incident")
	}
	for i := 0; i < 4; i++ {
		obs.Sequence++
		obs.SampledAt = obs.SampledAt.Add(30 * time.Second)
		obs.HousekeepingAt = obs.SampledAt
		obs.HousekeepingValid = true
		obs.EarlyCleaned = 0
		evaluateLogObservation(&node, obs, p, nil, true, obs.SampledAt)
	}
	if node.Signals["source_early_cleaning"].Active {
		t.Fatal("valid no-cleanup samples did not resolve incident")
	}
}
