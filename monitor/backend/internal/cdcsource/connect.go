package cdcsource

import (
	"encoding/json"
	"math"
)

// Names and attributes are fixed; raw MBean names never become exported labels.
const taskMBean = "kafka.connect:type=connector-task-metrics,connector=*,task=*"
const streamingMBean = "debezium.postgres:type=connector-metrics,context=streaming,server=*"

var states = []string{"UNASSIGNED", "RUNNING", "PAUSED", "FAILED", "RESTARTING", "STOPPED", "DESTROYED"}

func connect(fetch func(uint16, string, []byte) ([]byte, error)) (map[string]float64, error) {
	raw, err := fetch(8083, "/connectors?expand=status", nil)
	if err != nil {
		return nil, err
	}
	type state struct {
		State string `json:"state"`
	}
	type task struct {
		ID    *int   `json:"id"`
		State string `json:"state"`
	}
	var connectors map[string]struct {
		Status *struct {
			Connector *state `json:"connector"`
			Tasks     []task `json:"tasks"`
		} `json:"status"`
	}
	if decode(raw, &connectors) != nil || connectors == nil || len(connectors) > 256 {
		return nil, ErrUnavailable
	}
	values := map[string]float64{"connectors": float64(len(connectors)), "tasks": 0}
	counts := map[string]map[string]int{"connector": {}, "task": {}}
	for _, kind := range []string{"connector", "task"} {
		for _, s := range states {
			counts[kind][s] = 0
		}
	}
	for name, entry := range connectors {
		if name == "" || entry.Status == nil || entry.Status.Connector == nil || entry.Status.Tasks == nil {
			return nil, ErrUnavailable
		}
		s := entry.Status.Connector.State
		if _, ok := counts["connector"][s]; !ok {
			return nil, ErrUnavailable
		}
		counts["connector"][s]++
		seen := map[int]bool{}
		for _, t := range entry.Status.Tasks {
			if _, ok := counts["task"][t.State]; !ok || t.ID == nil || *t.ID < 0 || seen[*t.ID] {
				return nil, ErrUnavailable
			}
			seen[*t.ID] = true
			counts["task"][t.State]++
			values["tasks"]++
		}
	}
	if values["tasks"] > 1024 {
		return nil, ErrUnavailable
	}
	for kind, counters := range counts {
		for s, n := range counters {
			values[kind+"_states{state=\""+s+"\"}"] = float64(n)
		}
	}
	requests := []struct {
		bean       string
		attributes []string
	}{
		{taskMBean, []string{"offset-commit-failure-percentage"}},
		{streamingMBean, []string{"Connected", "MilliSecondsBehindSource"}},
	}
	for i, request := range requests {
		body, _ := json.Marshal(map[string]any{"type": "read", "mbean": request.bean, "attribute": request.attributes})
		raw, err = fetch(8778, "/jolokia/", body)
		if err != nil {
			return nil, err
		}
		var response struct {
			Status    int                                   `json:"status"`
			Value     map[string]map[string]json.RawMessage `json:"value"`
			ErrorType string                                `json:"error_type"`
		}
		if decode(raw, &response) != nil {
			return nil, ErrUnavailable
		}
		if response.Status == 404 && response.ErrorType == "javax.management.InstanceNotFoundException" && response.Value == nil {
			response.Status, response.Value = 200, map[string]map[string]json.RawMessage{}
		}
		if response.Status != 200 || response.Value == nil || len(response.Value) > 1024 {
			return nil, ErrUnavailable
		}
		if i == 0 {
			values["offset_commit_tasks"] = float64(len(response.Value))
			for _, attrs := range response.Value {
				v, ok := number(attrs["offset-commit-failure-percentage"])
				if !ok || v < 0 || v > 1 {
					return nil, ErrUnavailable
				}
				values["offset_commit_failure_ratio_max"] = math.Max(values["offset_commit_failure_ratio_max"], v)
			}
		} else {
			values["streaming_connectors"] = float64(len(response.Value))
			values["streaming_connected"] = 0
			values["source_delay_connectors"] = 0
			for _, attrs := range response.Value {
				var connected *bool
				if decode(attrs["Connected"], &connected) != nil || connected == nil {
					return nil, ErrUnavailable
				}
				if *connected {
					values["streaming_connected"]++
				}
				v, ok := number(attrs["MilliSecondsBehindSource"])
				if !ok || math.Trunc(v) != v {
					return nil, ErrUnavailable
				}
				// Debezium uses -1 before the first event or after resetting the lag meter.
				if v == -1 {
					continue
				}
				values["source_delay_connectors"]++
				// Clock skew can make the native delay negative. Preserve it, do not clamp to zero.
				previous, exists := values["source_delay_max_seconds"]
				if !exists || v/1000 > previous {
					values["source_delay_max_seconds"] = v / 1000
				}
			}
		}
	}
	return values, nil
}

func number(raw json.RawMessage) (float64, bool) {
	var v *float64
	if decode(raw, &v) != nil || v == nil {
		return 0, false
	}
	return *v, !math.IsNaN(*v) && !math.IsInf(*v, 0)
}
