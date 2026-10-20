package resourcequery

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var httpDefinitions = []Definition{
	{"http.requests.per_second", "requests_per_second", 60},
	{"http.failures.percent", "percent", 60},
	{"http.client_errors.percent", "percent", 60},
	{"http.duration.mean_seconds", "seconds", 60},
	{"http.duration.p95_seconds", "seconds", 60},
	{"http.duration.p99_seconds", "seconds", 60},
	{"http.active_requests", "requests", 0},
}

// HTTPPlan can aggregate one current process, list its observed templates, or
// read one exact template/method. It cannot select arbitrary labels or PromQL.
type HTTPPlan struct {
	Plan
	WindowSeconds int
	Grouped       bool
	Route         *string
	Method        string
	Rank          string
}

func HTTPCatalog() []Definition { return append([]Definition(nil), httpDefinitions...) }

func NewHTTPPlan(start, end, now time.Time, trend, grouped bool, route *string, method string, window int, b Budget) (HTTPPlan, error) {
	p := HTTPPlan{WindowSeconds: window, Grouped: grouped, Route: route, Method: method}
	if (window != 60 && window != 300 && window != 900) || (grouped && (trend || route != nil || method != "")) || (route == nil && method != "") || (route != nil && (!httpRoute(*route) || !httpMethod(method))) {
		return p, ErrInvalid
	}
	catalog := HTTPCatalog()
	if grouped || route != nil {
		catalog = catalog[:len(catalog)-1] // OTel active requests are process/method, not route-specific.
	}
	keys := make([]string, len(catalog))
	for i := range catalog {
		keys[i] = catalog[i].Key
		if catalog[i].WindowSeconds != 0 {
			catalog[i].WindowSeconds = window
		}
	}
	var err error
	p.Plan, err = newPlan(keys, start, end, now, trend, nil, b, catalog)
	if err == nil && grouped {
		p.SeriesUpperBound = b.MaxSeries / len(keys) * len(keys)
		if p.SeriesUpperBound < len(keys) || p.SeriesUpperBound > b.MaxTotalPoints {
			err = ErrBudget
		}
	}
	return p, err
}

func httpRoute(route string) bool {
	return len(route) <= 1024 && utf8.ValidString(route) && (route == "" || strings.HasPrefix(route, "/")) && strings.IndexFunc(route, unicode.IsControl) < 0
}

func httpMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "CONNECT", "TRACE", "QUERY", "_OTHER":
		return true
	}
	return false
}

func (s ProcessScope) httpExpression(p HTTPPlan) (string, error) {
	if s.Validate() != nil || (s.Role != "backend" && s.Role != "ingress") {
		return "", ErrInvalid
	}
	checked, err := NewHTTPPlan(p.Start, p.End, p.End, p.Trend, p.Grouped, p.Route, p.Method, p.WindowSeconds, DefaultBudget())
	if err != nil || len(checked.Metrics) != len(p.Metrics) {
		return "", ErrInvalid
	}
	if p.Rank != "" && (!p.Grouped || (p.Rank != "mean" && p.Rank != "qps")) {
		return "", ErrInvalid
	}
	w := strconv.Itoa(p.WindowSeconds) + "s"
	offset := " offset " + w
	info, up, total := s.identitySelector(), s.selector("up"), s.selector("addp_http_server_duration_seconds_count")
	started := s.selector("addp_http_server_instrumentation_start_time_seconds")
	currentGuard := s.identityGuard("") +
		` and (count(` + up + `)==1) and (max(` + up + `)==1) and (max(timestamp(` + up + `))==max(timestamp(` + info + `)))` +
		` and (count(` + total + `)==1) and (max(timestamp(` + total + `))==max(timestamp(` + info + `)))` +
		` and (count(` + started + `)==1) and (max(` + started + `)>0)` +
		` and (max(timestamp(` + total + `))>=time()-16)`
	guard := currentGuard + s.identityGuard(offset) +
		` and (count(` + up + offset + `)==1) and (max(` + up + offset + `)==1)` +
		` and (max(timestamp(` + up + offset + `))==max(timestamp(` + total + offset + `)))` +
		` and (max(` + started + `)<=time()-` + strconv.Itoa(p.WindowSeconds) + `)` +
		` and (max(` + started + `)==max(` + started + offset + `))` +
		` and (min(min_over_time(` + up + `[` + w + `]))==1) and (min(count_over_time(` + up + `[` + w + `]))>=` + strconv.Itoa(p.WindowSeconds/15) + `)` +
		` and (max(timestamp(` + total + `))>=time()-16) and (max(timestamp(` + total + offset + `))>=time()-` + strconv.Itoa(p.WindowSeconds+16) + `)` +
		` and (abs(max(timestamp(` + total + `))-max(timestamp(` + total + offset + `))-` + strconv.Itoa(p.WindowSeconds) + `)<=1)` +
		` and (max(resets(` + total + `[` + w + `]))==0)`
	group := "()"
	if p.Grouped {
		group = "(http_route,http_request_method)"
	}
	raw := func(metric, filter string) string {
		selector := s.selector(metric)
		if p.Route != nil {
			filter += `,http_route=` + strconv.Quote(*p.Route) + `,http_request_method=` + strconv.Quote(p.Method)
		}
		return strings.TrimSuffix(selector, "}") + filter + "}"
	}
	sum := func(selector string) string { return `(sum by` + group + `(` + selector + `))` }
	// Compare sums at the two fixed window boundaries. Identity/start/reset
	// guards make these immutable-process counter deltas. This includes a new
	// status/error series once without rate extrapolation biasing rare errors.
	count := sum(raw("http_server_request_duration_seconds_count", ""))
	baselineCount := sum(raw("http_server_request_duration_seconds_count", "") + offset)
	delta := func(metric, filter string, zeroFallback bool) string {
		current := sum(raw(metric, filter))
		baseline := sum(raw(metric, filter) + offset)
		if zeroFallback {
			current = `(` + current + ` or on` + group + ` (0*` + count + `))`
			baseline = `(` + baseline + ` or on` + group + ` (0*` + baselineCount + `))`
		}
		return `(` + current + `-on` + group + ` ` + baseline + `)`
	}
	countDelta := delta("http_server_request_duration_seconds_count", "", false)
	if !p.Grouped && p.Route == nil {
		countDelta = `(max(` + total + `)-max(` + total + offset + `))`
	}
	countRate := `(` + countDelta + `/` + strconv.Itoa(p.WindowSeconds) + `)`
	groupGuard := ""
	if p.Grouped || p.Route != nil {
		// offset applies to selectors, not aggregate expressions.
		groupGuard = ` and on` + group + ` (` + sum(raw("http_server_request_duration_seconds_count", "")+offset) + `>=0)`
	} else {
		groupGuard = ` and (max(` + total + offset + `)>=0)`
	}
	parts := []string{}
	rankGuard := ""
	if p.Rank != "" {
		rankValue := countRate
		if p.Rank == "mean" {
			rankValue = `(` + delta("http_server_request_duration_seconds_sum", "", false) + `/` + countDelta + `)`
		}
		limit := p.SeriesUpperBound / len(p.Metrics)
		if limit > 10 {
			limit = 10
		}
		if limit < 1 {
			return "", ErrBudget
		}
		rankGuard = ` and on` + group + ` topk(` + strconv.Itoa(limit) + `,(` + rankValue + groupGuard + `)>=0)`
	}
	for _, d := range p.Metrics {
		var value string
		metricGuard, metricGroupGuard := guard, groupGuard
		switch d.Key {
		case "http.requests.per_second":
			value = countRate
		case "http.failures.percent":
			value = `(100*` + delta("http_server_request_duration_seconds_count", `,error_type!=""`, true) + `/` + countDelta + `)`
			if !p.Grouped && p.Route == nil {
				value = `(100*(max(` + s.selector("addp_http_server_failures_total") + `)-max(` + s.selector("addp_http_server_failures_total") + offset + `))/` + countDelta + `)`
			}
		case "http.client_errors.percent":
			value = `(100*` + delta("http_server_request_duration_seconds_count", `,http_response_status_code=~"4.."`, true) + `/` + countDelta + `)`
			if !p.Grouped && p.Route == nil {
				value = `(100*(max(` + s.selector("addp_http_server_client_errors_total") + `)-max(` + s.selector("addp_http_server_client_errors_total") + offset + `))/` + countDelta + `)`
			}
		case "http.duration.mean_seconds":
			value = `(` + delta("http_server_request_duration_seconds_sum", "", false) + `/` + countDelta + `)`
			if !p.Grouped && p.Route == nil {
				value = `((max(` + s.selector("addp_http_server_duration_seconds_sum") + `)-max(` + s.selector("addp_http_server_duration_seconds_sum") + offset + `))/` + countDelta + `)`
			}
		case "http.duration.p95_seconds", "http.duration.p99_seconds":
			q := "0.95"
			if d.Key == "http.duration.p99_seconds" {
				q = "0.99"
			}
			bucketGroup := "(le)"
			if p.Grouped {
				bucketGroup = "(le,http_route,http_request_method)"
			}
			value = `histogram_quantile(` + q + `,` + `((sum by` + bucketGroup + `(` + raw("http_server_request_duration_seconds_bucket", "") + `))-on` + bucketGroup + ` (sum by` + bucketGroup + `(` + raw("http_server_request_duration_seconds_bucket", "") + offset + `)))` + `)`
			if !p.Grouped && p.Route == nil {
				value = `histogram_quantile(` + q + `,sum by(le)(` + s.selector("addp_http_server_duration_seconds_bucket") + `-` + s.selector("addp_http_server_duration_seconds_bucket") + offset + `))`
			}
		case "http.active_requests":
			value = `sum(` + s.selector("http_server_active_requests") + `)`
			metricGuard, metricGroupGuard = currentGuard, ""
		default:
			return "", ErrInvalid
		}
		// Scalar identity/collection evidence filters every route without joining
		// private identity labels into the public grouped response.
		value = `(` + value + metricGroupGuard + rankGuard + `) and on() (vector(1)` + metricGuard + `)`
		stamp := `(max(timestamp(` + total + `)) and on() (vector(1)` + metricGuard + `))`
		if p.Grouped {
			stamp = `((` + count + `*0 + on() group_left() ` + stamp + `)` + rankGuard + `)`
		}
		for _, component := range []struct{ key, expression string }{{"value", value}, {"sampled_at", stamp}} {
			parts = append(parts, fmt.Sprintf(`label_replace(label_replace((%s),"addp_metric",%s,"",""),"addp_component",%s,"","")`, component.expression, strconv.Quote(d.Key), strconv.Quote(component.key)))
		}
	}
	return strings.Join(parts, " or "), nil
}

type HTTPObservation struct {
	Route  string   `json:"route"`
	Method string   `json:"method"`
	Series []Series `json:"series"`
}

// HTTPCollection reuses current process identity evidence. Instrumentation
// readiness is separate from process UP and from a complete request window.
func (c *Client) HTTPCollection(ctx context.Context, scope ProcessScope, at time.Time, b Budget) (Collection, error) {
	if c == nil {
		return Collection{}, ErrUnavailable
	}
	if scope.Validate() != nil || b.Validate() != nil {
		return Collection{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(b.TimeoutSeconds)*time.Second)
	defer cancel()
	started := scope.selector("addp_http_server_instrumentation_start_time_seconds")
	expression := scope.collectionExpression() + ` or label_replace(max(` + started + `),"signal","http_started","","") or label_replace(max(timestamp(` + started + `)),"signal","http_sampled_at","","")`
	data, err := c.read(ctx, "/api/v1/query", url.Values{"query": {expression}, "time": {at.Format(time.RFC3339)}, "timeout": {strconv.Itoa(b.TimeoutSeconds) + "s"}})
	if err != nil {
		return Collection{}, err
	}
	values, err := normalizeSignals(data, at, map[string]bool{"up_count": true, "up": true, "sampled_at": true, "identity_valid": true, "http_started": true, "http_sampled_at": true})
	if err != nil {
		return Collection{}, err
	}
	result, err := normalizeUpEvidence(values, at)
	if err != nil || result.State != "collecting" {
		return result, err
	}
	if values["identity_valid"] != 1 {
		result.State = "identity_mismatch"
	} else if values["http_started"] == 0 || values["http_sampled_at"] != values["sampled_at"] {
		result.State = "not_instrumented"
	}
	return result, nil
}

// HTTP shares the protected transport, point normalizer and query budgets. Route
// dimensions are removed only after their closed shape and limit are validated.
func (c *Client) HTTP(ctx context.Context, p HTTPPlan, scope ProcessScope, b Budget) ([]HTTPObservation, error) {
	if c == nil {
		return nil, ErrUnavailable
	}
	if b.Validate() != nil {
		return nil, ErrInvalid
	}
	expression, err := scope.httpExpression(p)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(b.TimeoutSeconds)*time.Second)
	defer cancel()
	// Use the same URL, TLS and deadline handling as existing fixed queries.
	endpoint, query := planExpressionQuery(p.Plan, expression, b)
	data, err := c.readForm(ctx, endpoint, query)
	if err != nil {
		return nil, err
	}
	return normalizeHTTP(data, p, b)
}

func normalizeHTTP(data envelope, p HTTPPlan, b Budget) ([]HTTPObservation, error) {
	if !p.Grouped {
		series, err := normalize(data, p.Plan, b)
		row := HTTPObservation{Series: series}
		if p.Route != nil {
			row.Route, row.Method = *p.Route, p.Method
		}
		return []HTTPObservation{row}, err
	}
	if data.Data.ResultType != "vector" || data.Data.Result == nil || len(data.Data.Result) > 2*b.MaxSeries {
		return nil, ErrBudget
	}
	groups := map[string]envelope{}
	identities := map[string]HTTPObservation{}
	for _, row := range data.Data.Result {
		route, method := row.Metric["http_route"], row.Metric["http_request_method"]
		// Prometheus omits an empty http_route label for unmatched requests.
		expectedLabels := 3
		if _, hasRoute := row.Metric["http_route"]; hasRoute {
			expectedLabels++
		}
		if len(row.Metric) != expectedLabels || !httpRoute(route) || !httpMethod(method) {
			return nil, ErrUnavailable
		}
		key := method + "\x00" + route
		group, exists := groups[key]
		if !exists {
			group.Data.ResultType, group.Data.Result = "vector", []wireSeries{}
		}
		copy := row
		copy.Metric = map[string]string{"addp_metric": row.Metric["addp_metric"], "addp_component": row.Metric["addp_component"]}
		group.Data.Result = append(group.Data.Result, copy)
		groups[key], identities[key] = group, HTTPObservation{Route: route, Method: method}
		if len(groups)*len(p.Metrics) > b.MaxSeries || len(groups)*len(p.Metrics) > b.MaxTotalPoints {
			return nil, ErrBudget
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]HTTPObservation, 0, len(keys))
	for _, key := range keys {
		row := identities[key]
		var err error
		row.Series, err = normalize(groups[key], p.Plan, b)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, nil
}
