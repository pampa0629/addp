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
	"sync"
	"time"

	"github.com/google/uuid"
)

// Technical sources retain recent diagnostic events, not business history.
const probeSourceAge = time.Hour

// ProbeReceiver owns one Capture lifetime. A detection event is not a source
// instance: repeated detections must preserve both source identity and sequence.
type ProbeReceiver struct {
	o        Options
	input    *probeInput
	busy     chan struct{}
	closed   sync.Once
	done     chan error
	ctx      context.Context
	cancel   context.CancelFunc
	closeErr error
}

func NewProbeReceiver(o Options) *ProbeReceiver {
	o.Module = "runtime-probe"
	o.InstanceID = uuid.NewString()
	o.Role = "backend"
	ctx, cancel := context.WithCancel(context.Background())
	p := &ProbeReceiver{o: o, input: &probeInput{events: make(chan []byte, 1), closed: ctx.Done()}, busy: make(chan struct{}, 1), done: make(chan error, 1), ctx: ctx, cancel: cancel}
	go func() { p.done <- Capture(o, p.input, strings.NewReader("")) }()
	return p
}

// Close stops input and waits for Capture to flush and release its segment lock.
func (p *ProbeReceiver) Close() error {
	p.closed.Do(func() {
		p.cancel()
		p.closeErr = <-p.done
	})
	return p.closeErr
}

// probeInput admits at most one bounded diagnostic message without blocking a
// caller on filesystem I/O. Only Capture reads pending; Close drains admitted input.
type probeInput struct {
	events  chan []byte
	closed  <-chan struct{}
	pending []byte
}

func (r *probeInput) Read(dst []byte) (int, error) {
	if len(r.pending) == 0 {
		select {
		case r.pending = <-r.events:
		case <-r.closed:
			select {
			case r.pending = <-r.events:
			default:
				return 0, io.EOF
			}
		}
	}
	n := copy(dst, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

// Probe writes through the normal receiver and waits for this exact new event.
func (p *ProbeReceiver) Probe(ctx context.Context, endpoint, token string) (time.Duration, error) {
	started := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(p.ctx, cancel)
	defer func() { stop(); cancel() }()
	target, err := url.Parse(endpoint)
	if err != nil || target.Host == "" || target.User != nil || (target.Scheme != "http" && target.Scheme != "https") {
		return 0, fmt.Errorf("invalid probe endpoint")
	}
	select {
	case p.busy <- struct{}{}:
		defer func() { <-p.busy }()
	case <-ctx.Done():
		return time.Since(started), ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return time.Since(started), err
	}
	select {
	case <-p.input.closed:
		return time.Since(started), io.ErrClosedPipe
	default:
	}
	message := "runtime-log-delivery-probe " + uuid.NewString()
	select {
	case p.input.events <- []byte(message + "\n"):
	case <-p.input.closed:
		return time.Since(started), io.ErrClosedPipe
	case <-ctx.Done():
		return time.Since(started), ctx.Err()
	}
	target.Path = "/loki/api/v1/query_range"
	target.Fragment = ""
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for {
		target.RawQuery = url.Values{"query": {`{deployment="addp",module_name="runtime-probe"} | instance_id=` + strconv.Quote(p.o.InstanceID) + ` |= ` + strconv.Quote(message)}, "start": {strconv.FormatInt(started.Add(-time.Second).UnixNano(), 10)}, "end": {strconv.FormatInt(time.Now().UnixNano(), 10)}, "limit": {"1"}}.Encode()
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
						if json.Unmarshal([]byte(v[1]), &entry) == nil && entry.InstanceID == p.o.InstanceID && entry.Module == p.o.Module && entry.Message == message {
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
