package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func closeClient(c *esClient) { c.transport.CloseIdleConnections() }
func request(ctx context.Context, c *esClient, method, path string, body interface{}, out interface{}) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.Perform(req)
	if err != nil {
		return fmt.Errorf("Elasticsearch request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		kind := plugin.EngineCatalogErrorUnavailable
		if res.StatusCode == 404 {
			kind = plugin.EngineCatalogErrorNotFound
		}
		return plugin.WrapEngineCatalogError(kind, fmt.Errorf("Elasticsearch request rejected (HTTP %d)", res.StatusCode))
	}
	if out == nil {
		_, err = io.Copy(io.Discard, res.Body)
		return err
	}
	dec := json.NewDecoder(io.LimitReader(res.Body, 32<<20))
	dec.UseNumber()
	return dec.Decode(out)
}

type resolvedIndex struct {
	Name       string   `json:"name"`
	Attributes []string `json:"attributes"`
	DataStream string   `json:"data_stream"`
}

func resolveIndices(ctx context.Context, c *esClient, name string) ([]resolvedIndex, error) {
	var result struct {
		Indices []resolvedIndex `json:"indices"`
	}
	err := request(ctx, c, "GET", "/_resolve/index/"+url.PathEscape(name)+"?expand_wildcards=open", nil, &result)
	if err != nil {
		return nil, err
	}
	indices := make([]resolvedIndex, 0, len(result.Indices))
	for _, idx := range result.Indices {
		open, hidden, closed := false, false, false
		for _, a := range idx.Attributes {
			open = open || a == "open"
			hidden = hidden || a == "hidden" || a == "system"
			closed = closed || a == "closed"
		}
		if open && !hidden && !closed && idx.DataStream == "" && !strings.HasPrefix(idx.Name, ".") {
			indices = append(indices, idx)
		}
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i].Name < indices[j].Name })
	return indices, nil
}
func indexName(path plugin.EngineCatalogPath) (string, error) {
	business := plugin.EngineCatalogPathWithoutRoot(path)
	if len(path.Segments) != 2 || !plugin.IsEngineCatalogRootPath(plugin.EngineCatalogPath{Segments: path.Segments[:1]}) || path.Segments[0].Term != plugin.EngineCatalogTermService || path.Version != plugin.EngineCatalogPathVersion || len(business.Segments) != 1 || business.Segments[0].Term != "index" || (business.Segments[0].Kind != "" && business.Segments[0].Kind != "index") {
		return "", plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("exact index path required"))
	}
	name := business.Segments[0].Name
	if name == "" || strings.HasPrefix(name, ".") || strings.ContainsAny(name, "*?,:/\\#% ") || name != strings.ToLower(name) {
		return "", plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("ordinary exact index name required"))
	}
	return name, nil
}
func (p *ElasticsearchPlugin) indexEntry(engineID uint, name string) plugin.EngineCatalogEntry {
	path := plugin.EngineCatalogRootPath(p.EngineCatalogModel(), engineID)
	path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: "index", Kind: "index", Name: name})
	return plugin.EngineCatalogEntry{Name: name, Path: path, Term: "index", Kind: "index", Role: plugin.EngineCatalogRoleLeaf}
}
func requireIndex(ctx context.Context, c *esClient, name string) error {
	indices, err := resolveIndices(ctx, c, name)
	if err != nil {
		return err
	}
	for _, idx := range indices {
		if idx.Name == name {
			return nil
		}
	}
	return plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorNotFound, fmt.Errorf("ordinary index not found"))
}
func (p *ElasticsearchPlugin) ListChildren(ctx context.Context, c plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	if !plugin.IsEngineCatalogRootPath(parent) || parent.Segments[0].Term != plugin.EngineCatalogTermService || parent.Version != plugin.EngineCatalogPathVersion {
		if _, err := indexName(parent); err != nil {
			return nil, err
		}
		return []plugin.EngineCatalogEntry{}, nil
	}
	client, err := p.client(c)
	if err != nil {
		return nil, err
	}
	defer closeClient(client)
	indices, err := resolveIndices(ctx, client, "*")
	if err != nil {
		return nil, err
	}
	entries := make([]plugin.EngineCatalogEntry, 0, len(indices))
	for _, idx := range indices {
		entries = append(entries, p.indexEntry(parent.EngineID, idx.Name))
	}
	start := max(0, min(opts.Offset, len(entries)))
	end := len(entries)
	if opts.Limit > 0 {
		end = min(end, start+opts.Limit)
	}
	return entries[start:end], nil
}
func (p *ElasticsearchPlugin) ResolvePath(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	if plugin.IsEngineCatalogRootPath(path) && path.Segments[0].Term == plugin.EngineCatalogTermService && path.Version == plugin.EngineCatalogPathVersion {
		entry := plugin.EngineCatalogRootEntry(p.EngineCatalogModel(), path.EngineID, "")
		return &entry, nil
	}
	name, err := indexName(path)
	if err != nil {
		return nil, err
	}
	client, err := p.client(c)
	if err != nil {
		return nil, err
	}
	defer closeClient(client)
	if err = requireIndex(ctx, client, name); err != nil {
		return nil, err
	}
	entry := p.indexEntry(path.EngineID, name)
	return &entry, nil
}
func (p *ElasticsearchPlugin) DescribeEngineCatalogFacts(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	name, err := indexName(path)
	if err != nil {
		return nil, err
	}
	client, err := p.client(c)
	if err != nil {
		return nil, err
	}
	defer closeClient(client)
	if err = requireIndex(ctx, client, name); err != nil {
		return nil, err
	}
	var response map[string]struct {
		Mappings map[string]interface{} `json:"mappings"`
	}
	if err = request(ctx, client, "GET", "/"+url.PathEscape(name)+"/_mapping", nil, &response); err != nil {
		return nil, err
	}
	mapping, ok := response[name]
	if !ok || len(response) != 1 {
		return nil, fmt.Errorf("unexpected mapping read set")
	}
	table := &datatype.TableInfo{Name: name, Kind: "index", Native: map[string]interface{}{"schema_type": "mapping", "is_sampled": false, "mapping": mapping.Mappings}}
	properties, _ := mapping.Mappings["properties"].(map[string]interface{})
	mappingFields(properties, nil, false, &table.Fields)
	if opts.IncludeStatistics {
		var count struct {
			Count  int64 `json:"count"`
			Shards struct {
				Failed int `json:"failed"`
			} `json:"_shards"`
		}
		if err = request(ctx, client, "GET", "/"+url.PathEscape(name)+"/_count", nil, &count); err != nil {
			return nil, err
		}
		if count.Shards.Failed != 0 {
			return nil, fmt.Errorf("partial Elasticsearch count")
		}
		table.RowCount = &count.Count
	}
	return &plugin.EngineCatalogFacts{Path: p.indexEntry(path.EngineID, name).Path, Kind: "index", Table: table}, nil
}
func mappingFields(properties map[string]interface{}, parent []string, multi bool, fields *[]datatype.FieldInfo) {
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		spec, ok := properties[name].(map[string]interface{})
		if !ok {
			continue
		}
		native, _ := spec["type"].(string)
		children, _ := spec["properties"].(map[string]interface{})
		if native == "" && children != nil {
			native = "object"
		}
		path := append(append([]string(nil), parent...), name)
		field := datatype.FieldInfo{Name: strings.Join(path, "."), Path: path, Type: mappingType(native), NativeType: native, Nullable: true, OrdinalPosition: len(*fields) + 1}
		if multi {
			field.Generated = true
		}
		*fields = append(*fields, field)
		mappingFields(children, path, false, fields)
		sub, _ := spec["fields"].(map[string]interface{})
		mappingFields(sub, path, true, fields)
	}
}
func mappingType(native string) datatype.FieldType {
	switch native {
	case "text", "keyword", "wildcard", "constant_keyword", "ip":
		return datatype.FieldTypeString
	case "boolean":
		return datatype.FieldTypeBool
	case "byte", "short", "integer":
		return datatype.FieldTypeInt
	case "long", "unsigned_long":
		return datatype.FieldTypeBigInt
	case "float", "half_float":
		return datatype.FieldTypeFloat
	case "double", "scaled_float":
		return datatype.FieldTypeDouble
	case "date", "date_nanos":
		return datatype.FieldTypeTimestamp
	case "binary":
		return datatype.FieldTypeBytes
	case "object", "nested", "flattened":
		return datatype.FieldTypeJSON
	default:
		return datatype.FieldTypeUnknown
	}
}

// indexUUID is a native read-consistency fact, not ADDP resource identity.
func indexUUID(ctx context.Context, client *esClient, name string) (string, error) {
	var response map[string]struct {
		Settings map[string]string `json:"settings"`
	}
	if err := request(ctx, client, "GET", "/"+url.PathEscape(name)+"/_settings/index.uuid?flat_settings=true", nil, &response); err != nil {
		return "", err
	}
	item, exists := response[name]
	if !exists || len(response) != 1 || item.Settings["index.uuid"] == "" {
		return "", fmt.Errorf("exact native index identity unavailable")
	}
	return item.Settings["index.uuid"], nil
}
