package capture

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
)

var exactMetricServer = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,255}$`)

func (c *ConnectClient) Delay(ctx context.Context, name string) (*CaptureDelay, error) {
	if c.metricsURL == "" || !exactMetricServer.MatchString(name) {
		return nil, fmt.Errorf("capture delay unavailable")
	}
	bean := "debezium.postgres:context=streaming,server=" + name + ",type=connector-metrics"
	body, _ := json.Marshal(map[string]any{"type": "read", "mbean": bean, "attribute": []string{"Connected", "MilliSecondsBehindSource"}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.metricsURL+"/jolokia/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// No Connect REST credentials are forwarded to the distinct metrics endpoint.
	client := &http.Client{Timeout: c.client.Timeout, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("capture delay unavailable")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(raw) > 16384 {
		return nil, fmt.Errorf("invalid capture delay response")
	}
	var value struct {
		Status    int    `json:"status"`
		ErrorType string `json:"error_type"`
		Request   struct {
			MBean string `json:"mbean"`
		} `json:"request"`
		Value *struct {
			Connected *bool    `json:"Connected"`
			Delay     *float64 `json:"MilliSecondsBehindSource"`
		} `json:"value"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Request.MBean != bean {
		return nil, fmt.Errorf("unmatched capture delay response")
	}
	if response.StatusCode == 404 && value.Status == 404 && value.ErrorType == "javax.management.InstanceNotFoundException" {
		return &CaptureDelay{Status: "no_sample"}, nil
	}
	if response.StatusCode != 200 || value.Status != 200 || value.Value == nil || value.Value.Connected == nil || value.Value.Delay == nil {
		return nil, fmt.Errorf("capture delay unavailable")
	}
	ms := *value.Value.Delay
	if math.IsNaN(ms) || math.IsInf(ms, 0) || math.Trunc(ms) != ms {
		return nil, fmt.Errorf("invalid capture delay value")
	}
	result := &CaptureDelay{Status: "available", Connected: value.Value.Connected}
	if ms != -1 && *value.Value.Connected {
		seconds := ms / 1000
		result.Seconds = &seconds
	}
	return result, nil
}
