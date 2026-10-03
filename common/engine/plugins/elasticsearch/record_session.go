package elasticsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"sync"
	"time"

	"github.com/addp/common/engine/plugin"
)

type recordSession struct {
	mu           sync.Mutex
	client       *esClient
	pit          string
	index        string
	body         map[string]interface{}
	after        []interface{}
	offset       int64
	closed, done bool
}

func (p *ElasticsearchPlugin) OpenRecordReadSession(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.RecordReadSessionOptions) (plugin.RecordReadSession, error) {
	if len(opts.Hints) > 0 {
		return nil, fmt.Errorf("Elasticsearch record hints are unsupported")
	}
	return p.openSession(ctx, c, path, map[string]interface{}{"query": map[string]interface{}{"match_all": map[string]interface{}{}}})
}
func (p *ElasticsearchPlugin) openSession(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath, body map[string]interface{}) (*recordSession, error) {
	name, err := indexName(path)
	if err != nil {
		return nil, err
	}
	client, err := p.client(c)
	if err != nil {
		return nil, err
	}
	if err = requireIndex(ctx, client, name); err != nil {
		closeClient(client)
		return nil, err
	}
	uuid, err := indexUUID(ctx, client, name)
	if err != nil {
		closeClient(client)
		return nil, err
	}
	var result struct {
		ID     string `json:"id"`
		Shards struct {
			Failed int `json:"failed"`
		} `json:"_shards"`
	}
	err = request(ctx, client, "POST", "/"+url.PathEscape(name)+"/_pit?keep_alive=1m&allow_partial_search_results=false", nil, &result)
	session := &recordSession{client: client, pit: result.ID, body: body, index: name}
	if err != nil || result.ID == "" || result.Shards.Failed != 0 {
		session.Close(context.Background())
		if err == nil {
			err = fmt.Errorf("incomplete Elasticsearch PIT")
		}
		return nil, err
	}
	currentUUID, identityErr := indexUUID(ctx, client, name)
	if identityErr == nil {
		identityErr = requireIndex(ctx, client, name)
	}
	if identityErr != nil || currentUUID != uuid {
		session.Close(context.Background())
		return nil, fmt.Errorf("Elasticsearch index changed while opening PIT")
	}
	return session, nil
}
func (s *recordSession) ReadBatch(ctx context.Context, limit int) (*plugin.RecordBatchData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("Elasticsearch record session closed")
	}
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("record batch limit must be between 1 and 1000")
	}
	batch := &plugin.RecordBatchData{Offset: s.offset, Records: []map[string]interface{}{}}
	if s.done {
		return batch, nil
	}
	body := make(map[string]interface{}, len(s.body)+5)
	for k, v := range s.body {
		body[k] = v
	}
	body["pit"] = map[string]interface{}{"id": s.pit, "keep_alive": "1m"}
	body["size"] = limit
	body["track_total_hits"] = false
	sorts, _ := body["sort"].([]interface{})
	sorts = append(append([]interface{}(nil), sorts...), map[string]interface{}{"_shard_doc": "asc"})
	body["sort"] = sorts
	if len(s.after) > 0 {
		body["search_after"] = s.after
	}
	var result struct {
		PIT      string `json:"pit_id"`
		TimedOut bool   `json:"timed_out"`
		Shards   struct {
			Failed int `json:"failed"`
		} `json:"_shards"`
		Hits struct {
			Hits []struct {
				Index  string                 `json:"_index"`
				Source map[string]interface{} `json:"_source"`
				Sort   []interface{}          `json:"sort"`
			} `json:"hits"`
		} `json:"hits"`
	}
	err := request(ctx, s.client, "POST", "/_search?allow_partial_search_results=false", body, &result)
	if result.PIT != "" {
		s.pit = result.PIT
	}
	if err != nil || result.TimedOut || result.Shards.Failed != 0 {
		s.closeLocked(context.Background())
		if err == nil {
			err = fmt.Errorf("partial or timed out Elasticsearch search")
		}
		return nil, err
	}
	for _, hit := range result.Hits.Hits {
		if hit.Index != s.index || hit.Source == nil || len(hit.Sort) == 0 {
			s.closeLocked(context.Background())
			return nil, fmt.Errorf("Elasticsearch _source and PIT sort values are required")
		}
		batch.Records = append(batch.Records, recordValue(hit.Source).(map[string]interface{}))
		s.after = hit.Sort
	}
	s.offset += int64(len(batch.Records))
	s.done = len(batch.Records) < limit
	return batch, nil
}
func (s *recordSession) closeLocked(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	defer closeClient(s.client)
	if s.pit == "" {
		return nil
	}
	if ctx.Err() != nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return request(ctx, s.client, "DELETE", "/_pit", map[string]interface{}{"id": s.pit}, nil)
}
func (s *recordSession) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeLocked(ctx)
}

// Unsafe integers cross JSON storage and browser boundaries as exact decimal strings.
// Mapping retains the native numeric type; no float64 conversion is permitted.
func recordValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case json.Number:
		if integer, ok := new(big.Int).SetString(typed.String(), 10); ok && new(big.Int).Abs(integer).Cmp(big.NewInt(9007199254740991)) > 0 {
			return typed.String()
		}
		return typed
	case map[string]interface{}:
		for key, item := range typed {
			typed[key] = recordValue(item)
		}
		return typed
	case []interface{}:
		for i, item := range typed {
			typed[i] = recordValue(item)
		}
		return typed
	default:
		return value
	}
}
