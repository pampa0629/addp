package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	commonconfig "github.com/addp/common/config"
	"github.com/addp/common/opaquetoken"
	"github.com/addp/common/runtimelog"
)

var ErrRuntimeLogsDisabled = errors.New("runtime logs disabled")
var ErrRuntimeLogsInvalid = errors.New("invalid runtime log query")
var ErrRuntimeLogsUpstream = errors.New("runtime log upstream unavailable")
var ErrRuntimeLogsBoundary = errors.New("runtime log timestamp boundary exceeds query capacity")
var ErrRuntimeLogsBusy = errors.New("runtime log query capacity exhausted")

type RuntimeLogQuery struct {
	From, To       time.Time
	Level, Keyword string
	Node           string
	Limit          int
	Cursor         string
	UserID         uint
}
type RuntimeLogResult struct {
	Entries          []runtimelog.Entry `json:"entries"`
	From             time.Time          `json:"from"`
	To               time.Time          `json:"to"`
	Returned         int                `json:"returned"`
	HasMore          bool               `json:"has_more"`
	NextCursor       string             `json:"next_cursor,omitempty"`
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
	codec           *opaquetoken.Codec
	now             func() time.Time
}

func NewRuntimeLogService(endpoint, token string, encryptionKey []byte) *RuntimeLogService {
	hours := 168
	invalid := false
	deploymentState := commonconfig.RuntimeLogDeploymentState()
	switch deploymentState {
	case "disabled":
		endpoint, token = "", ""
	case "unconfigured":
		invalid = true
	}
	if raw := os.Getenv("LOKI_RETENTION_HOURS"); deploymentState == "enabled" && raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 24 || n > 8760 {
			invalid = true
		} else {
			hours = n
		}
	}
	return &RuntimeLogService{codec: opaquetoken.New(encryptionKey, "addp/system/runtime-log-cursor/v1"), now: time.Now, configInvalid: invalid, endpoint: strings.TrimRight(endpoint, "/"), token: token, retention: time.Duration(hours) * time.Hour, slots: make(chan struct{}, 4), client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (q RuntimeLogQuery) Validate() error {
	if q.UserID == 0 || len(q.Cursor) > 4096 || !time.Unix(0, q.From.UnixNano()).Equal(q.From) || !time.Unix(0, q.To.UnixNano()).Equal(q.To) || q.From.IsZero() || q.To.IsZero() || !q.From.Before(q.To) || q.To.Sub(q.From) > 7*24*time.Hour || q.Limit < 1 || q.Limit > 1000 || utf8.RuneCountInString(q.Keyword) > 128 || strings.ContainsRune(q.Keyword, 0) {
		return ErrRuntimeLogsInvalid
	}
	switch q.Level {
	case "", "debug", "info", "warn", "error", "unknown":
	default:
		return ErrRuntimeLogsInvalid
	}
	return nil
}

const runtimeLogStorageLimit = 1001
const runtimeLogCursorTTL = 30 * time.Minute

type runtimeLogCursor struct {
	Version   int       `json:"v"`
	UserID    uint      `json:"user"`
	QueryHash string    `json:"query"`
	Timestamp time.Time `json:"timestamp"`
	Sequence  uint64    `json:"sequence"`
	ExpiresAt time.Time `json:"expires_at"`
}
type runtimeLogCandidate struct {
	entry     runtimelog.Entry
	timestamp time.Time
	sequence  uint64
}

func runtimeLogQueryHash(module, id string, q RuntimeLogQuery) string {
	body, _ := json.Marshal([]any{module, id, q.From.UTC(), q.To.UTC(), q.Level, q.Keyword, q.Limit, q.Node})
	return fmt.Sprintf("%x", sha256.Sum256(body))
}
func (s *RuntimeLogService) Query(ctx context.Context, module, id string, q RuntimeLogQuery) (*RuntimeLogResult, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	expires := now.Add(runtimeLogCursorTTL)
	hash := runtimeLogQueryHash(module, id, q)
	var cursor runtimeLogCursor
	if q.Cursor != "" {
		if s.codec.Decode("page", q.Cursor, &cursor) != nil || cursor.Version != 1 || cursor.UserID != q.UserID || cursor.QueryHash != hash || !now.Before(cursor.ExpiresAt) || cursor.ExpiresAt.After(now.Add(runtimeLogCursorTTL)) || cursor.Timestamp.Before(q.From) || !cursor.Timestamp.Before(q.To) || cursor.Sequence == 0 {
			return nil, ErrRuntimeLogsInvalid
		}
		expires = cursor.ExpiresAt
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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	candidates := []runtimeLogCandidate{}
	olderTo := q.To
	// Finish the previous timestamp before moving to older timestamps. Sequence
	// comparison remains uint64; LogQL numeric filters would lose precision.
	if q.Cursor != "" {
		bucket, err := s.completeBucket(ctx, module, id, q, cursor.Timestamp)
		if err != nil {
			return nil, err
		}
		for _, item := range bucket {
			if item.sequence < cursor.Sequence {
				candidates = append(candidates, item)
			}
		}
		olderTo = cursor.Timestamp
	}
	var older []runtimeLogCandidate
	var saturated bool
	var err error
	// Existing boundary entries count against this batch's candidate budget.
	// Only a dense timestamp completion needs the full storage safety limit.
	if wanted := q.Limit + 1 - len(candidates); wanted > 0 {
		older, saturated, err = s.fetch(ctx, module, id, q, q.From, olderTo, wanted)
		if err != nil {
			return nil, err
		}
	}
	candidates = append(candidates, older...)
	sortRuntimeLogs(candidates)
	end := runtimeLogPageEnd(candidates, q.Limit)
	// Only the oldest timestamp of a saturated storage response can be partial.
	// Complete it before choosing any ordering boundary within that timestamp.
	if end > 0 && saturated && len(older) > 0 && candidates[end-1].timestamp.Equal(older[len(older)-1].timestamp) {
		timestamp := candidates[end-1].timestamp
		bucket, err := s.completeBucket(ctx, module, id, q, timestamp)
		if err != nil {
			return nil, err
		}
		known := map[string]runtimelog.Entry{}
		for _, item := range bucket {
			known[item.entry.ID] = item.entry
		}
		remaining := make([]runtimeLogCandidate, 0, len(candidates)+len(bucket))
		for _, item := range candidates {
			if !item.timestamp.Equal(timestamp) {
				remaining = append(remaining, item)
				continue
			}
			if match, ok := known[item.entry.ID]; !ok || match != item.entry {
				return nil, ErrRuntimeLogsUpstream
			}
		}
		candidates = append(remaining, bucket...)
		sortRuntimeLogs(candidates)
		end = runtimeLogPageEnd(candidates, q.Limit)
	}
	if end == 0 && len(candidates) > 0 {
		return nil, ErrRuntimeLogsBoundary
	}
	hasMore := end < len(candidates)
	// A saturated result may contain duplicate lines. Confirm older existence
	// rather than presenting a guessed end-of-query or an empty continuation.
	if end > 0 && !hasMore && saturated {
		last := candidates[end-1]
		lookahead, _, err := s.fetch(ctx, module, id, q, q.From, last.timestamp, 1)
		if err != nil {
			return nil, err
		}
		hasMore = len(lookahead) > 0
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries := make([]runtimelog.Entry, end)
	for i := 0; i < end; i++ {
		entries[end-1-i] = candidates[i].entry
	}
	result := &RuntimeLogResult{Entries: entries, From: q.From.UTC(), To: q.To.UTC(), Returned: end, HasMore: hasMore, QueriedAt: s.now().UTC(), CollectionState: "unknown", OutsideRetention: q.From.Before(now.Add(-s.retention))}
	if hasMore {
		last := candidates[end-1]
		result.NextCursor, err = s.codec.Encode("page", runtimeLogCursor{Version: 1, UserID: q.UserID, QueryHash: hash, Timestamp: last.timestamp, Sequence: last.sequence, ExpiresAt: expires})
		if err != nil {
			return nil, ErrRuntimeLogsUpstream
		}
	}
	return result, nil
}
func runtimeLogPageEnd(entries []runtimeLogCandidate, limit int) int {
	used, end := 0, 0
	for i, item := range entries {
		if i >= limit {
			break
		}
		body, _ := json.Marshal(item.entry)
		if used+len(body)+4096 > 2<<20 {
			break
		}
		used += len(body)
		end++
	}
	return end
}
func sortRuntimeLogs(entries []runtimeLogCandidate) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].timestamp.Equal(entries[j].timestamp) {
			return entries[i].sequence > entries[j].sequence
		}
		return entries[i].timestamp.After(entries[j].timestamp)
	})
}
func (s *RuntimeLogService) completeBucket(ctx context.Context, module, id string, q RuntimeLogQuery, timestamp time.Time) ([]runtimeLogCandidate, error) {
	entries, saturated, err := s.fetch(ctx, module, id, q, timestamp, timestamp.Add(time.Nanosecond), runtimeLogStorageLimit)
	if err != nil {
		return nil, err
	}
	if saturated {
		return nil, ErrRuntimeLogsBoundary
	}
	return entries, nil
}
func (s *RuntimeLogService) fetch(ctx context.Context, module, id string, q RuntimeLogQuery, from, to time.Time, limit int) ([]runtimeLogCandidate, bool, error) {
	if !from.Before(to) {
		return nil, false, nil
	}
	target, err := url.Parse(s.endpoint)
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") || target.User != nil {
		return nil, false, ErrRuntimeLogsUpstream
	}
	target.Path = strings.TrimRight(target.Path, "/") + "/loki/api/v1/query_range"
	logql := fmt.Sprintf(`{deployment="addp",module_name=%s} | instance_id=%s`, strconv.Quote(module), strconv.Quote(id))
	if q.Node != "" {
		logql += " | node_name=" + strconv.Quote(q.Node)
	}
	if q.Level != "" {
		logql += " | level=" + strconv.Quote(q.Level)
	}
	if q.Keyword != "" {
		logql += " |= " + strconv.Quote(q.Keyword)
	}
	target.RawQuery = (url.Values{"query": {logql}, "start": {strconv.FormatInt(from.UnixNano(), 10)}, "end": {strconv.FormatInt(to.UnixNano(), 10)}, "limit": {strconv.Itoa(limit)}, "direction": {"backward"}}).Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, false, ErrRuntimeLogsUpstream
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	response, err := s.client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, false, ErrRuntimeLogsUpstream
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65<<20))
	if err != nil {
		return nil, false, err
	}
	if len(body) >= 65<<20 {
		return nil, false, ErrRuntimeLogsUpstream
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
		return nil, false, ErrRuntimeLogsUpstream
	}
	entries := []runtimeLogCandidate{}
	seen := map[string]runtimelog.Entry{}
	count := 0
	for _, stream := range payload.Data.Result {
		for _, value := range stream.Values {
			count++
			if count > limit || len(value) != 2 {
				return nil, false, ErrRuntimeLogsUpstream
			}
			var line, ts string
			if json.Unmarshal(value[0], &ts) != nil || json.Unmarshal(value[1], &line) != nil {
				return nil, false, ErrRuntimeLogsUpstream
			}
			var entry runtimelog.Entry
			if json.Unmarshal([]byte(line), &entry) != nil || entry.InstanceID != id || entry.Module != module || (q.Node != "" && entry.Node != q.Node) {
				return nil, false, ErrRuntimeLogsUpstream
			}
			parsed, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
			nanos, nsErr := strconv.ParseInt(ts, 10, 64)
			sequenceText, ok := strings.CutPrefix(entry.ID, id+":")
			sequence, seqErr := strconv.ParseUint(sequenceText, 10, 64)
			if err != nil || nsErr != nil || !time.Unix(0, nanos).Equal(parsed) || parsed.Before(from) || !parsed.Before(to) || !ok || seqErr != nil || sequence == 0 || strconv.FormatUint(sequence, 10) != sequenceText || (q.Level != "" && entry.Level != q.Level) {
				return nil, false, ErrRuntimeLogsUpstream
			}
			if previous, exists := seen[entry.ID]; exists {
				if previous != entry {
					return nil, false, ErrRuntimeLogsUpstream
				}
				continue
			}
			seen[entry.ID] = entry
			entries = append(entries, runtimeLogCandidate{entry: entry, timestamp: parsed, sequence: sequence})
		}
	}
	sortRuntimeLogs(entries)
	return entries, count == limit, nil
}
