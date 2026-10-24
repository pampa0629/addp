package resourcequery

import (
	"context"
	"math"
	"net/url"
	"strconv"
	"time"
)

var containerDefinitions = []Definition{
	{"container.cpu.core_equivalents", "cores", 60},
	{"container.memory.current_bytes", "bytes", 0},
	{"container.cpu.limit_percent", "percent", 60},
	{"container.memory.limit_percent", "percent", 0},
}

func NewContainerPlan(count int, start, end, now time.Time, trend bool, b Budget) (Plan, error) {
	if count != 1 || trend || !start.IsZero() || !end.IsZero() {
		return Plan{}, ErrInvalid
	}
	keys := []string{}
	for _, d := range containerDefinitions {
		keys = append(keys, d.Key)
	}
	return newPlan(keys, now.Truncate(time.Second), now.Truncate(time.Second), now, false, nil, b, containerDefinitions)
}

// ContainerObservation reuses the process's complete rate/identity guards and
// the same transport/normalizer; only its two fixed raw cgroup families differ.
func (c *Client) ContainerObservation(ctx context.Context, scope ProcessScope, at time.Time, b Budget) (Summary, error) {
	if c == nil {
		return Summary{}, ErrUnavailable
	}
	if scope.Validate() != nil {
		return Summary{}, ErrInvalid
	}
	plan, err := NewContainerPlan(1, time.Time{}, time.Time{}, at, false, b)
	if err != nil {
		return Summary{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(b.TimeoutSeconds)*time.Second)
	defer cancel()
	result := Summary{Series: Empty(plan, "no_data"), Collection: Collection{State: "collecting"}}
	if scope.Cgroup == nil {
		if scope.Container != nil {
			return Summary{}, ErrInvalid
		}
		result.Collection.State = "not_connected"
		return result, nil
	}
	if scope.Cgroup.Validate() != nil || scope.Container != nil && (scope.Container.Validate(at) != nil || scope.Container.Identity != *scope.Cgroup || scope.Container.ModuleName != scope.ModuleName || scope.Container.Role != scope.Role || scope.StartedAt.Before(scope.Container.StartedAt)) {
		return Summary{}, ErrInvalid
	}
	scope.cgroupQuery = true
	processPlan, err := NewProcessSummaryPlan(1, at, b)
	if err != nil {
		return Summary{}, err
	}
	processPlan.Metrics = processPlan.Metrics[:2]
	expression, err := scope.expression(processPlan)
	if err != nil {
		return Summary{}, err
	}
	series, err := c.queryExpression(ctx, processPlan, expression, b)
	if err != nil {
		return Summary{}, err
	}
	evidence, err := c.read(ctx, "/api/v1/query", url.Values{"query": {scope.collectionExpression()}, "time": {at.Format(time.RFC3339)}, "timeout": {strconv.Itoa(b.TimeoutSeconds) + "s"}, "lookback_delta": {strconv.FormatInt(LookbackSeconds, 10) + "s"}})
	if err != nil {
		return Summary{}, err
	}
	result.Collection, err = normalizeProcessCollection(evidence, at)
	if err != nil {
		return Summary{}, err
	}
	if result.Collection.State != "collecting" {
		return result, nil
	}
	for index := 0; index < 2; index++ {
		result.Series[index] = series[index]
		result.Series[index].MetricKey = containerDefinitions[index].Key
		if index == 1 {
			for _, point := range series[index].Points {
				if point.Value != nil && (*point.Value > 1<<53-1 || math.Trunc(*point.Value) != *point.Value) {
					return Summary{}, ErrUnavailable
				}
			}
		}
	}
	for index := 0; index < 2; index++ {
		ratio := &result.Series[index+2]
		for j, point := range result.Series[index].Points {
			if point.DataState != "valid" || point.Value == nil || scope.Container == nil {
				continue
			}
			proof := scope.Container
			if proof.Validate(at) != nil {
				continue
			}
			bound := float64(0)
			if index == 0 && proof.CPUCores != nil && !proof.StableSince.After(at.Add(-76*time.Second)) {
				bound = *proof.CPUCores
			}
			if index == 1 && proof.MemoryBytes != nil {
				bound = float64(*proof.MemoryBytes)
			}
			if bound <= 0 {
				continue
			}
			value := *point.Value / bound * 100
			if math.IsInf(value, 0) || math.IsNaN(value) {
				return Summary{}, ErrUnavailable
			}
			copy := point
			copy.Value = &value
			ratio.Points[j] = copy
		}
	}
	return result, nil
}
