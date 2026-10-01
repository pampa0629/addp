package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/addp/common/runtimelog"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrRuntimeLogsDisabled = errors.New("runtime logs disabled")
var ErrRuntimeLogsInvalid = errors.New("invalid runtime log query")
var ErrRuntimeLogsUpstream = errors.New("runtime log upstream unavailable")
var ErrRuntimeLogsBusy = errors.New("runtime log query capacity exhausted")

type RuntimeLogQuery struct {
	From, To       time.Time
	Level, Keyword string
	Limit          int
}
type RuntimeLogResult struct {
	Entries          []runtimelog.Entry `json:"entries"`
	From             time.Time          `json:"from"`
	To               time.Time          `json:"to"`
	Returned         int                `json:"returned"`
	Limited          bool               `json:"limited"`
	QueriedAt        time.Time          `json:"queried_at"`
	CollectionState  string             `json:"collection_state"`
	OutsideRetention bool               `json:"outside_retention"`
}
type RuntimeLogService struct {
	endpoint, token string
	retention       time.Duration
	configInvalid   bool
	client          *http.Client
	slots           chan struct{}
}

func NewRuntimeLogService(endpoint, token string) *RuntimeLogService {
	hours := 168
	invalid := false
	if raw := os.Getenv("LOKI_RETENTION_HOURS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 24 || n > 8760 {
			invalid = true
		} else {
			hours = n
		}
	}
	return &RuntimeLogService{configInvalid: invalid, endpoint: strings.TrimRight(endpoint, "/"), token: token, retention: time.Duration(hours) * time.Hour, slots: make(chan struct{}, 4), client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (q RuntimeLogQuery) Validate() error {
	if q.From.IsZero() || q.To.IsZero() || !q.From.Before(q.To) || q.To.Sub(q.From) > 7*24*time.Hour || q.Limit < 1 || q.Limit > 1000 || utf8.RuneCountInString(q.Keyword) > 128 || strings.ContainsRune(q.Keyword, 0) {
		return ErrRuntimeLogsInvalid
	}
	switch q.Level {
	case "", "debug", "info", "warn", "error", "unknown":
	default:
		return ErrRuntimeLogsInvalid
	}
	return nil
}
func (s *RuntimeLogService) Query(ctx context.Context, module, id string, q RuntimeLogQuery) (*RuntimeLogResult, error) {
	if e := q.Validate(); e != nil {
		return nil, e
	}
	if s.configInvalid {
		return nil, ErrRuntimeLogsUpstream
	}
	if s.endpoint == "" {
		return nil, ErrRuntimeLogsDisabled
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return nil, ErrRuntimeLogsBusy
	}
	target, e := url.Parse(s.endpoint)
	if e != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") || target.User != nil {
		return nil, ErrRuntimeLogsUpstream
	}
	target.Path = strings.TrimRight(target.Path, "/") + "/loki/api/v1/query_range"
	logql := fmt.Sprintf(`{deployment="addp",module_name=%s} | instance_id=%s`, strconv.Quote(module), strconv.Quote(id))
	if q.Level != "" {
		logql += " | level=" + strconv.Quote(q.Level)
	}
	if q.Keyword != "" {
		logql += " |= " + strconv.Quote(q.Keyword)
	}
	values := url.Values{"query": {logql}, "start": {strconv.FormatInt(q.From.UnixNano(), 10)}, "end": {strconv.FormatInt(q.To.Add(-time.Nanosecond).UnixNano(), 10)}, "limit": {strconv.Itoa(q.Limit + 1)}, "direction": {"backward"}}
	target.RawQuery = values.Encode()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if e != nil {
		return nil, ErrRuntimeLogsUpstream
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	response, e := s.client.Do(req)
	if e != nil {
		return nil, e
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, ErrRuntimeLogsUpstream
	}
	body, e := io.ReadAll(io.LimitReader(response.Body, 65<<20))
	if e != nil {
		return nil, e
	}
	if len(body) >= 65<<20 {
		return nil, ErrRuntimeLogsUpstream
	}
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Values [][]json.RawMessage `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Status != "success" || payload.Data.ResultType != "streams" {
		return nil, ErrRuntimeLogsUpstream
	}
	entries := make([]runtimelog.Entry, 0)
	seen := map[string]bool{}
	times := map[string]time.Time{}
	for _, stream := range payload.Data.Result {
		for _, value := range stream.Values {
			if len(value) < 2 {
				return nil, ErrRuntimeLogsUpstream
			}
			var line string
			if json.Unmarshal(value[1], &line) != nil {
				return nil, ErrRuntimeLogsUpstream
			}
			var entry runtimelog.Entry
			if json.Unmarshal([]byte(line), &entry) != nil || entry.InstanceID != id || entry.Module != module || entry.ID == "" {
				return nil, ErrRuntimeLogsUpstream
			}
			parsed, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
			if err != nil {
				return nil, ErrRuntimeLogsUpstream
			}
			times[entry.ID] = parsed
			if !seen[entry.ID] {
				seen[entry.ID] = true
				entries = append(entries, entry)
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if times[entries[i].ID].Equal(times[entries[j].ID]) {
			return entries[i].ID > entries[j].ID
		}
		return times[entries[i].ID].After(times[entries[j].ID])
	})
	limited := len(entries) > q.Limit
	if limited {
		entries = entries[:q.Limit]
	}
	used := 0
	end := 0
	for i, entry := range entries {
		b, _ := json.Marshal(entry)
		if used+len(b)+4096 > 2<<20 {
			limited = true
			break
		}
		used += len(b)
		end = i + 1
	}
	entries = entries[:end]
	// Present selected recent output in chronological reading order.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	now := time.Now().UTC()
	return &RuntimeLogResult{Entries: entries, From: q.From.UTC(), To: q.To.UTC(), Returned: len(entries), Limited: limited, QueriedAt: now, CollectionState: "unknown", OutsideRetention: q.From.Before(now.Add(-s.retention))}, nil
}
