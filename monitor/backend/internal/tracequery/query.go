// Package tracequery is the sole bounded Tempo adapter. Raw Tempo/OTLP fields
// never become browser DTOs and callers cannot supply TraceQL or endpoints.
package tracequery

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/common/execution/observability"
	"github.com/addp/common/httptrace"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

var ErrInvalid = errors.New("invalid_trace_query")
var ErrBudget = errors.New("trace_query_budget_exceeded")
var ErrUpstream = errors.New("trace_backend_unavailable")
var ErrNotFound = errors.New("trace_not_found")
var ErrBusy = errors.New("trace_query_busy")
var ErrDenied = errors.New("trace_participant_not_readable")

type Span struct {
	ID         string    `json:"span_id"`
	ParentID   string    `json:"parent_span_id"`
	Module     string    `json:"module_name"`
	Instance   string    `json:"instance_id"`
	Role       string    `json:"role"`
	Kind       string    `json:"kind"`
	Scope      string    `json:"scope"`
	Phase      string    `json:"phase,omitempty"`
	Attempt    int       `json:"attempt"`
	Outcome    string    `json:"outcome,omitempty"`
	Links      []Link    `json:"links"`
	Method     string    `json:"method"`
	Route      string    `json:"route"`
	Status     int       `json:"status"`
	Failed     bool      `json:"failed"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	DurationMS float64   `json:"duration_ms"`
}

// Link is a causal reference, never an inferred HTTP dependency. The numeric
// runtime ID is attached only after the current User's System authorization.
type Link struct {
	TraceID           string    `json:"trace_id"`
	SpanID            string    `json:"span_id"`
	ModuleName        string    `json:"module_name"`
	Role              string    `json:"role"`
	InstanceID        string    `json:"instance_id"`
	StartedAt         time.Time `json:"started_at"`
	RuntimeInstanceID uint      `json:"runtime_instance_id"`
}
type Edge struct {
	FromModule   string `json:"from_module"`
	FromInstance string `json:"from_instance"`
	ToModule     string `json:"to_module"`
	ToInstance   string `json:"to_instance"`
	ClientSpan   string `json:"client_span_id"`
	ServerSpan   string `json:"server_span_id"`
}
type Detail struct {
	ID            string    `json:"trace_id"`
	Start         time.Time `json:"start"`
	End           time.Time `json:"end"`
	DurationMS    float64   `json:"duration_ms"`
	Spans         []Span    `json:"spans"`
	Edges         []Edge    `json:"edges"`
	Partial       bool      `json:"partial"`
	PolicyVersion int       `json:"policy_version"`
	Coverage      string    `json:"coverage"`
}
type Backend interface {
	Search(context.Context, commonclient.RuntimeInstanceReference, time.Time, time.Time, int) ([]string, error)
	Trace(context.Context, string, time.Time, time.Time) (Detail, error)
}
type Client struct {
	origin *url.URL
	http   *http.Client
}

func NewClient(origin string, client *http.Client) (*Client, error) {
	u, e := url.Parse(origin)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || client == nil {
		return nil, ErrInvalid
	}
	copy := *client
	copy.Timeout = 5 * time.Second
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{u, &copy}, nil
}
func FromEnvironment() Backend {
	if os.Getenv("ADDP_OBSERVABILITY_TRACES_ENABLED") != "true" {
		return nil
	}
	client, e := httptrace.TLSHTTPClient(os.Getenv("MONITOR_TRACE_TLS_DIR"), 5*time.Second)
	if e != nil {
		return nil
	}
	v, e := NewClient(os.Getenv("MONITOR_TEMPO_URL"), client)
	if e != nil {
		return nil
	}
	return v
}
func (c *Client) read(ctx context.Context, path string, q url.Values, accept string) ([]byte, error) {
	u := *c.origin
	u.Path = path
	u.RawQuery = q.Encode()
	req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	req.Header.Set("Accept", accept)
	res, e := c.http.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUpstream
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return nil, ErrNotFound
	}
	if res.StatusCode != 200 {
		return nil, ErrUpstream
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if len(b) > 2<<20 {
		return nil, ErrBudget
	}
	if e != nil {
		return nil, ErrUpstream
	}
	return b, nil
}
func (c *Client) Search(ctx context.Context, subject commonclient.RuntimeInstanceReference, start, end time.Time, limit int) ([]string, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	expression := "{ resource.service.name = " + strconv.Quote(subject.ModuleName) + " && resource.service.instance.id = " + strconv.Quote(subject.InstanceID) + " }"
	b, e := c.read(ctx, "/api/search", url.Values{"q": {expression}, "start": {strconv.FormatInt(start.Unix(), 10)}, "end": {strconv.FormatInt(end.Unix(), 10)}, "limit": {strconv.Itoa(limit)}}, "application/json")
	if e != nil {
		return nil, e
	}
	var result struct {
		Traces []struct {
			ID string `json:"traceID"`
		} `json:"traces"`
	}
	if json.Unmarshal(b, &result) != nil || result.Traces == nil || len(result.Traces) > limit {
		return nil, ErrUpstream
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, v := range result.Traces {
		id, err := tempoTraceID(v.ID)
		if err != nil || seen[id] {
			return nil, ErrUpstream
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

// Tempo's JSON search API serializes trace IDs as unpadded lowercase hex.
// Normalize once at this protocol boundary; the ADDP API remains fixed-width.
func tempoTraceID(value string) (string, error) {
	if len(value) < 1 || len(value) > 32 {
		return "", ErrUpstream
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return "", ErrUpstream
		}
	}
	id := strings.Repeat("0", 32-len(value)) + value
	if !httptrace.ParseTraceID(id) {
		return "", ErrUpstream
	}
	return id, nil
}

func (c *Client) Trace(ctx context.Context, id string, start, end time.Time) (Detail, error) {
	if !httptrace.ParseTraceID(id) {
		return Detail{}, ErrInvalid
	}
	b, e := c.read(ctx, "/api/traces/"+id, url.Values{"start": {strconv.FormatInt(start.Unix(), 10)}, "end": {strconv.FormatInt(end.Unix(), 10)}}, "application/protobuf")
	if e != nil {
		return Detail{}, e
	}
	var raw collector.ExportTraceServiceRequest
	if proto.Unmarshal(b, &raw) != nil {
		return Detail{}, ErrUpstream
	}
	return Decode(id, &raw)
}

func attrs(v []*commonpb.KeyValue) map[string]*commonpb.AnyValue {
	out := map[string]*commonpb.AnyValue{}
	for _, a := range v {
		if a != nil {
			out[a.Key] = a.Value
		}
	}
	return out
}

var moduleName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,49}$`)
var instanceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$`)

func bounded(v string, max int) bool { return len(v) <= max && !strings.ContainsAny(v, "\x00\r\n?#") }
func Decode(id string, raw *collector.ExportTraceServiceRequest) (Detail, error) {
	result := Detail{ID: id, Spans: []Span{}, Edges: []Edge{}, PolicyVersion: Definition().Version, Coverage: "sampled"}
	seen := map[string]bool{}
	for _, batch := range raw.GetResourceSpans() {
		a := attrs(batch.GetResource().GetAttributes())
		module, instance, role := a["service.name"].GetStringValue(), a["service.instance.id"].GetStringValue(), a["addp.role"].GetStringValue()
		if !moduleName.MatchString(module) || !instanceName.MatchString(instance) || (role != "backend" && role != "ingress") {
			return result, ErrUpstream
		}
		for _, scope := range batch.GetScopeSpans() {
			scopeName := scope.GetScope().GetName()
			if scopeName != "addp.http" && scopeName != "addp.execution" {
				return result, ErrUpstream
			}
			for _, span := range scope.GetSpans() {
				sid := hex.EncodeToString(span.SpanId)
				parent := hex.EncodeToString(span.ParentSpanId)
				if !httptrace.ParseSpanID(sid) || seen[sid] || hex.EncodeToString(span.TraceId) != id || (parent != "" && !httptrace.ParseSpanID(parent)) || span.StartTimeUnixNano == 0 || span.EndTimeUnixNano < span.StartTimeUnixNano || span.EndTimeUnixNano > uint64(1<<63-1) {
					return result, ErrUpstream
				}
				seen[sid] = true
				if len(result.Spans) >= 1000 {
					return result, ErrBudget
				}
				s := Span{ID: sid, ParentID: parent, Module: module, Instance: instance, Role: role, Scope: scopeName, Links: []Link{}, Start: time.Unix(0, int64(span.StartTimeUnixNano)).UTC(), End: time.Unix(0, int64(span.EndTimeUnixNano)).UTC(), DurationMS: float64(span.EndTimeUnixNano-span.StartTimeUnixNano) / 1e6}
				var err error
				if scopeName == "addp.http" {
					err = decodeHTTP(span, &s)
				} else {
					err = decodeExecution(id, span, &s)
				}
				if err != nil {
					return result, err
				}
				if result.Start.IsZero() || s.Start.Before(result.Start) {
					result.Start = s.Start
				}
				if s.End.After(result.End) {
					result.End = s.End
				}
				result.Spans = append(result.Spans, s)
			}
		}
	}
	if len(result.Spans) == 0 {
		return result, ErrNotFound
	}
	result.DurationMS = float64(result.End.Sub(result.Start)) / float64(time.Millisecond)
	byID := map[string]Span{}
	for _, s := range result.Spans {
		byID[s.ID] = s
	}
	matchedClients := map[string]bool{}
	roots := 0
	for _, s := range result.Spans {
		if s.ParentID == "" {
			if s.Kind != "server" && s.Scope != "addp.execution" {
				result.Partial = true
			}
			roots++
			continue
		}
		parent, ok := byID[s.ParentID]
		if !ok {
			result.Partial = true
			continue
		}
		visited := map[string]bool{s.ID: true}
		p := parent
		for {
			if visited[p.ID] {
				return result, ErrUpstream
			}
			visited[p.ID] = true
			next, ok := byID[p.ParentID]
			if !ok {
				break
			}
			p = next
		}
		if s.Kind == "server" && parent.Kind == "client" {
			matchedClients[parent.ID] = true
			result.Edges = append(result.Edges, Edge{parent.Module, parent.Instance, s.Module, s.Instance, parent.ID, s.ID})
		}
	}
	for _, s := range result.Spans {
		if s.Kind == "client" && !matchedClients[s.ID] {
			result.Partial = true
		}
	}
	if roots != 1 {
		result.Partial = true
	}
	sort.Slice(result.Spans, func(i, j int) bool {
		if result.Spans[i].Start.Equal(result.Spans[j].Start) {
			return result.Spans[i].ID < result.Spans[j].ID
		}
		return result.Spans[i].Start.Before(result.Spans[j].Start)
	})
	return result, nil
}

// Existing HTTP attributes are projected through their established whitelist.
// Execution instrumentation has a smaller exact contract and accepts no opaque
// fields or events, so execution identifiers and SQL cannot reach its DTO.
func decodeHTTP(raw *tracepb.Span, span *Span) error {
	if len(raw.Links) != 0 || raw.DroppedLinksCount != 0 {
		return ErrUpstream
	}
	fields := attrs(raw.Attributes)
	span.Method, span.Route = fields["http.request.method"].GetStringValue(), fields["http.route"].GetStringValue()
	status := fields["http.response.status_code"].GetIntValue()
	switch span.Method {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "CONNECT", "TRACE", "QUERY", "_OTHER":
	default:
		return ErrUpstream
	}
	if !bounded(span.Route, 1024) || status < 0 || status > 599 || (status > 0 && status < 100) {
		return ErrUpstream
	}
	switch raw.Kind {
	case tracepb.Span_SPAN_KIND_SERVER:
		span.Kind = "server"
	case tracepb.Span_SPAN_KIND_CLIENT:
		span.Kind = "client"
	default:
		return ErrUpstream
	}
	span.Status, span.Failed = int(status), fields["addp.http.server_failure"].GetBoolValue()
	return nil
}

func decodeExecution(id string, raw *tracepb.Span, span *Span) error {
	if span.Role != "backend" || span.ParentID != "" || len(raw.Attributes) != 3 || raw.DroppedAttributesCount != 0 || len(raw.Events) != 0 || raw.DroppedEventsCount != 0 || len(raw.Links) > 2 || raw.DroppedLinksCount != 0 {
		return ErrUpstream
	}
	fields := map[string]*commonpb.AnyValue{}
	for _, item := range raw.Attributes {
		if item == nil || item.Value == nil || fields[item.Key] != nil {
			return ErrUpstream
		}
		switch item.Key {
		case "addp.execution.phase", "addp.execution.outcome":
			if _, ok := item.Value.Value.(*commonpb.AnyValue_StringValue); !ok {
				return ErrUpstream
			}
		case "addp.execution.attempt":
			if _, ok := item.Value.Value.(*commonpb.AnyValue_IntValue); !ok {
				return ErrUpstream
			}
		default:
			return ErrUpstream
		}
		fields[item.Key] = item.Value
	}
	span.Phase, span.Outcome = fields["addp.execution.phase"].GetStringValue(), fields["addp.execution.outcome"].GetStringValue()
	attempt := fields["addp.execution.attempt"].GetIntValue()
	if attempt < 0 || attempt > int64(^uint(0)>>1) {
		return ErrUpstream
	}
	span.Attempt = int(attempt)
	var kind tracepb.Span_SpanKind
	switch span.Phase {
	case "enqueue":
		span.Kind, kind = "producer", tracepb.Span_SPAN_KIND_PRODUCER
		if attempt != 0 {
			return ErrUpstream
		}
	case "claim":
		span.Kind, kind = "consumer", tracepb.Span_SPAN_KIND_CONSUMER
	case "dispatch":
		span.Kind, kind = "producer", tracepb.Span_SPAN_KIND_PRODUCER
	case "terminal":
		span.Kind, kind = "internal", tracepb.Span_SPAN_KIND_INTERNAL
	default:
		return ErrUpstream
	}
	if raw.Kind != kind || (span.Phase != "enqueue" && attempt < 1) {
		return ErrUpstream
	}
	switch span.Outcome {
	case "success", "failed", "timeout", "cancelled", "rejected":
	default:
		return ErrUpstream
	}
	span.Failed = span.Outcome == "failed" || span.Outcome == "timeout"
	if span.Failed != (raw.GetStatus().GetCode() == tracepb.Status_STATUS_CODE_ERROR) {
		return ErrUpstream
	}
	seen := map[string]bool{}
	for _, item := range raw.Links {
		if item == nil || len(item.Attributes) != 4 || item.DroppedAttributesCount != 0 || item.TraceState != "" {
			return ErrUpstream
		}
		values := map[string]string{}
		for _, attr := range item.Attributes {
			if attr == nil || attr.Value == nil {
				return ErrUpstream
			}
			if _, duplicate := values[attr.Key]; duplicate {
				return ErrUpstream
			}
			value, ok := attr.Value.Value.(*commonpb.AnyValue_StringValue)
			if !ok {
				return ErrUpstream
			}
			switch attr.Key {
			case "addp.link.module_name", "addp.link.role", "addp.link.instance_id", "addp.link.started_at":
			default:
				return ErrUpstream
			}
			values[attr.Key] = value.StringValue
		}
		started, err := time.Parse(time.RFC3339Nano, values["addp.link.started_at"])
		if err != nil || started.UTC().Format(time.RFC3339Nano) != values["addp.link.started_at"] {
			return ErrUpstream
		}
		ref := observability.TraceReference{TraceID: hex.EncodeToString(item.TraceId), SpanID: hex.EncodeToString(item.SpanId), ModuleName: values["addp.link.module_name"], Role: values["addp.link.role"], InstanceID: values["addp.link.instance_id"], StartedAt: started}
		key := ref.TraceID + "/" + ref.SpanID
		if ref.Validate() != nil || ref.TraceID == id || seen[key] {
			return ErrUpstream
		}
		seen[key] = true
		span.Links = append(span.Links, Link{TraceID: ref.TraceID, SpanID: ref.SpanID, ModuleName: ref.ModuleName, Role: ref.Role, InstanceID: ref.InstanceID, StartedAt: started})
	}
	return nil
}
