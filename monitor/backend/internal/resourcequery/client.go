package resourcequery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const responseLimit = 8 << 20

type Client struct {
	origin string
	http   *http.Client
}

func NewClient(origin string, ca []byte, certificate tls.Certificate) (*Client, error) {
	u, err := url.Parse(origin)
	roots := x509.NewCertPool()
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.Port() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(origin, "%@?#\\\r\n") || !roots.AppendCertsFromPEM(ca) || len(certificate.Certificate) == 0 || certificate.PrivateKey == nil {
		return nil, ErrInvalid
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil || leaf.IsCA || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return nil, ErrInvalid
	}
	clientAuth := false
	for _, usage := range leaf.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			clientAuth = true
		}
	}
	if !clientAuth {
		return nil, ErrInvalid
	}
	certificate.Leaf = leaf
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil || port == 0 || strconv.FormatUint(port, 10) != u.Port() {
		return nil, ErrInvalid
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{certificate}}, MaxConnsPerHost: 8, MaxIdleConnsPerHost: 8, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 32 << 10, DisableCompression: true}
	return &Client{origin: origin, http: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Close() {
	if c != nil {
		c.http.CloseIdleConnections()
	}
}

type Point struct {
	EvaluatedAt time.Time  `json:"evaluated_at"`
	SampledAt   *time.Time `json:"sampled_at"`
	Value       *float64   `json:"value"`
	DataState   string     `json:"data_state"`
}
type Series struct {
	MetricKey     string  `json:"metric_key"`
	Unit          string  `json:"unit"`
	WindowSeconds int     `json:"window_seconds"`
	Points        []Point `json:"points"`
}

func Empty(p Plan, state string) []Series {
	out := make([]Series, 0, len(p.Metrics))
	for _, d := range p.Metrics {
		row := Series{MetricKey: d.Key, Unit: d.Unit, WindowSeconds: d.WindowSeconds, Points: make([]Point, p.Points)}
		for i := range row.Points {
			row.Points[i] = Point{EvaluatedAt: p.Start.Add(time.Duration(int64(i)*p.StepSeconds) * time.Second), DataState: state}
		}
		out = append(out, row)
	}
	return out
}

type sample [2]json.RawMessage

func (s *sample) UnmarshalJSON(data []byte) error {
	var pair []json.RawMessage
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	if len(pair) != 2 {
		return ErrUnavailable
	}
	s[0], s[1] = pair[0], pair[1]
	return nil
}

type wireSeries struct {
	Metric     map[string]string `json:"metric"`
	Value      *sample           `json:"value"`
	Values     []sample          `json:"values"`
	Histograms json.RawMessage   `json:"histograms"`
	Histogram  json.RawMessage   `json:"histogram"`
}
type envelope struct {
	Status    string   `json:"status"`
	ErrorType string   `json:"errorType"`
	Warnings  []string `json:"warnings"`
	Infos     []string `json:"infos"`
	Data      struct {
		ResultType string       `json:"resultType"`
		Result     []wireSeries `json:"result"`
	} `json:"data"`
}

func (c *Client) Query(ctx context.Context, p Plan, s Scope, b Budget) ([]Series, error) {
	if b.Validate() != nil {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(b.TimeoutSeconds)*time.Second)
	defer cancel()
	if c == nil {
		return nil, ErrUnavailable
	}
	expression, err := p.Expression(s)
	if err != nil {
		return nil, err
	}
	endpoint := "/api/v1/query"
	q := url.Values{"query": {expression}, "timeout": {strconv.Itoa(b.TimeoutSeconds) + "s"}, "lookback_delta": {strconv.FormatInt(LookbackSeconds, 10) + "s"}}
	if p.Trend {
		endpoint = "/api/v1/query_range"
		q.Set("start", p.Start.Format(time.RFC3339))
		q.Set("end", p.End.Format(time.RFC3339))
		q.Set("step", strconv.FormatInt(p.StepSeconds, 10)+"s")
	} else {
		q.Set("time", p.End.Format(time.RFC3339))
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.origin+endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	if len(body) > responseLimit {
		return nil, ErrBudget
	}
	var data envelope
	if json.Unmarshal(body, &data) != nil {
		return nil, ErrUnavailable
	}
	if data.Status == "error" && data.ErrorType == "timeout" {
		return nil, context.DeadlineExceeded
	}
	if response.StatusCode != 200 || data.Status != "success" || len(data.Warnings) > 0 || len(data.Infos) > 0 {
		return nil, ErrUnavailable
	}
	return normalize(data, p, b)
}
func normalize(data envelope, p Plan, b Budget) ([]Series, error) {
	expected := "vector"
	if p.Trend {
		expected = "matrix"
	}
	if data.Data.ResultType != expected || data.Data.Result == nil {
		return nil, ErrUnavailable
	}
	if len(data.Data.Result) > 2*b.MaxSeries || len(data.Data.Result) > 2*len(p.Metrics) {
		return nil, ErrBudget
	}
	out := Empty(p, "no_data")
	indexes := map[string]int{}
	for i, d := range p.Metrics {
		indexes[d.Key] = i
	}
	seen := map[string]bool{}
	values := map[string]map[int]float64{}
	stamps := map[string]map[int]float64{}
	count := 0
	for _, row := range data.Data.Result {
		key, component := row.Metric["addp_metric"], row.Metric["addp_component"]
		_, known := indexes[key]
		identity := key + ":" + component
		if !known || len(row.Metric) != 2 || (component != "value" && component != "sampled_at") || seen[identity] || len(row.Histograms) > 0 || len(row.Histogram) > 0 {
			return nil, ErrUnavailable
		}
		seen[identity] = true
		samples := row.Values
		if p.Trend {
			if row.Value != nil || samples == nil {
				return nil, ErrUnavailable
			}
		} else {
			if row.Value == nil || row.Values != nil {
				return nil, ErrUnavailable
			}
			samples = []sample{*row.Value}
		}
		count += len(samples)
		if len(samples) > b.MaxPointsPerSeries || count > 2*b.MaxTotalPoints {
			return nil, ErrBudget
		}
		target := values
		if component == "sampled_at" {
			target = stamps
		}
		target[key] = map[int]float64{}
		last := -1
		for _, pair := range samples {
			var at float64
			var raw string
			if json.Unmarshal(pair[0], &at) != nil || json.Unmarshal(pair[1], &raw) != nil || math.IsNaN(at) || math.IsInf(at, 0) {
				return nil, ErrUnavailable
			}
			offset := at - float64(p.Start.Unix())
			index := int(offset / float64(p.StepSeconds))
			if offset < 0 || math.Mod(offset, float64(p.StepSeconds)) != 0 || index < 0 || index >= p.Points || index <= last {
				return nil, ErrUnavailable
			}
			last = index
			value, e := strconv.ParseFloat(raw, 64)
			if e != nil {
				return nil, ErrUnavailable
			}
			target[key][index] = value
		}
	}
	for key, rowIndex := range indexes {
		for i := range out[rowIndex].Points {
			v, vok := values[key][i]
			ts, tok := stamps[key][i]
			point := &out[rowIndex].Points[i]
			if !vok || !tok || math.IsNaN(v) || math.IsInf(v, 0) || math.IsNaN(ts) || math.IsInf(ts, 0) || v < 0 || ts < 0 || ts > float64(point.EvaluatedAt.Unix()) || (out[rowIndex].Unit == "percent" && v > 100) {
				continue
			}
			sec, fraction := math.Modf(ts)
			stamp := time.Unix(int64(sec), int64(fraction*1e9)).UTC()
			point.SampledAt = &stamp
			point.Value = &v
			point.DataState = "valid"
			if point.EvaluatedAt.Sub(stamp) > time.Duration(FreshnessSeconds)*time.Second {
				point.DataState = "stale"
			}
		}
	}
	return out, nil
}
