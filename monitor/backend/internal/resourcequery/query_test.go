package resourcequery

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var testTime = time.Unix(1800000000, 0).UTC()

const testNode = "11111111-1111-4111-8111-111111111111"

func plan(t *testing.T, trend bool) Plan {
	t.Helper()
	p, e := NewPlan([]string{"node.memory.used_percent"}, testTime.Add(-30*time.Second), testTime, testTime, trend, nil, DefaultBudget())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestPlansAndWhitelistRejectInjectionAndBoundSevenDays(t *testing.T) {
	for _, keys := range [][]string{nil, {"up"}, {"node.memory.used_percent", "node.memory.used_percent"}, {`node.memory.used_percent} or up{`}} {
		if _, e := NewPlan(keys, testTime, testTime, testTime, false, nil, DefaultBudget()); !errors.Is(e, ErrInvalid) {
			t.Fatalf("keys=%q err=%v", keys, e)
		}
	}
	keys := []string{}
	for _, d := range Catalog() {
		if !d.Filesystem() {
			keys = append(keys, d.Key)
		}
	}
	zone := time.FixedZone("offset", 8*3600)
	utcPlan, e := NewPlan([]string{"node.load.average_1m"}, testTime.Add(-time.Minute).In(zone), testTime.In(zone), testTime, true, nil, DefaultBudget())
	if e != nil || utcPlan.Start.Location() != time.UTC || utcPlan.End.Location() != time.UTC {
		t.Fatal("response timeline not normalized to UTC", e)
	}
	p, e := NewPlan(keys, testTime.Add(-7*24*time.Hour), testTime, testTime, true, nil, DefaultBudget())
	if e != nil || p.Points > 1000 || p.Points*len(keys) > 20000 || p.StepSeconds%15 != 0 {
		t.Fatalf("plan=%+v err=%v", p, e)
	}
	if _, e := NewPlan(keys, testTime.Add(-7*24*time.Hour-time.Second), testTime, testTime, true, nil, DefaultBudget()); !errors.Is(e, ErrBudget) {
		t.Fatal(e)
	}
	tiny := DefaultBudget()
	tiny.MaxTotalPoints = 2
	if _, e := NewPlan(keys, testTime, testTime, testTime, false, nil, tiny); !errors.Is(e, ErrBudget) {
		t.Fatal("instant output budget bypass", e)
	}
	if _, e := NewPlan(keys, testTime.Add(time.Second), testTime.Add(time.Minute), testTime, true, nil, DefaultBudget()); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	b := DefaultBudget()
	b.MaxTotalPoints = 2
	if _, e := NewPlan(keys, testTime.Add(-time.Minute), testTime, testTime, true, nil, b); !errors.Is(e, ErrBudget) {
		t.Fatal(e)
	}
	expression, e := p.Expression(Scope{NodeID: testNode, Instance: `127.0.0.1:9100"} or up{`})
	if e != nil || !strings.Contains(expression, `instance="127.0.0.1:9100\"} or up{"`) {
		t.Fatalf("instance not quoted: %s %v", expression, e)
	}
	if _, e := p.Expression(Scope{NodeID: "injection", Instance: "host:9100"}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	// Timestamp equality is a same-scrape guard on memory and CPU capacity.
	if !strings.Contains(expression, "timestamp(node_memory_MemAvailable_bytes") || !strings.Contains(expression, "count(node_cpu_seconds_total") || !strings.Contains(expression, "rate(") {
		t.Fatal(expression)
	}
}
func wire(t *testing.T, body string) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatal(err)
	}
	return e
}
func rawRow(component string, values string, trend bool) string {
	field := "value"
	if trend {
		field = "values"
	}
	return `{"metric":{"addp_metric":"node.memory.used_percent","addp_component":"` + component + `"},"` + field + `":` + values + `}`
}
func data(rows string, trend bool) string {
	kind := "vector"
	if trend {
		kind = "matrix"
	}
	return `{"status":"success","data":{"resultType":"` + kind + `","result":[` + rows + `]}}`
}
func pair(at int64, value string) string {
	return `[` + strconv.FormatInt(at, 10) + `,"` + value + `"]`
}
func TestNormalizeZeroGapsStaleFutureAndPartialEvidence(t *testing.T) {
	p := plan(t, false)
	for _, tc := range []struct {
		name, value, stamp, state string
		null                      bool
	}{
		{"zero", "0", "1800000000", "valid", false}, {"stale", "12", "1799999939", "stale", false},
		{"fresh boundary", "12", "1799999940", "valid", false}, {"future", "12", "1800000001", "no_data", true},
		{"nan", "NaN", "1800000000", "no_data", true}, {"infinity", "+Inf", "1800000000", "no_data", true},
		{"invalid percent", "101", "1800000000", "no_data", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := rawRow("value", pair(testTime.Unix(), tc.value), false) + "," + rawRow("sampled_at", pair(testTime.Unix(), tc.stamp), false)
			result, e := normalize(wire(t, data(rows, false)), p, DefaultBudget())
			if e != nil {
				t.Fatal(e)
			}
			point := result[0].Points[0]
			if point.DataState != tc.state || (point.Value == nil) != tc.null {
				t.Fatalf("point=%+v", point)
			}
		})
	}
	for _, rows := range []string{"", rawRow("value", pair(testTime.Unix(), "10"), false)} {
		result, e := normalize(wire(t, data(rows, false)), p, DefaultBudget())
		if e != nil || result[0].Points[0].Value != nil || result[0].Points[0].DataState != "no_data" {
			t.Fatalf("partial=%+v %v", result, e)
		}
	}
	p = plan(t, true)
	rows := rawRow("value", "["+pair(testTime.Unix()-30, "0")+","+pair(testTime.Unix(), "2")+"]", true) + "," + rawRow("sampled_at", "["+pair(testTime.Unix()-30, "1799999970")+","+pair(testTime.Unix(), "1800000000")+"]", true)
	result, e := normalize(wire(t, data(rows, true)), p, DefaultBudget())
	if e != nil || len(result[0].Points) != 3 || result[0].Points[1].Value != nil || result[0].Points[0].Value == nil {
		t.Fatalf("gaps=%+v %v", result, e)
	}
}
func TestRejectForeignDuplicateMisalignedAndOverBudgetResponses(t *testing.T) {
	p := plan(t, false)
	row := rawRow("value", pair(testTime.Unix(), "1"), false)
	for _, rows := range []string{row + "," + row, strings.Replace(row, "node.memory.used_percent", "node.hidden", 1), strings.Replace(row, "1800000000", "1800000001", 1), strings.Replace(row, `"addp_component"`, `"instance":"private:9100","addp_component"`, 1)} {
		if _, e := normalize(wire(t, data(rows, false)), p, DefaultBudget()); !errors.Is(e, ErrUnavailable) {
			t.Fatalf("accepted %s: %v", rows, e)
		}
	}
	if _, e := normalize(wire(t, data(row+","+row+","+row, false)), p, DefaultBudget()); !errors.Is(e, ErrBudget) {
		t.Fatal(e)
	}
}

func TestFilesystemPlansBudgetDimensionsAndMissingEvidence(t *testing.T) {
	keys := []string{"node.filesystem.total_bytes", "node.filesystem.free_bytes", "node.filesystem.available_bytes", "node.filesystem.used_bytes", "node.filesystem.used_percent"}
	b := DefaultBudget()
	dims := Dimensions{"device": "/dev/a", "mountpoint": "/data space\t", "fstype": "ext4"}
	all, err := NewPlan(keys, testTime.Add(-7*24*time.Hour), testTime, testTime, true, nil, b)
	if err != nil || all.FilesystemGroups != 20 || all.SeriesUpperBound != 100 || all.Points*all.SeriesUpperBound > 20000 {
		t.Fatal(all, err)
	}
	selected, err := NewPlan(keys, testTime.Add(-7*24*time.Hour), testTime, testTime, true, dims, b)
	if err != nil || selected.FilesystemGroups != 1 || selected.SeriesUpperBound != 5 || selected.StepSeconds >= all.StepSeconds {
		t.Fatal(selected, err)
	}
	for _, dimensions := range []Dimensions{{"device": "a"}, {"device": "a", "mountpoint": "relative", "fstype": "x"}, {"device": "a", "mountpoint": "/", "fstype": "x", "extra": "secret"}} {
		if _, err := NewPlan(keys, testTime, testTime, testTime, false, dimensions, b); !errors.Is(err, ErrInvalid) {
			t.Fatal(dimensions, err)
		}
	}
	if _, err := NewPlan([]string{"node.load.average_1m"}, testTime, testTime, testTime, false, dims, b); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := NewPlan(append(keys, "node.cpu.logical_cores"), testTime, testTime, testTime, false, nil, Budget{MaxMetrics: 12, MaxSeries: 5, MaxPointsPerSeries: 1000, MaxTotalPoints: 20000, MaxRangeSeconds: 604800, TimeoutSeconds: 5, SubjectConcurrency: 2, ProcessConcurrency: 8}); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
	rows, err := normalize(wire(t, data("", true)), selected, b)
	if err != nil || len(rows) != 5 || rows[0].Dimensions.identity() != dims.identity() || rows[0].Points[0].Value != nil {
		t.Fatal(rows, err)
	}
}

func TestFilesystemNormalizeMountUnionFailuresAndStrictScope(t *testing.T) {
	key := "node.filesystem.used_percent"
	b := DefaultBudget()
	b.MaxSeries = 2
	p, err := NewPlan([]string{key}, testTime.Add(-30*time.Second), testTime, testTime, true, nil, b)
	if err != nil {
		t.Fatal(err)
	}
	dim := Dimensions{"device": "/dev/a", "mountpoint": "/", "fstype": "ext4"}
	makeRow := func(component string, dimensions Dimensions, samples string) string {
		labels := dimensions.Copy()
		labels["addp_metric"], labels["addp_component"] = key, component
		encoded, _ := json.Marshal(labels)
		return `{"metric":` + string(encoded) + `,"values":` + samples + `}`
	}
	first := "[" + pair(testTime.Unix()-30, "1799999970") + "]"
	last := "[" + pair(testTime.Unix(), "1800000000") + "]"
	root := makeRow("value", dim, "["+pair(testTime.Unix()-30, "0")+"]") + "," + makeRow("sampled_at", dim, first) + "," + makeRow("observed_at", dim, first)
	bind := dim.Copy()
	bind["mountpoint"] = "/bind"
	// A known mount with a failing statfs emits only presence, never a false zero.
	rows, err := normalize(wire(t, data(root+","+makeRow("observed_at", bind, last), true)), p, b)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for _, row := range rows {
		if row.Dimensions["mountpoint"] == "/" {
			if row.Points[0].Value == nil || *row.Points[0].Value != 0 || row.Points[1].Value != nil {
				t.Fatal(row)
			}
		} else if row.Points[2].Value != nil || row.Points[2].DataState != "no_data" {
			t.Fatal(row)
		}
	}
	third := dim.Copy()
	third["mountpoint"] = "/third"
	if _, err := normalize(wire(t, data(root+","+makeRow("observed_at", bind, last)+","+makeRow("observed_at", third, last), true)), p, b); !errors.Is(err, ErrBudget) {
		t.Fatal("historical mount union bypass", err)
	}
	selected, _ := NewPlan([]string{key}, p.Start, p.End, testTime, true, dim, b)
	if _, err := normalize(wire(t, data(makeRow("observed_at", bind, last), true)), selected, b); !errors.Is(err, ErrUnavailable) {
		t.Fatal("foreign mount accepted", err)
	}
	if _, err := normalize(wire(t, data(root+","+makeRow("observed_at", dim, first), true)), p, b); !errors.Is(err, ErrUnavailable) {
		t.Fatal("duplicate evidence accepted", err)
	}
	foreign := dim.Copy()
	foreign["device_error"] = "secret error"
	if _, err := normalize(wire(t, data(makeRow("observed_at", foreign, last), true)), p, b); !errors.Is(err, ErrUnavailable) {
		t.Fatal("private label leaked", err)
	}
}
func TestLimiterNoQueueAndHotBudgetDecrease(t *testing.T) {
	var l Limiter
	b := DefaultBudget()
	r1, e := l.Acquire("user", b)
	if e != nil {
		t.Fatal(e)
	}
	r2, e := l.Acquire("user", b)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := l.Acquire("user", b); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	b.SubjectConcurrency = 1
	r1()
	r1()
	if _, e := l.Acquire("user", b); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	r2()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, e := l.Acquire("user", b)
			if e == nil {
				release()
			}
		}()
	}
	wg.Wait()
	if l.total != 0 || len(l.subjects) != 0 {
		t.Fatal("limiter leaked subjects")
	}
}

func tlsFixture(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server, []byte, tls.Certificate) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, e := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	issue := func(serial int64, server bool) tls.Certificate {
		k, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		cert := &x509.Certificate{SerialNumber: big.NewInt(serial), NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		if server {
			cert.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
			cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		}
		der, e := x509.CreateCertificate(rand.Reader, cert, ca, &k.PublicKey, key)
		if e != nil {
			t.Fatal(e)
		}
		return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: k}
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{issue(2, true)}, MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}
	server.StartTLS()
	t.Cleanup(server.Close)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	clientCertificate := issue(3, false)
	c, e := NewClient(server.URL, caPEM, clientCertificate)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(c.Close)
	return c, server, caPEM, clientCertificate
}
func TestRealMutualTLSQueryFailuresDoNotBecomeEmptySuccess(t *testing.T) {
	var mode atomic.Value
	mode.Store("ok")
	var calls atomic.Int32
	c, server, _, _ := tlsFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || r.URL.Path != "/api/v1/query" || r.URL.Query().Has("limit") || r.URL.Query().Get("lookback_delta") != "300s" || !strings.Contains(r.URL.Query().Get("query"), `instance="127.0.0.1:9100"`) {
			t.Error("transport escaped fixed query protocol")
		}
		switch mode.Load().(string) {
		case "redirect":
			w.Header().Set("Location", "https://private.invalid:9443/")
			w.WriteHeader(302)
		case "timeout":
			w.WriteHeader(503)
			w.Write([]byte(`{"status":"error","errorType":"timeout"}`))
		case "error":
			w.WriteHeader(500)
			w.Write([]byte(`{"status":"error","error":"private-internal-detail"}`))
		case "warning":
			w.Write([]byte(`{"status":"success","warnings":["incomplete"],"data":{"resultType":"vector","result":[]}}`))
		case "limit":
			w.Write([]byte(strings.Repeat("a", responseLimit+1)))
		default:
			w.Write([]byte(data("", false)))
		}
	})
	p := plan(t, false)
	scope := Scope{NodeID: testNode, Instance: "127.0.0.1:9100"}
	for _, tc := range []struct {
		mode string
		want error
	}{{"ok", nil}, {"redirect", ErrUnavailable}, {"error", ErrUnavailable}, {"warning", ErrUnavailable}, {"timeout", context.DeadlineExceeded}, {"limit", ErrBudget}} {
		mode.Store(tc.mode)
		result, e := c.Query(context.Background(), p, scope, DefaultBudget())
		if !errors.Is(e, tc.want) {
			t.Fatalf("mode=%s error=%v", mode, e)
		}
		if e != nil && result != nil {
			t.Fatal("backend failure packaged as data")
		}
	}
	if calls.Load() != 6 {
		t.Fatal("redirect followed")
	}
	// An unauthenticated client is rejected by the same server before its handler.
	anonymous := server.Client()
	if response, e := anonymous.Get(server.URL); e == nil {
		response.Body.Close()
		t.Fatal("anonymous query accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.Query(ctx, p, scope, DefaultBudget()); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestQueryOriginMustBeAnExplicitHTTPSOrigin(t *testing.T) {
	_, _, ca, cert := tlsFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	for _, origin := range []string{"http://localhost:9090", "https://localhost", "https://localhost:09090", "https://localhost:9090/", "https://localhost:9090?", "https://localhost:9090#", "https://localhost:9090?query=up", "https://user:secret@localhost:9090", "https://localhost:9090/api/v1/query"} {
		if c, e := NewClient(origin, ca, cert); !errors.Is(e, ErrInvalid) {
			if c != nil {
				c.Close()
			}
			t.Fatalf("accepted non-origin %q", origin)
		}
	}
}

func TestQueryClientRejectsSigningAuthorityAsClientCredential(t *testing.T) {
	_, server, ca, cert := tlsFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	block, _ := pem.Decode(ca)
	cert.Certificate = [][]byte{block.Bytes}
	if c, e := NewClient(server.URL, ca, cert); !errors.Is(e, ErrInvalid) {
		if c != nil {
			c.Close()
		}
		t.Fatal("signing authority accepted as query credential")
	}
}

func TestCPUBusyWindowMetadataAndPercentageEvidence(t *testing.T) {
	p, err := NewPlan([]string{"node.cpu.busy_percent"}, testTime, testTime, testTime, false, nil, DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"no_data", "not_connected"} {
		row := Empty(p, state)[0]
		if row.Unit != "percent" || row.WindowSeconds != 60 || row.Points[0].Value != nil {
			t.Fatalf("CPU window lost on %s: %+v", state, row)
		}
	}
	for _, value := range []string{"0", "50", "100", "100.01", "-0.01", "NaN", "+Inf"} {
		raw := data(rawRow("value", pair(testTime.Unix(), value), false)+","+rawRow("sampled_at", pair(testTime.Unix(), strconv.FormatInt(testTime.Unix()-1, 10)), false), false)
		raw = strings.ReplaceAll(raw, "node.memory.used_percent", "node.cpu.busy_percent")
		rows, err := normalize(wire(t, raw), p, DefaultBudget())
		if err != nil {
			t.Fatal(err)
		}
		valid := value == "0" || value == "50" || value == "100"
		if (rows[0].Points[0].DataState == "valid") != valid || rows[0].WindowSeconds != 60 {
			t.Fatalf("CPU value %s: %+v", value, rows)
		}
		if !valid && rows[0].Points[0].Value != nil {
			t.Fatal("invalid percent fabricated as finite value")
		}
	}
}

func TestInodeBudgetAndStrictIntegerEvidence(t *testing.T) {
	keys := []string{"node.filesystem.inodes_total", "node.filesystem.inodes_free", "node.filesystem.inodes_used", "node.filesystem.inodes_used_percent"}
	b := DefaultBudget()
	dims := Dimensions{"device": "/dev/a", "mountpoint": "/", "fstype": "ext4"}
	p, err := NewPlan(keys, testTime.Add(-7*24*time.Hour), testTime, testTime, true, nil, b)
	if err != nil || p.FilesystemGroups != 25 || p.SeriesUpperBound != 100 || p.Points*p.SeriesUpperBound > 20000 {
		t.Fatal(p, err)
	}
	selected, err := NewPlan(keys, testTime.Add(-7*24*time.Hour), testTime, testTime, true, dims, b)
	if err != nil || selected.SeriesUpperBound != 4 || selected.StepSeconds >= p.StepSeconds {
		t.Fatal(selected, err)
	}
	rows, err := normalize(wire(t, data("", true)), selected, b)
	if err != nil || len(rows) != 4 || rows[0].Dimensions.identity() != dims.identity() || rows[0].Points[0].DataState != "no_data" {
		t.Fatal(rows, err)
	}
	instant, _ := NewPlan(keys[1:2], testTime, testTime, testTime, false, dims, b)
	row := func(component, value string) string {
		labels := dims.Copy()
		labels["addp_metric"], labels["addp_component"] = keys[1], component
		encoded, _ := json.Marshal(labels)
		return `{"metric":` + string(encoded) + `,"value":` + pair(testTime.Unix(), value) + `}`
	}
	for _, value := range []string{"10.5", "9007199254740992", "+Inf", "-1", "0", "9007199254740991"} {
		result, err := normalize(wire(t, data(row("value", value)+","+row("sampled_at", "1800000000")+","+row("observed_at", "1800000000"), false)), instant, b)
		if err != nil {
			t.Fatal(err)
		}
		valid := value == "0" || value == "9007199254740991"
		if (result[0].Points[0].DataState == "valid") != valid {
			t.Fatal(value, result)
		}
	}
}
