package falkordb

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func identifierQuote(name string) string { return "`" + strings.ReplaceAll(name, "`", "``") + "`" }
func labelsClause(labels []string) string {
	var s string
	for _, label := range labels {
		s += ":" + identifierQuote(label)
	}
	return s
}
func stringLabels(v interface{}) []string {
	values, _ := v.([]interface{})
	out := make([]string, 0, len(values))
	for _, v := range values {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
func endpoint(labels []string) datatype.GraphEndpointInfo {
	return datatype.GraphEndpointInfo{ShapeName: strings.Join(labels, "+"), Labels: labels}
}

func (p *FalkorDBPlugin) SampleGraph(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.GraphSampleOptions) (*plugin.GraphData, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		return nil, fmt.Errorf("FalkorDB sample limit exceeds 1000")
	}
	filter := opts.Filter.Clone()
	query := "MATCH (n)-[r]->(m) RETURN n,r,m"
	switch filter.Kind {
	case "":
	case plugin.GraphSampleKindNodeShape:
		query = "MATCH (n" + labelsClause(filter.Labels) + ") RETURN n"
	case plugin.GraphSampleKindRelationshipShape:
		if filter.RelationshipType == "" {
			return nil, fmt.Errorf("relationship shape requires a type")
		}
		query = "MATCH (n" + labelsClause(filter.FromLabels) + ")-[r:" + identifierQuote(filter.RelationshipType) + "]->(m" + labelsClause(filter.ToLabels) + ") RETURN n,r,m"
	default:
		return nil, fmt.Errorf("unknown graph sample filter")
	}
	query += fmt.Sprintf(" LIMIT %d", limit)
	result, err := p.ExecuteGraphQueryAtPath(ctx, c, path, query, plugin.QueryOptions{ReadOnly: true, Limit: limit})
	if err != nil {
		return nil, err
	}
	if result.GraphData == nil && filter.Kind == "" {
		result, err = p.ExecuteGraphQueryAtPath(ctx, c, path, fmt.Sprintf("MATCH (n) RETURN n LIMIT %d", limit), plugin.QueryOptions{ReadOnly: true, Limit: limit})
		if err != nil {
			return nil, err
		}
	}
	if result.GraphData == nil {
		return &plugin.GraphData{Nodes: []plugin.GraphNode{}, Relationships: []plugin.GraphRelationship{}}, nil
	}
	return result.GraphData, nil
}

func (p *FalkorDBPlugin) DescribeEngineCatalogFacts(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	if _, err := p.ResolvePath(ctx, c, path); err != nil {
		return nil, err
	}
	if _, err := p.graphName(path); err != nil {
		return nil, err
	}
	run := func(query string) (*plugin.GraphQueryResult, error) {
		return p.ExecuteGraphQueryAtPath(ctx, c, path, query, plugin.QueryOptions{ReadOnly: true, Limit: 501})
	}
	count := func(query string) (*int64, error) {
		r, err := run(query)
		if err != nil {
			return nil, err
		}
		if len(r.Rows) != 1 {
			return nil, fmt.Errorf("invalid graph count")
		}
		n, ok := r.Rows[0]["count"].(int64)
		if !ok {
			return nil, fmt.Errorf("invalid graph count value")
		}
		return &n, nil
	}
	nodes, err := count("MATCH (n) RETURN count(n) AS count")
	if err != nil {
		return nil, err
	}
	rels, err := count("MATCH ()-[r]->() RETURN count(r) AS count")
	if err != nil {
		return nil, err
	}
	directed := true
	info := &datatype.GraphInfo{Model: datatype.GraphModelPropertyGraph, Directed: &directed, NodeCount: nodes, RelationshipCount: rels}
	shapes, err := run("MATCH (n) RETURN labels(n) AS labels,count(n) AS count LIMIT 501")
	if err != nil {
		return nil, err
	}
	if len(shapes.Rows) > 500 {
		return nil, fmt.Errorf("graph node shapes exceed 500")
	}
	for _, row := range shapes.Rows {
		labels := stringLabels(row["labels"])
		n, ok := row["count"].(int64)
		if !ok {
			return nil, fmt.Errorf("invalid node shape count")
		}
		kind := datatype.GraphNodeShapeKindLabel
		if len(labels) != 1 {
			kind = datatype.GraphNodeShapeKindLabelSet
		}
		properties, err := p.shapeProperties(ctx, c, path, "MATCH (n"+labelsClause(labels)+") WHERE size(labels(n)) = "+fmt.Sprint(len(labels))+" WITH n LIMIT 100 UNWIND keys(n) AS property RETURN DISTINCT property,typeOf(n[property]) AS native_type LIMIT 501")
		if err != nil {
			return nil, err
		}
		info.NodeShapes = append(info.NodeShapes, datatype.GraphNodeShapeInfo{Name: strings.Join(labels, "+"), Kind: kind, Labels: labels, Count: &n, Properties: properties})
	}
	relationships, err := run("MATCH (a)-[r]->(b) RETURN type(r) AS type,labels(a) AS source,labels(b) AS target,count(r) AS count LIMIT 501")
	if err != nil {
		return nil, err
	}
	if len(relationships.Rows) > 500 {
		return nil, fmt.Errorf("graph endpoint patterns exceed 500")
	}
	indexes := map[string]int{}
	for _, row := range relationships.Rows {
		typ, ok := row["type"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid relationship type")
		}
		index, exists := indexes[typ]
		if !exists {
			properties, err := p.shapeProperties(ctx, c, path, "MATCH ()-[r:"+identifierQuote(typ)+"]->() WITH r LIMIT 100 UNWIND keys(r) AS property RETURN DISTINCT property,typeOf(r[property]) AS native_type LIMIT 501")
			if err != nil {
				return nil, err
			}
			index = len(info.RelationshipShapes)
			indexes[typ] = index
			zero := int64(0)
			info.RelationshipShapes = append(info.RelationshipShapes, datatype.GraphRelationshipShapeInfo{Type: typ, Count: &zero, Properties: properties})
		}
		n, ok := row["count"].(int64)
		if !ok {
			return nil, fmt.Errorf("invalid endpoint count")
		}
		shape := &info.RelationshipShapes[index]
		*shape.Count += n
		shape.Patterns = append(shape.Patterns, datatype.GraphRelationshipPatternInfo{From: endpoint(stringLabels(row["source"])), To: endpoint(stringLabels(row["target"])), Count: &n})
	}
	sort.Slice(info.NodeShapes, func(i, j int) bool { return info.NodeShapes[i].Name < info.NodeShapes[j].Name })
	sort.Slice(info.RelationshipShapes, func(i, j int) bool { return info.RelationshipShapes[i].Type < info.RelationshipShapes[j].Type })
	return &plugin.EngineCatalogFacts{Path: path, Kind: plugin.EngineCatalogKindGraph, Graph: info}, nil
}
func (p *FalkorDBPlugin) shapeProperties(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath, query string) ([]datatype.FieldInfo, error) {
	// Property facts are a bounded observation, never a claim of complete schema.
	r, err := p.ExecuteGraphQueryAtPath(ctx, c, path, query, plugin.QueryOptions{ReadOnly: true, Limit: 501})
	if err != nil {
		return nil, err
	}
	if len(r.Rows) > 500 {
		return nil, fmt.Errorf("graph properties exceed budget")
	}
	types := map[string]string{}
	for _, row := range r.Rows {
		name, ok := row["property"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid graph property")
		}
		native, ok := row["native_type"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid graph property type")
		}
		if old, exists := types[name]; exists && old != native {
			types[name] = "mixed"
		} else {
			types[name] = native
		}
	}
	names := make([]string, 0, len(types))
	for name := range types {
		names = append(names, name)
	}
	sort.Strings(names)
	fields := make([]datatype.FieldInfo, 0, len(names))
	for i, name := range names {
		native := types[name]
		typ := datatype.FieldTypeUnknown
		switch strings.ToLower(native) {
		case "string":
			typ = datatype.FieldTypeString
		case "integer":
			typ = datatype.FieldTypeBigInt
		case "float", "double":
			typ = datatype.FieldTypeDouble
		case "boolean":
			typ = datatype.FieldTypeBool
		case "list":
			typ = datatype.FieldTypeArray
		case "map":
			typ = datatype.FieldTypeJSON
		case "point":
			typ = datatype.FieldTypeGeometry
		}
		fields = append(fields, datatype.FieldInfo{Name: name, Type: typ, NativeType: native, Nullable: true, OrdinalPosition: i + 1})
	}
	return fields, nil
}
