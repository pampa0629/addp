package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/addp/monitor/internal/frontendprobe"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

type kubeConfig struct {
	API       string `json:"api_origin"`
	Kubelet   string `json:"kubelet_origin"`
	Namespace string `json:"namespace"`
	Node      string `json:"node"`
	CAFile    string `json:"ca_file"`
	TokenFile string `json:"token_file"`
}

var kubeName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func (k kubeConfig) valid() bool {
	for _, origin := range []string{k.API, k.Kubelet} {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || origin != "https://"+u.Host {
			return false
		}
	}
	return kubeName.MatchString(k.Namespace) && len(k.Namespace) <= 63 && kubeName.MatchString(k.Node) && filepath.IsAbs(k.CAFile) && filepath.IsAbs(k.TokenFile)
}

type kubeMeta struct {
	Name, Namespace, UID string
	DeletionTimestamp    *time.Time
	OwnerReferences      []struct {
		APIVersion, Kind, Name, UID string
		Controller                  bool
	}
}

func (m kubeMeta) controller(kind string) string {
	for _, r := range m.OwnerReferences {
		if r.Controller && r.APIVersion == "apps/v1" && r.Kind == kind {
			return r.UID
		}
	}
	return ""
}

type containerSample struct {
	cpu, memory *float64
	timestamp   int64
}

func kubeSamples(raw []byte, namespace string, now time.Time) (map[[2]string]*containerSample, error) {
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	rows := map[[2]string]*containerSample{}
	for _, name := range []string{"container_cpu_usage_seconds_total", "container_memory_working_set_bytes"} {
		for _, metric := range families[name].GetMetric() {
			labels := map[string]string{}
			for _, l := range metric.Label {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["namespace"] != namespace {
				continue
			}
			key := [2]string{labels["pod"], labels["container"]}
			ts := metric.GetTimestampMs()
			if ts > now.UnixMilli() || ts < now.Add(-30*time.Second).UnixMilli() {
				continue
			}
			row := rows[key]
			if row == nil {
				row = &containerSample{timestamp: ts}
				rows[key] = row
			}
			if row.timestamp != ts {
				return nil, errConfiguration
			}
			var field **float64
			var value *float64
			switch name {
			case "container_cpu_usage_seconds_total":
				field = &row.cpu
				if metric.Counter != nil {
					value = metric.Counter.Value
				}
			case "container_memory_working_set_bytes":
				field = &row.memory
				if metric.Gauge != nil {
					value = metric.Gauge.Value
				}
			}
			if value == nil || *field != nil || *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
				return nil, errConfiguration
			}
			*field = value
		}
	}
	return rows, nil
}

func (k kubeConfig) snapshot(ctx context.Context) ([]frontend, map[string]*resource, error) {
	ca, err := os.ReadFile(k.CAFile)
	if err != nil {
		return nil, nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, nil, errConfiguration
	}
	token, err := os.ReadFile(k.TokenFile)
	if err != nil {
		return nil, nil, err
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	defer tr.CloseIdleConnections()
	// RoundTrip keeps the bearer token on the two configured TLS origins.
	get := func(endpoint string) ([]byte, int, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
		resp, err := tr.RoundTrip(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, resp.StatusCode, fmt.Errorf("Kubernetes API status %d", resp.StatusCode)
		}
		const max = 8 << 20
		b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
		if len(b) > max {
			return nil, 0, errConfiguration
		}
		return b, resp.StatusCode, err
	}
	base := k.API + "/apis/apps/v1/namespaces/" + k.Namespace + "/"
	deps := map[string]frontend{}
	for _, module := range frontendprobe.Modules {
		service := module + "-frontend"
		if module == "console" {
			service = module
		}
		raw, status, err := get(base + "deployments/" + service)
		if status == http.StatusNotFound {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		var d struct{ Metadata kubeMeta }
		if json.Unmarshal(raw, &d) != nil || d.Metadata.UID == "" || d.Metadata.Name != service || d.Metadata.Namespace != k.Namespace {
			return nil, nil, errConfiguration
		}
		if d.Metadata.DeletionTimestamp == nil {
			deps[d.Metadata.UID] = frontend{module: module, service: service, entry: frontendEntry(module)}
		}
	}
	raw, _, err := get(base + "replicasets")
	if err != nil {
		return nil, nil, err
	}
	var sets struct{ Items []struct{ Metadata kubeMeta } }
	if json.Unmarshal(raw, &sets) != nil {
		return nil, nil, errConfiguration
	}
	rs := map[string]frontend{}
	for _, r := range sets.Items {
		m := r.Metadata
		if f, ok := deps[m.controller("Deployment")]; ok && m.Namespace == k.Namespace && m.UID != "" && m.DeletionTimestamp == nil {
			rs[m.UID] = f
		}
	}
	type identity struct {
		pod, uid, id string
		started      time.Time
		restarts     uint64
	}
	identities := func() (map[frontend]identity, error) {
		raw, _, err := get(k.API + "/api/v1/namespaces/" + k.Namespace + "/pods?fieldSelector=spec.nodeName%3D" + k.Node)
		if err != nil {
			return nil, err
		}
		var pods struct {
			Items []struct {
				Metadata kubeMeta
				Spec     struct{ NodeName string }
				Status   struct {
					ContainerStatuses []struct {
						Name, ContainerID string
						RestartCount      uint64
						State             struct {
							Running *struct{ StartedAt time.Time }
						}
					}
				}
			}
		}
		if json.Unmarshal(raw, &pods) != nil {
			return nil, errConfiguration
		}
		found := map[frontend]identity{}
		for _, p := range pods.Items {
			m := p.Metadata
			f, ok := rs[m.controller("ReplicaSet")]
			if !ok || m.Namespace != k.Namespace || m.UID == "" || m.DeletionTimestamp != nil || p.Spec.NodeName != k.Node {
				continue
			}
			for _, c := range p.Status.ContainerStatuses {
				if c.Name != f.service || c.State.Running == nil || c.ContainerID == "" {
					continue
				}
				if _, seen := found[f]; seen {
					found[f] = identity{}
					continue
				}
				found[f] = identity{pod: m.Name, uid: m.UID, id: c.ContainerID, started: c.State.Running.StartedAt, restarts: c.RestartCount}
			}
		}
		return found, nil
	}
	before, err := identities()
	if err != nil {
		return nil, nil, err
	}
	raw, _, err = get(k.Kubelet + "/metrics/resource")
	if err != nil {
		return nil, nil, err
	}
	samples, err := kubeSamples(raw, k.Namespace, time.Now())
	if err != nil {
		return nil, nil, err
	}

	after, err := identities()
	if err != nil {
		return nil, nil, err
	}
	var rows []frontend
	resources := map[string]*resource{}
	for f, c := range after {
		rows = append(rows, f)
		if c.id == "" || before[f] != c {
			continue
		}
		sample := samples[[2]string{c.pod, f.service}]
		// Pod status truncates the runtime start instant to seconds. Require a usage
		// timestamp strictly after that second, including when kubelet serves cached data.
		if sample == nil || sample.cpu == nil || sample.memory == nil || c.started.IsZero() || sample.timestamp < c.started.Add(time.Second).UnixMilli() {
			continue
		}
		resources[f.module] = &resource{cpuSeconds: *sample.cpu, memoryBytes: *sample.memory, generation: c.uid + "/" + c.id, restarts: &c.restarts}
	}
	return rows, resources, nil
}
