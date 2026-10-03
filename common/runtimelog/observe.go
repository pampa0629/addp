package runtimelog

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/logpipeline"
)

// Observe bounds both network reads and source traversal. Missing metrics stay unknown.
func Observe(ctx context.Context, o Options, probe *ProbeReceiver, node, boot string, sequence uint64, endpoint, token, alloy string) logpipeline.Observation {
	obs := logpipeline.Observation{Schema: logpipeline.Schema, Node: node, BootID: boot, Sequence: sequence, SourceLimitBytes: o.NodeBytes, Receivers: []logpipeline.Receiver{}}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	fetch := func(target string) ([]byte, bool) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, false
		}
		res, err := client.Do(req)
		if err != nil {
			return nil, false
		}
		defer res.Body.Close()
		b, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
		return b, err == nil && res.StatusCode == 200 && len(b) <= 2<<20
	}
	_, obs.APIReady = fetch(strings.TrimRight(endpoint, "/") + "/ready")
	if data, ok := fetch(strings.TrimRight(alloy, "/") + "/metrics"); ok {
		obs.Collector = ParseCollector(string(data))
	}
	count := 0
	obs.SourcesValid = filepath.WalkDir(o.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		count++
		if count > 10000 {
			return fmt.Errorf("source traversal exceeds limit")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected source symlink")
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".jsonl") {
			info, err := d.Info()
			if err != nil {
				return err
			}
			obs.SourceBytes += info.Size()
		}
		if d.Name() != "status.json" {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		var r logpipeline.Receiver
		if err = json.NewDecoder(io.LimitReader(file, 8192)).Decode(&r); err != nil {
			return err
		}
		r.Module = filepath.Base(filepath.Dir(filepath.Dir(path)))
		if r.Module == "runtime-probe" || r.Module == "housekeeper" || time.Since(r.ObservedAt) > 120*time.Second {
			return nil
		}
		if r.InstanceID != filepath.Base(filepath.Dir(path)) || !logpipeline.Identity.MatchString(r.Module) || !logpipeline.Identity.MatchString(r.InstanceID) {
			return fmt.Errorf("invalid source identity")
		}
		if len(obs.Receivers) >= logpipeline.MaxReceivers {
			return fmt.Errorf("too many receivers")
		}
		obs.Receivers = append(obs.Receivers, r)
		return nil
	}) == nil
	if f, err := os.Open(filepath.Join(o.Root, "housekeeping-status.json")); err == nil {
		var status struct {
			ObservedAt     time.Time `json:"observed_at"`
			QuotaExhausted bool      `json:"quota_exhausted"`
			EarlyCleaned   uint64    `json:"source_files_early_cleaned"`
		}
		err = json.NewDecoder(io.LimitReader(f, 8192)).Decode(&status)
		f.Close()
		obs.HousekeepingAt = status.ObservedAt
		obs.QuotaExhausted = status.QuotaExhausted
		obs.EarlyCleaned = status.EarlyCleaned
		obs.HousekeepingValid = err == nil && time.Since(status.ObservedAt) < 120*time.Second && !status.ObservedAt.After(time.Now().Add(30*time.Second))
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	delay, err := probe.Probe(probeCtx, endpoint, token)
	obs.ProbeDelivered = err == nil
	obs.ProbeDelayMS = delay.Milliseconds()
	obs.SampledAt = time.Now().UTC()
	return obs
}

func ParseCollector(data string) logpipeline.Collector {
	c := logpipeline.Collector{}
	seen := map[string]bool{}
	values := map[string]float64{}
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.SplitN(fields[0], "{", 2)[0]
		switch name {
		case "process_start_time_seconds", "loki_write_batch_retries_total", "loki_write_dropped_entries_total", "loki_write_sent_entries_total":
		default:
			continue
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || value < 0 || value != value || value >= float64(^uint64(0)) {
			return c
		}
		values[name] += value
		seen[name] = true
	}
	c.Valid = scanner.Err() == nil && seen["process_start_time_seconds"] && seen["loki_write_batch_retries_total"] && seen["loki_write_dropped_entries_total"] && seen["loki_write_sent_entries_total"] && values["process_start_time_seconds"] > 0
	if c.Valid {
		c.StartedAt = values["process_start_time_seconds"]
		c.Retries = uint64(values["loki_write_batch_retries_total"])
		c.Dropped = uint64(values["loki_write_dropped_entries_total"])
		c.Sent = uint64(values["loki_write_sent_entries_total"])
	}
	return c
}
