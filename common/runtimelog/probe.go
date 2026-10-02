package runtimelog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Probe writes through the normal receiver and waits for the exact event in Loki.
func Probe(ctx context.Context, o Options, endpoint, token string) (time.Duration, error) {
	o.Module = "runtime-probe"
	o.InstanceID = uuid.NewString()
	o.Role = "backend"
	started := time.Now()
	if err := Capture(o, strings.NewReader("runtime-log-delivery-probe\n"), strings.NewReader("")); err != nil {
		return 0, err
	}
	target, err := url.Parse(endpoint)
	if err != nil || target.Host == "" || target.User != nil || (target.Scheme != "http" && target.Scheme != "https") {
		return 0, fmt.Errorf("invalid probe endpoint")
	}
	target.Path = "/loki/api/v1/query_range"
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for {
		target.RawQuery = url.Values{"query": {`{deployment="addp",module_name="runtime-probe"} | instance_id=` + strconv.Quote(o.InstanceID)}, "start": {strconv.FormatInt(started.Add(-time.Second).UnixNano(), 10)}, "end": {strconv.FormatInt(time.Now().UnixNano(), 10)}, "limit": {"10"}}.Encode()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, e := client.Do(req)
		if e == nil {
			var data struct {
				Status string `json:"status"`
				Data   struct {
					Result []struct {
						Values [][]string `json:"values"`
					} `json:"result"`
				} `json:"data"`
			}
			e = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&data)
			res.Body.Close()
			if e == nil && res.StatusCode == 200 && data.Status == "success" {
				for _, stream := range data.Data.Result {
					for _, v := range stream.Values {
						if len(v) != 2 {
							continue
						}
						var entry Entry
						if json.Unmarshal([]byte(v[1]), &entry) == nil && entry.InstanceID == o.InstanceID && entry.Module == o.Module && entry.Message == "runtime-log-delivery-probe" {
							return time.Since(started), nil
						}
					}
				}
			}
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return time.Since(started), ctx.Err()
		case <-timer.C:
		}
	}
}
