package resourcequery

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/addp/monitor/internal/metricsdiscovery"
)

// ActiveCollectionTargets contains only the safe evidence needed for the
// collection configuration page. Raw Prometheus addresses and errors stay private.
type ActiveCollectionTargets struct {
	Complete bool
	Scopes   map[string][]time.Time // each active target; zero means no fresh successful scrape
}

func (c *Client) ActiveCollectionTargets(ctx context.Context, at time.Time, limit int) (ActiveCollectionTargets, error) {
	if c == nil {
		return ActiveCollectionTargets{}, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.origin+"/api/v1/targets?state=active&scrapePool=addp_nodes", nil)
	if err != nil {
		return ActiveCollectionTargets{}, ErrUnavailable
	}
	body, status, err := c.readJSONBody(ctx, req)
	if err != nil {
		return ActiveCollectionTargets{}, err
	}
	var wire struct {
		Status string `json:"status"`
		Data   struct {
			Active []struct {
				ScrapePool string            `json:"scrapePool"`
				Discovered map[string]string `json:"discoveredLabels"`
				Health     string            `json:"health"`
				LastScrape time.Time         `json:"lastScrape"`
			} `json:"activeTargets"`
		} `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &wire) != nil || wire.Status != "success" || wire.Data.Active == nil {
		return ActiveCollectionTargets{}, ErrUnavailable
	}
	if len(wire.Data.Active) > limit {
		return ActiveCollectionTargets{}, ErrBudget
	}
	result := ActiveCollectionTargets{Complete: true, Scopes: make(map[string][]time.Time)}
	for _, target := range wire.Data.Active {
		if target.ScrapePool != "addp_nodes" || target.Discovered == nil {
			return ActiveCollectionTargets{}, ErrUnavailable
		}
		id := target.Discovered[metricsdiscovery.CollectionScopeLabel]
		switch id {
		case metricsdiscovery.UnmanagedCollectionScope:
			continue
		case "":
			switch target.Discovered["addp_monitor_kind"] {
			case "process_resources", "search_resources", "compute_resources":
				// Targets retained from a previous HTTP SD snapshot without
				// scope labels cannot prove that a paused scope is absent.
				result.Complete = false
			}
			continue
		}
		var sampled time.Time
		if target.Health == "up" && !target.LastScrape.Before(at.Add(-30*time.Second)) && !target.LastScrape.After(at.Add(5*time.Second)) {
			sampled = target.LastScrape.UTC()
		}
		result.Scopes[id] = append(result.Scopes[id], sampled)
	}
	return result, nil
}
