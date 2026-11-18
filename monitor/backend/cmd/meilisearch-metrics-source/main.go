package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"regexp"
	"time"

	"github.com/addp/monitor/internal/sourceserver"
)

var unavailable = errors.New("Meilisearch observation unavailable")
var taskKey = regexp.MustCompile("^[a-f0-9]{64}$")

type configuration struct {
	Address netip.Addr `json:"address"`
	Key     string     `json:"task_key"`
}

type taskPage struct {
	Total   *uint64 `json:"total"`
	Results []struct {
		Status     string    `json:"status"`
		EnqueuedAt time.Time `json:"enqueuedAt"`
	} `json:"results"`
}

func read(ctx context.Context, client *http.Client, endpoint, key string, now func() time.Time) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	fetch := func(status, order string) (taskPage, error) {
		var page taskPage
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/tasks?limit=1&statuses="+status+order, nil)
		if err != nil {
			return page, unavailable
		}
		request.Header.Set("Authorization", "Bearer "+key)
		response, err := client.Do(request)
		if err != nil {
			return page, unavailable
		}
		defer response.Body.Close()
		const limit = 64 * 1024
		raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
		if response.StatusCode != http.StatusOK || err != nil || len(raw) > limit || json.Unmarshal(raw, &page) != nil ||
			page.Total == nil || page.Results == nil || len(page.Results) > 1 || (*page.Total == 0) != (len(page.Results) == 0) {
			return taskPage{}, unavailable
		}
		if len(page.Results) == 1 && page.Results[0].Status != status {
			return taskPage{}, unavailable
		}
		return page, nil
	}
	pending, err := fetch("enqueued", "&reverse=true")
	if err != nil {
		return nil, err
	}
	failed, err := fetch("failed", "")
	if err != nil || ctx.Err() != nil {
		return nil, unavailable
	}
	at := now()
	var age float64
	if len(pending.Results) == 1 {
		queued := pending.Results[0].EnqueuedAt
		if queued.IsZero() || queued.After(at) {
			return nil, unavailable
		}
		age = at.Sub(queued).Seconds()
	}
	return fmt.Appendf(nil, "addp_meilisearch_tasks_pending %d\naddp_meilisearch_tasks_oldest_wait_seconds %g\naddp_meilisearch_tasks_retained_failed %d\naddp_meilisearch_observed_at_seconds %d\n",
		*pending.Total, age, *failed.Total, at.Unix()), nil
}

func run() error {
	file, err := os.Open("/source/config.json")
	if err != nil {
		return unavailable
	}
	defer file.Close()
	var cfg configuration
	decoder := json.NewDecoder(io.LimitReader(file, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cfg) != nil || !cfg.Address.IsPrivate() || cfg.Address.Is4In6() || cfg.Address.Zone() != "" || !taskKey.MatchString(cfg.Key) {
		return unavailable
	}
	if _, err := decoder.Token(); err != io.EOF {
		return unavailable
	}
	transport := &http.Transport{
		DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, DisableKeepAlives: true,
		ResponseHeaderTimeout: 2 * time.Second, MaxResponseHeaderBytes: 8192,
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	endpoint := "http://" + netip.AddrPortFrom(cfg.Address, 7700).String()
	return sourceserver.Serve(sourceserver.Handler(func(ctx context.Context) ([]byte, error) {
		return read(ctx, client, endpoint, cfg.Key, time.Now)
	}), "/source")
}

func main() {
	if run() != nil {
		_, _ = io.WriteString(os.Stderr, "Meilisearch observation source unavailable\n")
		os.Exit(1)
	}
}
