package service

import (
	"context"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"
)

type HTTPResourceBackend interface {
	HTTP(context.Context, resourcequery.HTTPPlan, resourcequery.ProcessScope, resourcequery.Budget) ([]resourcequery.HTTPObservation, error)
	HTTPCollection(context.Context, resourcequery.ProcessScope, time.Time, resourcequery.Budget) (resourcequery.Collection, error)
}

type httpHistoryBackend interface {
	HTTPHistory(context.Context, resourcequery.HTTPPlan, resourcequery.ProcessScope, time.Time, resourcequery.Budget) ([]resourcequery.HTTPObservation, error)
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
	HistoryState  string                          `json:"history_state,omitempty" enums:"available,no_data,outside_instance,outside_retention"`
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
	var historyState string
	var readStopped func(context.Context, client.RuntimeInstanceReference, time.Time, resourcequery.Budget) error
	if request.Trend {
		readStopped = func(ctx context.Context, owner client.RuntimeInstanceReference, now time.Time, b resourcequery.Budget) error {
			state, rows, err := readStoppedHTTP(ctx, s.process.Backend, httpPlan, owner, now, b)
			historyState, data[owner.ID] = state, rows
			return err
		}
	}
	current, plan, err := s.observeProcessesUsing(ctx, principal, token, request.IDs, request.Start, request.End, request.Trend, planFor, read, readStopped)
	if err != nil {
		return response, err
	}
	response.Start, response.End, response.StepSeconds = plan.Start, plan.End, plan.StepSeconds
	for _, row := range current.Data {
		observations := data[row.Subject.ID]
		if observations == nil || (row.Collection.State != "collecting" && historyState != "available") {
			observations = []resourcequery.HTTPObservation{}
		}
		response.Data = append(response.Data, HTTPProcessObservation{Subject: row.Subject, Collection: row.Collection, PolicyVersion: row.PolicyVersion, QueriedAt: row.QueriedAt, Observations: observations, HistoryState: historyState})
	}
	return response, nil
}

func readStoppedHTTP(ctx context.Context, backend ProcessResourceBackend, plan resourcequery.HTTPPlan, owner client.RuntimeInstanceReference, now time.Time, b resourcequery.Budget) (string, []resourcequery.HTTPObservation, error) {
	if owner.Role != "backend" && owner.Role != "ingress" {
		return "", nil, resourcequery.ErrInvalid
	}
	if owner.StoppedAt == nil || owner.StoppedAt.IsZero() || owner.StoppedAt.After(now) {
		return "", nil, metricsdiscovery.ErrIdentityUnavailable
	}
	if !plan.End.Before(*owner.StoppedAt) {
		return "outside_instance", nil, nil
	}
	oldest := now.UTC().Truncate(time.Second).Add(-resourcequery.HTTPHistoryAge)
	if plan.End.Before(oldest) {
		return "outside_retention", nil, nil
	}
	if !*owner.ProcessMetricsDeclared {
		return "no_data", nil, nil
	}
	if owner.RegisteredAt.IsZero() || owner.ProcessStartedAt == nil || owner.ProcessStartedAt.IsZero() ||
		owner.RegisteredAt.After(*owner.StoppedAt) || owner.ProcessStartedAt.After(*owner.StoppedAt) {
		return "", nil, metricsdiscovery.ErrIdentityUnavailable
	}
	for _, candidate := range []time.Time{owner.RegisteredAt, *owner.ProcessStartedAt} {
		if candidate.After(oldest) {
			oldest = candidate
		}
	}
	if plan.End.Before(oldest) {
		return "outside_instance", nil, nil
	}
	history, ok := backend.(httpHistoryBackend)
	if !ok {
		return "", nil, metricsdiscovery.ErrUnconfigured
	}
	scope := resourcequery.ProcessScope{ID: owner.ID, ModuleName: owner.ModuleName, InstanceID: owner.InstanceID, Role: owner.Role, StartedAt: *owner.ProcessStartedAt}
	rows, err := history.HTTPHistory(ctx, plan, scope, oldest, b)
	if err != nil {
		return "", nil, err
	}
	for _, row := range rows {
		for _, series := range row.Series {
			for _, point := range series.Points {
				if point.DataState == "valid" && point.Value != nil {
					return "available", rows, nil
				}
			}
		}
	}
	return "no_data", nil, nil
}
