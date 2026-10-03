package elasticsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
)

var fieldName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

func (*ElasticsearchPlugin) QueryLanguages() []string { return []string{"es_dsl"} }
func (*ElasticsearchPlugin) GenerateSampleQuery(context.Context, plugin.ConnectionInfo, plugin.SampleQueryOptions) (string, string) {
	return `{"query":{"match_all":{}},"size":10}`, "es_dsl"
}
func decodeDSL(raw string) (map[string]interface{}, error) {
	if len(raw) > 1<<20 {
		return nil, fmt.Errorf("ES DSL exceeds size limit")
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var body map[string]interface{}
	if err := dec.Decode(&body); err != nil {
		return nil, fmt.Errorf("invalid ES DSL JSON: %w", err)
	}
	var extra interface{}
	if dec.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("ES DSL requires one JSON object")
	}
	if body == nil {
		return nil, fmt.Errorf("ES DSL object required")
	}
	for key, value := range body {
		switch key {
		case "query":
			if err := validateClause(value, 0); err != nil {
				return nil, err
			}
		case "size":
			if _, err := positiveSize(value); err != nil {
				return nil, err
			}
		case "sort":
			if err := validateSort(value); err != nil {
				return nil, err
			}
		case "_source":
			if err := validateSource(value); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported ES DSL option: %s", key)
		}
	}
	if _, ok := body["query"]; !ok {
		body["query"] = map[string]interface{}{"match_all": map[string]interface{}{}}
	}
	return body, nil
}
func positiveSize(v interface{}) (int, error) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, fmt.Errorf("size must be an integer")
	}
	i, err := n.Int64()
	if err != nil || i < 1 || i > 10000 {
		return 0, fmt.Errorf("size must be between 1 and 10000")
	}
	return int(i), nil
}
func scalar(v interface{}) bool {
	switch v.(type) {
	case string, json.Number, bool:
		return true
	default:
		return false
	}
}
func validateClause(v interface{}, depth int) error {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) != 1 || depth > 32 {
		return fmt.Errorf("unsupported ES DSL clause")
	}
	for kind, value := range m {
		args, ok := value.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s requires an object", kind)
		}
		switch kind {
		case "match_all":
			if len(args) != 0 {
				return fmt.Errorf("match_all options are unsupported")
			}
		case "term", "terms", "range", "match":
			if len(args) != 1 {
				return fmt.Errorf("%s requires one field", kind)
			}
			for field, arg := range args {
				if !fieldName.MatchString(field) {
					return fmt.Errorf("literal field name required")
				}
				switch kind {
				case "term":
					if !scalar(arg) {
						return fmt.Errorf("term requires a literal value")
					}
				case "terms":
					values, ok := arg.([]interface{})
					if !ok || len(values) == 0 || len(values) > 1024 {
						return fmt.Errorf("terms requires a literal array; terms lookup is unsupported")
					}
					for _, x := range values {
						if !scalar(x) {
							return fmt.Errorf("terms requires literal values")
						}
					}
				case "range":
					bounds, ok := arg.(map[string]interface{})
					if !ok || len(bounds) == 0 {
						return fmt.Errorf("range bounds required")
					}
					for k, x := range bounds {
						if k != "gt" && k != "gte" && k != "lt" && k != "lte" {
							return fmt.Errorf("unsupported range option")
						}
						if !scalar(x) {
							return fmt.Errorf("range requires literal bounds")
						}
					}
				case "match":
					if _, ok := arg.(string); !ok {
						return fmt.Errorf("match requires text")
					}
				}
			}
		case "exists":
			if len(args) != 1 {
				return fmt.Errorf("exists requires field")
			}
			field, ok := args["field"].(string)
			if !ok || !fieldName.MatchString(field) {
				return fmt.Errorf("exists requires literal field")
			}
		case "bool":
			if len(args) == 0 {
				return fmt.Errorf("bool clauses required")
			}
			for k, x := range args {
				switch k {
				case "must", "filter", "should", "must_not":
					if values, ok := x.([]interface{}); ok {
						if len(values) == 0 || len(values) > 1024 {
							return fmt.Errorf("invalid bool array")
						}
						for _, item := range values {
							if err := validateClause(item, depth+1); err != nil {
								return err
							}
						}
					} else if err := validateClause(x, depth+1); err != nil {
						return err
					}
				case "minimum_should_match":
					n, ok := x.(json.Number)
					if !ok {
						return fmt.Errorf("minimum_should_match requires nonnegative integer")
					}
					i, err := n.Int64()
					if err != nil || i < 0 {
						return fmt.Errorf("invalid minimum_should_match")
					}
				default:
					return fmt.Errorf("unsupported bool option")
				}
			}
		default:
			return fmt.Errorf("unsupported ES DSL clause: %s", kind)
		}
	}
	return nil
}
func validateSort(v interface{}) error {
	values, ok := v.([]interface{})
	if !ok || len(values) == 0 || len(values) > 16 {
		return fmt.Errorf("sort requires a field array")
	}
	for _, x := range values {
		if field, ok := x.(string); ok {
			if !fieldName.MatchString(field) || field == "_shard_doc" {
				return fmt.Errorf("invalid sort field")
			}
			continue
		}
		m, ok := x.(map[string]interface{})
		if !ok || len(m) != 1 {
			return fmt.Errorf("sort requires field and order")
		}
		for field, order := range m {
			if !fieldName.MatchString(field) || field == "_shard_doc" || (order != "asc" && order != "desc") {
				return fmt.Errorf("sort supports literal fields and asc/desc")
			}
		}
	}
	return nil
}
func validateSource(v interface{}) error {
	if v == true {
		return nil
	}
	values, ok := v.([]interface{})
	if !ok || len(values) == 0 {
		return fmt.Errorf("_source requires true or an exact field array")
	}
	for _, x := range values {
		field, ok := x.(string)
		if !ok || !fieldName.MatchString(field) {
			return fmt.Errorf("_source requires literal fields")
		}
	}
	return nil
}
func (p *ElasticsearchPlugin) PrepareQuery(ctx context.Context, c plugin.ConnectionInfo, req plugin.QueryRequest) (plugin.PreparedQuery, error) {
	if req.Language != "es_dsl" || req.TargetPath == nil {
		return nil, fmt.Errorf("ES DSL requires an explicit index path")
	}
	if len(req.Options.Parameters) > 0 || len(req.Options.Args) > 0 || req.Options.Offset != 0 || req.Options.Describe || req.Options.Spatial {
		return nil, fmt.Errorf("unsupported ES DSL query options")
	}
	path := *req.TargetPath
	path.Segments = append([]plugin.EngineCatalogSegment(nil), path.Segments...)
	name, err := indexName(path)
	if err != nil {
		return nil, err
	}
	engineID := req.EngineID
	if engineID == 0 {
		engineID = req.Options.EngineID
	}
	if engineID == 0 {
		engineID = path.EngineID
	}
	if engineID == 0 || (path.EngineID != 0 && path.EngineID != engineID) || (req.Options.EngineID != 0 && req.Options.EngineID != engineID) {
		return nil, fmt.Errorf("query engine identity is inconsistent")
	}
	path = p.indexEntry(engineID, name).Path
	body, err := decodeDSL(req.Query)
	if err != nil {
		return nil, err
	}
	limit := 1000
	if req.Options.Limit > 0 {
		limit = min(req.Options.Limit, 10000)
	}
	if v, ok := body["size"]; ok {
		n, _ := positiveSize(v)
		limit = min(limit, n)
	}
	delete(body, "size")
	conn := make(plugin.ConnectionInfo, len(c))
	for k, v := range c {
		conn[k] = v
	}
	analysis, err := plugin.NewQueryAnalysis("es_dsl", plugin.QuerySchemaCoverageUnknown)
	if err != nil {
		return nil, err
	}
	timeout := req.Options.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return plugin.NewPreparedQuery(analysis,
		func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) },
		func(ctx context.Context, _ *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
			facts, err := p.DescribeEngineCatalogFacts(ctx, conn, path, plugin.EngineCatalogFactsOptions{})
			if err != nil {
				return nil, err
			}
			return &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{{Path: path, Fields: facts.Table.Fields, IdentityOutput: true}}}, nil
		},
		func(ctx context.Context) (result *plugin.QueryResult, err error) {
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			session, err := p.openSession(ctx, conn, path, body)
			if err != nil {
				return nil, err
			}
			defer func() {
				if closeErr := session.Close(context.Background()); err == nil && closeErr != nil {
					result, err = nil, closeErr
				}
			}()
			rows := make([]map[string]interface{}, 0)
			for len(rows) < limit {
				batch, err := session.ReadBatch(ctx, min(1000, limit-len(rows)))
				if err != nil {
					return nil, err
				}
				if len(batch.Records) == 0 {
					break
				}
				rows = append(rows, batch.Records...)
			}
			columns := map[string]bool{}
			for _, row := range rows {
				for key := range row {
					columns[key] = true
				}
			}
			names := make([]string, 0, len(columns))
			for name := range columns {
				names = append(names, name)
			}
			sort.Strings(names)
			return &plugin.QueryResult{Columns: names, Rows: rows}, nil
		})
}
