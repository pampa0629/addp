package spark

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	commoni18n "github.com/addp/common/middleware/i18n"
)

const maxStateBytes = 1 << 20

type masterState struct {
	URL          string `json:"url"`
	Status       string `json:"status"`
	AliveWorkers int    `json:"aliveworkers"`
	Workers      []struct {
		ID    string `json:"id"`
		State string `json:"state"`
	} `json:"workers"`
}

// Native facts validate the declared UI; they never replace the configured RPC
// endpoint. Docker/NAT may publish another host or port. No redirects or raw
// upstream bodies/errors are exposed, and TLS uses the system trust store.
func probeMaster(ctx context.Context, endpoint, stateURL string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{}).DialContext, TLSHandshakeTimeout: 5 * time.Second, MaxResponseHeaderBytes: 64 << 10}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, stateURL, nil)
	if err != nil {
		return commoni18n.NewMessageError("engine.spark.state_invalid")
	}
	response, err := client.Do(request)
	if err != nil {
		return commoni18n.NewMessageError("engine.spark.state_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return commoni18n.NewMessageError("engine.spark.state_unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxStateBytes+1))
	if err != nil || len(body) > maxStateBytes {
		return commoni18n.NewMessageError("engine.spark.state_invalid")
	}
	if err := validateMasterState(body); err != nil {
		return err
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", endpoint)
	if err != nil {
		return commoni18n.NewMessageError("engine.spark.rpc_unavailable")
	}
	return connection.Close()
}

func validateMasterState(body []byte) error {
	var state masterState
	if json.Unmarshal(body, &state) != nil {
		return commoni18n.NewMessageError("engine.spark.state_invalid")
	}
	u, err := url.Parse(state.URL)
	if err != nil || u.Scheme != "spark" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return commoni18n.NewMessageError("engine.spark.state_invalid")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return commoni18n.NewMessageError("engine.spark.state_invalid")
	}
	if state.Status != "ALIVE" || state.AliveWorkers <= 0 {
		return commoni18n.NewMessageError("engine.spark.capacity_unavailable")
	}
	alive := 0
	seen := map[string]bool{}
	for _, worker := range state.Workers {
		if worker.ID == "" || seen[worker.ID] {
			return commoni18n.NewMessageError("engine.spark.state_invalid")
		}
		seen[worker.ID] = true
		if worker.State == "ALIVE" {
			alive++
		}
	}
	if alive != state.AliveWorkers {
		return commoni18n.NewMessageError("engine.spark.state_invalid")
	}
	return nil
}
