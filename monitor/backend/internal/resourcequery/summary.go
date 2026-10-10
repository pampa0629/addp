package resourcequery

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func SummaryMetrics() []string {
	return []string{"node.cpu.busy_percent", "node.memory.used_percent"}
}

func NewSummaryPlan(count int, at time.Time, b Budget) (Plan, error) {
	if count < 1 || count > 100 {
		return Plan{}, ErrInvalid
	}
	p, err := NewPlan(SummaryMetrics(), at, at, at, false, nil, b)
	if err == nil && (count*len(p.Metrics) > b.MaxSeries || count*len(p.Metrics) > b.MaxTotalPoints) {
		err = ErrBudget
	}
	return p, err
}

type Summary struct {
	Collection Collection
	Series     []Series
}

// Summaries shares the scalar and collection formulas and their normalization.
// Two fixed requests cover all authorized active scopes, never one per host.
func (c *Client) Summaries(ctx context.Context, scopes []Scope, at time.Time, b Budget) (map[string]Summary, error) {
	p, err := NewSummaryPlan(len(scopes), at, b)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(b.TimeoutSeconds)*time.Second)
	defer cancel()
	metrics, collections := []string{}, []string{}
	ids := map[string]bool{}
	for _, scope := range scopes {
		if scope.Validate() != nil || ids[scope.NodeID] {
			return nil, ErrInvalid
		}
		ids[scope.NodeID] = true
		expression, err := p.Expression(scope)
		if err != nil {
			return nil, err
		}
		label := func(expression string) string {
			return `label_replace((` + expression + `),"addp_summary_node",` + strconv.Quote(scope.NodeID) + `,"","")`
		}
		metrics = append(metrics, label(expression))
		collections = append(collections, label(collectionExpression(scope)))
	}
	read := func(parts []string, bound int) (map[string]envelope, error) {
		form := url.Values{"query": {strings.Join(parts, " or ")}, "time": {at.Format(time.RFC3339)}, "timeout": {strconv.Itoa(b.TimeoutSeconds) + "s"}, "lookback_delta": {strconv.FormatInt(LookbackSeconds, 10) + "s"}}
		// Prometheus accepts read-only queries as form POST, avoiding URI limits.
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.origin+"/api/v1/query", strings.NewReader(form.Encode()))
		if err != nil {
			return nil, ErrUnavailable
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		data, err := c.readRequest(ctx, req)
		if err != nil {
			return nil, err
		}
		return splitSummaryEvidence(data, ids, bound)
	}
	data, err := read(metrics, len(p.Metrics)*2)
	if err != nil {
		return nil, err
	}
	evidence, err := read(collections, 9)
	if err != nil {
		return nil, err
	}
	result := make(map[string]Summary, len(scopes))
	for _, scope := range scopes {
		series, err := normalize(data[scope.NodeID], p, b)
		if err != nil {
			return nil, err
		}
		collection, err := normalizeCollection(evidence[scope.NodeID], at)
		if err != nil {
			return nil, err
		}
		result[scope.NodeID] = Summary{Collection: collection, Series: series}
	}
	return result, nil
}

func splitSummaryEvidence(data envelope, ids map[string]bool, bound int) (map[string]envelope, error) {
	if data.Data.ResultType != "vector" || data.Data.Result == nil {
		return nil, ErrUnavailable
	}
	if len(data.Data.Result) > len(ids)*bound {
		return nil, ErrBudget
	}
	groups := make(map[string]envelope, len(ids))
	for id := range ids {
		group := envelope{}
		group.Data.ResultType = "vector"
		group.Data.Result = []wireSeries{}
		groups[id] = group
	}
	for _, row := range data.Data.Result {
		id := row.Metric["addp_summary_node"]
		if !ids[id] {
			return nil, ErrUnavailable
		}
		labels := make(map[string]string, len(row.Metric)-1)
		for key, value := range row.Metric {
			if key != "addp_summary_node" {
				labels[key] = value
			}
		}
		row.Metric = labels
		group := groups[id]
		group.Data.Result = append(group.Data.Result, row)
		if len(group.Data.Result) > bound {
			return nil, ErrBudget
		}
		groups[id] = group
	}
	return groups, nil
}
