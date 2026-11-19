package resourcequery

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"
)

type ProcessMetricTotal struct {
	Value *float64 `json:"value"`
	Valid int      `json:"valid"`
}

type ProcessTotals struct {
	CPU ProcessMetricTotal `json:"cpu"`
	RSS ProcessMetricTotal `json:"rss"`
}

func (c *Client) ProcessResourceTotals(ctx context.Context, scopes []ProcessScope, modules int, at time.Time, b Budget) (map[string]ProcessTotals, error) {
	if b.Validate() != nil {
		return nil, ErrInvalid
	}
	if b.MaxMetrics < 2 || modules*2 > b.MaxSeries || modules*2 > b.MaxTotalPoints {
		return nil, ErrBudget
	}
	if c == nil {
		return nil, ErrUnavailable
	}
	result := make(map[string]ProcessTotals)
	if len(scopes) == 0 {
		return result, nil
	}
	expected := make(map[string]int)
	membership := make(map[string]bool)
	for _, scope := range scopes {
		if scope.Validate() != nil || membership[scope.InstanceID] {
			return nil, ErrInvalid
		}
		membership[scope.InstanceID] = true
		expected[scope.ModuleName]++
	}
	for index, definition := range processDefinitions[:2] {
		expression := processTotalsExpression(scopes, definition.Key)
		endpoint, args := planExpressionQuery(Plan{End: at}, expression, b)
		data, err := c.readForm(ctx, endpoint, args)
		if err != nil {
			return nil, err
		}
		ids := make(map[string]bool, len(expected))
		for module := range expected {
			ids[module] = true
		}
		groups, err := splitSummaryEvidence(data, ids, 2, "module")
		if err != nil {
			return nil, err
		}
		for module, group := range groups {
			signals, err := normalizeSignals(group, at, map[string]bool{"sum": true, "count": true})
			if err != nil {
				return nil, err
			}
			if len(signals) == 0 {
				continue
			}
			total, count := signals["sum"], signals["count"]
			if len(signals) != 2 || count < 1 || count > float64(expected[module]) || math.Trunc(count) != count {
				return nil, ErrUnavailable
			}
			row := result[module]
			if index == 0 {
				row.CPU = ProcessMetricTotal{Value: &total, Valid: int(count)}
			} else {
				row.RSS = ProcessMetricTotal{Value: &total, Valid: int(count)}
			}
			result[module] = row
		}
	}
	return result, nil
}

func processTotalsExpression(scopes []ProcessScope, metric string) string {
	parts := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		// Caller uses the two fixed process definitions and verified identities.
		value, _, _ := scope.metricValues(metric, true)
		parts = append(parts, `label_replace(label_replace((`+value+`),"module",`+strconv.Quote(scope.ModuleName)+`,"",""),"runtime",`+strconv.Quote(scope.InstanceID)+`,"","")`)
	}
	// Union once per result signal, preserving the per-instance labels until
	// the server has computed the per-module sum and coverage.
	set := strings.Join(parts, " or ")
	return `label_replace(sum by(module)(` + set + `),"signal","sum","","") or ` +
		`label_replace(count by(module)(` + set + `),"signal","count","","")`
}
