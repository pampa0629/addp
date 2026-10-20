package service

import (
	"context"
	"time"

	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"
)

type HTTPResourceBackend interface {
	HTTP(context.Context, resourcequery.HTTPPlan, resourcequery.ProcessScope, resourcequery.Budget) ([]resourcequery.HTTPObservation, error)
	HTTPCollection(context.Context, resourcequery.ProcessScope, time.Time, resourcequery.Budget) (resourcequery.Collection, error)
}

type HTTPObservationRequest struct {
	IDs           []uint
	WindowSeconds int
	Grouped       bool
	Route         *string
	Method        string
	Start, End    time.Time
	Trend         bool
	Sort          string
}

type HTTPProcessObservation struct {
	Subject       ProcessResourceSubject          `json:"subject"`
	Collection    ProcessCollection               `json:"collection"`
	PolicyVersion uint64                          `json:"policy_version"`
	QueriedAt     time.Time                       `json:"queried_at"`
	Observations  []resourcequery.HTTPObservation `json:"observations"`
}

type HTTPObservationResponse struct {
	Data          []HTTPProcessObservation `json:"data"`
	WindowSeconds int                      `json:"window_seconds"`
	Start         time.Time                `json:"start"`
	End           time.Time                `json:"end"`
	StepSeconds   int64                    `json:"step_seconds"`
	RankingLimit  int                      `json:"ranking_limit"`
}

func (s *ResourceObservationService) HTTP(ctx context.Context, principal, token string, request HTTPObservationRequest) (HTTPObservationResponse, error) {
	response := HTTPObservationResponse{Data: []HTTPProcessObservation{}, WindowSeconds: request.WindowSeconds}
	if request.Grouped {
		if request.Sort == "" {
			request.Sort = "mean"
		}
		if request.Sort != "mean" && request.Sort != "qps" {
			return response, resourcequery.ErrInvalid
		}
	} else if request.Sort != "" {
		return response, resourcequery.ErrInvalid
	}
	if s == nil {
		return response, metricsdiscovery.ErrUnconfigured
	}
	anchor := s.now().UTC().Truncate(time.Second)
	checkStart, checkEnd := request.Start, request.End
	if !request.Trend {
		checkStart, checkEnd = anchor, anchor
	}
	if _, err := resourcequery.NewHTTPPlan(checkStart, checkEnd, anchor, request.Trend, request.Grouped, request.Route, request.Method, request.WindowSeconds, resourcequery.DefaultBudget()); err != nil {
		return response, err
	}
	backend, ok := s.process.Backend.(HTTPResourceBackend)
	if !ok || backend == nil {
		return response, metricsdiscovery.ErrUnconfigured
	}
	if (request.Grouped || request.Trend || request.Route != nil) && len(request.IDs) != 1 {
		return response, resourcequery.ErrInvalid
	}
	var httpPlan resourcequery.HTTPPlan
	planFor := func(count int, start, end, now time.Time, trend bool, b resourcequery.Budget) (resourcequery.Plan, error) {
		if !trend {
			start, end = now.Truncate(time.Second), now.Truncate(time.Second)
		}
		var err error
		httpPlan, err = resourcequery.NewHTTPPlan(start, end, now, trend, request.Grouped, request.Route, request.Method, request.WindowSeconds, b)
		httpPlan.Rank = request.Sort
		if err == nil && request.Grouped {
			response.RankingLimit = httpPlan.SeriesUpperBound / len(httpPlan.Metrics)
			if response.RankingLimit > 10 {
				response.RankingLimit = 10
			}
		}
		if err == nil && (count*httpPlan.SeriesUpperBound > b.MaxSeries || count*httpPlan.SeriesUpperBound*httpPlan.Points > b.MaxTotalPoints) {
			err = resourcequery.ErrBudget
		}
		return httpPlan.Plan, err
	}
	data := map[uint][]resourcequery.HTTPObservation{}
	read := func(ctx context.Context, plan resourcequery.Plan, scopes []resourcequery.ProcessScope, b resourcequery.Budget) (map[uint]resourcequery.Summary, error) {
		httpPlan.Plan = plan
		result := map[uint]resourcequery.Summary{}
		for _, scope := range scopes {
			if scope.Role != "backend" && scope.Role != "ingress" {
				return nil, resourcequery.ErrInvalid
			}
			collection, err := backend.HTTPCollection(ctx, scope, anchor, b)
			if err != nil {
				return nil, err
			}
			rows := []resourcequery.HTTPObservation{}
			if collection.State == "collecting" {
				rows, err = backend.HTTP(ctx, httpPlan, scope, b)
				if err != nil {
					return nil, err
				}
			}
			data[scope.ID] = rows
			result[scope.ID] = resourcequery.Summary{Collection: collection, Series: []resourcequery.Series{}}
		}
		return result, nil
	}
	current, plan, err := s.observeProcessesUsing(ctx, principal, token, request.IDs, request.Start, request.End, request.Trend, planFor, read)
	if err != nil {
		return response, err
	}
	response.Start, response.End, response.StepSeconds = plan.Start, plan.End, plan.StepSeconds
	for _, row := range current.Data {
		observations := data[row.Subject.ID]
		if observations == nil || row.Collection.State != "collecting" {
			observations = []resourcequery.HTTPObservation{}
		}
		response.Data = append(response.Data, HTTPProcessObservation{Subject: row.Subject, Collection: row.Collection, PolicyVersion: row.PolicyVersion, QueriedAt: row.QueriedAt, Observations: observations})
	}
	return response, nil
}
