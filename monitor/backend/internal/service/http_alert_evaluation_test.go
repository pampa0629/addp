package service

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
)

func TestHTTPStoppedTrafficCannotResolveActiveIncident(t *testing.T) {
	at := time.Unix(1800000000, 0).UTC()
	policy := models.HTTPThresholdPolicy{TriggerValue: 50, RecoveryValue: 10, TriggerSeconds: 15, RecoverySeconds: 15}
	for _, stopped := range []bool{false, true} {
		ratio, qps := httpThresholdSeries(at, 0), httpThresholdSeries(at, 1)
		qps.MetricKey, qps.Unit = "http.requests.per_second", "requests_per_second"
		if stopped {
			for i := len(qps.Points) - 2; i < len(qps.Points); i++ {
				zero, undefined := 0.0, math.NaN() // Completed counters no longer advance: 0/0.
				qps.Points[i].Value, ratio.Points[i].Value = &zero, &undefined
			}
		}
		lines, err := aggregateHTTPRuleSeries([][]resourcequery.HTTPObservation{{{Series: []resourcequery.Series{qps, ratio}}}}, ratio.MetricKey)
		if err != nil || len(lines) != 1 {
			t.Fatal(err)
		}
		result := evaluateHTTPThreshold(lines[0], policy, at.Add(-time.Hour), at, true)
		want := PlatformAlertNormal
		if stopped {
			want = PlatformAlertUnknown
		}
		if result.Verdict != want {
			t.Fatal("stopped traffic must not manufacture recovery", stopped, result)
		}
		if stopped && (lines[0].Points[21].Value != nil || lines[0].Points[21].DataState != "no_data") {
			t.Fatal("idle ratio must stay unknown")
		}
	}
}

func TestHTTPNotificationUsesItsOwnWeComCategoryAndRejectsUnknownEvents(t *testing.T) {
	for _, kind := range []string{"opened", "resolved"} {
		raw := `{"schema":"addp.platform-http-alert/v1","event_id":"http-event","event_type":"` + kind + `","incident_id":1,"subject_kind":"module","subject_id":"monitor","severity":"warning","occurred_at":"2026-10-20T00:00:00Z"}`
		payload, err := wecomPlatformAlertPayload(raw, "http-delivery")
		body, _ := json.Marshal(payload)
		if err != nil || !strings.Contains(string(body), "平台接口") || !strings.Contains(string(body), "monitor") {
			t.Fatal(payload, err)
		}
		if _, err := wecomPlatformAlertPayload(strings.Replace(raw, kind, "invented", 1), "http-delivery"); err == nil {
			t.Fatal("unknown HTTP notification event accepted")
		}
	}
}

func httpThresholdSeries(at time.Time, value float64) resourcequery.Series {
	line := resourcequery.Series{MetricKey: "http.failures.percent", Unit: "percent", WindowSeconds: 60, Dimensions: resourcequery.Dimensions{}, Points: []resourcequery.Point{}}
	for i := 21; i >= 0; i-- {
		sample := at.Add(-time.Duration(i) * 15 * time.Second)
		v := value
		line.Points = append(line.Points, resourcequery.Point{EvaluatedAt: sample, SampledAt: &sample, Value: &v, DataState: "valid"})
	}
	return line
}
func TestHTTPThresholdRequiresRealAdvancingWindowAndFrozenRecovery(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	since := at.Add(-time.Hour)
	policy := models.HTTPThresholdPolicy{TriggerValue: 10, RecoveryValue: 2, TriggerSeconds: 30, RecoverySeconds: 30}
	line := httpThresholdSeries(at, 20)
	if result := evaluateHTTPThreshold(line, policy, since, at, false); result.Verdict != PlatformAlertRisk || result.Samples != 3 {
		t.Fatal(result)
	}
	line = httpThresholdSeries(at, 1)
	if result := evaluateHTTPThreshold(line, policy, since, at, true); result.Verdict != PlatformAlertNormal {
		t.Fatal(result)
	}
	if result := evaluateHTTPThreshold(line, policy, since, at, false); result.Verdict != PlatformAlertNormal {
		t.Fatal(result)
	}
	for _, mutate := range []func(*resourcequery.Series){func(s *resourcequery.Series) { s.Points[20].DataState = "no_data" }, func(s *resourcequery.Series) { s.Points[20].SampledAt = s.Points[21].SampledAt }, func(s *resourcequery.Series) { s.Points[20].EvaluatedAt = s.Points[21].EvaluatedAt }, func(s *resourcequery.Series) { s.Points[21].Value = nil }} {
		bad := httpThresholdSeries(at, 1)
		mutate(&bad)
		if result := evaluateHTTPThreshold(bad, policy, since, at, true); result.Verdict != PlatformAlertUnknown {
			t.Fatal("invalid evidence recovered", result)
		}
	}
	if result := evaluateHTTPThreshold(httpThresholdSeries(at, 20), policy, at.Add(-60*time.Second), at, false); result.Verdict != PlatformAlertUnknown {
		t.Fatal("HTTP window crossed rule activation", result)
	}
	// Editing the rule's new recovery value must not replace the opened event's
	// frozen policy. The old recovery threshold still keeps this incident open.
	if result := evaluateHTTPThreshold(httpThresholdSeries(at, 5), policy, since, at, true); result.Verdict != PlatformAlertRisk {
		t.Fatal(result)
	}
}

func TestHTTPAggregationUsesRequestsAsWeightsAndRejectsOneMissingSource(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Second)
	rows := [][]resourcequery.HTTPObservation{}
	for i, weight := range []float64{1, 9} {
		line := httpThresholdSeries(at, []float64{100, 0}[i])
		qps := httpThresholdSeries(at, weight)
		qps.MetricKey = "http.requests.per_second"
		qps.Unit = "requests_per_second"
		rows = append(rows, []resourcequery.HTTPObservation{{Series: []resourcequery.Series{line, qps}}})
	}
	result, err := aggregateHTTPRuleSeries(rows, "http.failures.percent")
	if err != nil || *result[0].Points[21].Value != 10 {
		t.Fatal(result, err)
	}
	rows[1][0].Series[1].Points[20].DataState = "no_data"
	result, err = aggregateHTTPRuleSeries(rows, "http.failures.percent")
	if err != nil || result[0].Points[20].Value != nil {
		t.Fatal("missing source was zero-filled", result, err)
	}
	rows[1][0].Series[1].Points[20].DataState = "valid"
	rows[1][0].Series[1].Points[21].SampledAt = rows[1][0].Series[1].Points[20].SampledAt
	result, err = aggregateHTTPRuleSeries(rows, "http.failures.percent")
	if err != nil || result[0].Points[21].Value != nil {
		t.Fatal("repeated source sample proved continuity", result, err)
	}
}
