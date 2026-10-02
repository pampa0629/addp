package service

import (
	"fmt"
	"github.com/addp/common/logpipeline"
	"github.com/addp/monitor/internal/models"
	"strings"
	"time"
)

func defaultLogPolicy() models.LogPipelinePolicy {
	return models.LogPipelinePolicy{ID: 1, Version: 1, FailureSamples: 3, RecoverySamples: 3, StaleSeconds: 120, DelayMS: 15000, CapacityPercent: 80, RecoveryPercent: 70}
}
func validLogPolicy(p models.LogPipelinePolicy) bool {
	return p.Version > 0 && p.FailureSamples >= 1 && p.FailureSamples <= 10 && p.RecoverySamples >= 1 && p.RecoverySamples <= 10 && p.StaleSeconds >= 90 && p.StaleSeconds <= 600 && p.DelayMS >= 1000 && p.DelayMS <= 20000 && p.CapacityPercent >= 50 && p.CapacityPercent <= 95 && p.RecoveryPercent >= 20 && p.RecoveryPercent < p.CapacityPercent
}

type logEvidence struct {
	valid, bad, immediate bool
	severity, instance    string
}

func stepLogSignal(old models.LogSignalState, e logEvidence, p models.LogPipelinePolicy) models.LogSignalState {
	old.InstanceID = e.instance
	if !e.valid {
		old.Failures = 0
		old.Successes = 0
		return old
	}
	if e.bad {
		old.Successes = 0
		old.Failures++
		if old.Failures >= p.FailureSamples || e.immediate {
			old.Active = true
			old.Severity = e.severity
		}
	} else {
		old.Failures = 0
		old.Successes++
		if old.Successes >= p.RecoverySamples {
			old.Active = false
		}
	}
	return old
}

// evaluateLogObservation uses only valid comparable evidence. Unknown data cannot resolve alarms.
func evaluateLogObservation(node *models.LogPipelineNode, obs logpipeline.Observation, p models.LogPipelinePolicy, active map[string]string, registryValid bool, now time.Time) {
	prev := node.Observation
	comparable := node.ReceivedAt != nil && prev.BootID == obs.BootID && now.Sub(*node.ReceivedAt) <= time.Duration(p.StaleSeconds)*time.Second
	if node.Signals == nil {
		node.Signals = map[string]models.LogSignalState{}
	}
	evidence := map[string]logEvidence{
		"observation_missing": {valid: true, severity: "critical"},
		"log_api":             {valid: true, bad: !obs.APIReady, severity: "critical"},
		"delivery_probe":      {valid: true, bad: !obs.ProbeDelivered, severity: "critical"},
		"delivery_delay":      {valid: obs.ProbeDelivered, bad: obs.ProbeDelayMS > p.DelayMS, severity: "warning"},
		"collector_metrics":   {valid: true, bad: !obs.Collector.Valid, severity: "critical"},
		"source_observation":  {valid: true, bad: !obs.SourcesValid, severity: "critical"},
		"housekeeping":        {valid: true, bad: !obs.HousekeepingValid, severity: "warning"},
		"source_quota":        {valid: obs.HousekeepingValid, bad: obs.QuotaExhausted, severity: "critical"},
	}
	capacityBad := false
	if obs.SourceLimitBytes > 0 {
		threshold := p.CapacityPercent
		if node.Signals["source_capacity"].Active {
			threshold = p.RecoveryPercent
		}
		capacityBad = float64(obs.SourceBytes)/float64(obs.SourceLimitBytes)*100 >= float64(threshold)
	}
	evidence["source_capacity"] = logEvidence{valid: obs.SourcesValid, bad: capacityBad, severity: "warning"}
	sameCollector := comparable && obs.Collector.Valid && prev.Collector.Valid && obs.Collector.StartedAt == prev.Collector.StartedAt
	evidence["collector_dropped"] = logEvidence{valid: sameCollector && obs.Collector.Dropped >= prev.Collector.Dropped, bad: obs.Collector.Dropped > prev.Collector.Dropped, immediate: true, severity: "critical"}
	evidence["collector_retries"] = logEvidence{valid: sameCollector && obs.Collector.Retries >= prev.Collector.Retries, bad: obs.Collector.Retries > prev.Collector.Retries, severity: "warning"}
	previous := map[string]logpipeline.Receiver{}
	for _, r := range prev.Receivers {
		previous[r.InstanceID] = r
	}
	current := map[string]logpipeline.Receiver{}
	for _, r := range obs.Receivers {
		current[r.InstanceID] = r
	}
	if registryValid {
		for id, module := range active {
			r, exists := current[id]
			exists = exists && r.Module == module && now.Sub(r.ObservedAt) <= time.Duration(p.StaleSeconds)*time.Second
			key := fmt.Sprintf("receiver_missing:%s", id)
			evidence[key] = logEvidence{valid: obs.SourcesValid, bad: !exists, severity: "critical", instance: id}
			prior, seen := previous[id]
			valid := exists && seen && comparable && r.Dropped >= prior.Dropped && r.WriteFailures >= prior.WriteFailures
			evidence["receiver_loss:"+id] = logEvidence{valid: valid, bad: r.Dropped > prior.Dropped || r.WriteFailures > prior.WriteFailures, immediate: true, severity: "critical", instance: id}
		}
	}
	earlyValid := comparable && obs.SourcesValid && prev.SourcesValid && obs.HousekeepingValid && prev.HousekeepingValid
	earlyBad := obs.HousekeepingAt.After(prev.HousekeepingAt) && obs.EarlyCleaned > 0
	for id, r := range current {
		if prior, exists := previous[id]; exists && r.EarlyCleaned >= prior.EarlyCleaned && r.EarlyCleaned > prior.EarlyCleaned {
			earlyBad = true
		}
	}
	evidence["source_early_cleaning"] = logEvidence{valid: earlyValid, bad: earlyBad, immediate: true, severity: "warning"}
	for key := range node.Signals {
		instance := receiverLogSignalInstance(key)
		if instance != "" && registryValid {
			if _, ok := active[instance]; !ok {
				evidence[key] = logEvidence{valid: true, instance: instance}
			}
		}
	}
	for key, old := range node.Signals {
		if _, ok := evidence[key]; !ok {
			evidence[key] = logEvidence{severity: old.Severity, instance: old.InstanceID}
		}
	}
	for key, e := range evidence {
		e.instance = receiverLogSignalInstance(key)
		node.Signals[key] = stepLogSignal(node.Signals[key], e, p)
	}
	node.Observation = obs
	node.RegistryValid = registryValid
	node.ReceivedAt = &now
}

func receiverLogSignalInstance(signal string) string {
	kind, instance, found := strings.Cut(signal, ":")
	if found && (kind == "receiver_missing" || kind == "receiver_loss") {
		return instance
	}
	return ""
}
