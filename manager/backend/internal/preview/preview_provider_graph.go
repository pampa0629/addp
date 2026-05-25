package preview

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/manager/internal/models"
)

// GraphPreviewProvider 图整体预览。
type GraphPreviewProvider struct{}

func NewGraphPreviewProvider() PreviewProvider {
	return &GraphPreviewProvider{}
}

func (p *GraphPreviewProvider) Name() string { return "builtin:graph" }

func (p *GraphPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	if req == nil || req.Engine == nil {
		return nil, fmt.Errorf("invalid preview request")
	}
	info := datatype.GraphInfoFromAttributes(req.Attributes)
	if info == nil {
		return nil, fmt.Errorf("graph metadata is missing")
	}
	columns, rows := graphOverviewRows(info)

	return &models.TablePreview{
		Mode:            PreviewModeTable,
		Columns:         columns,
		Rows:            rows,
		Total:           len(rows),
		Page:            maxInt(req.Page, 1),
		PageSize:        normalizePageSize(req.PageSize),
		GeometryColumns: []string{},
		EngineID:        req.Engine.ID,
		Schema:          req.Schema,
		Table:           req.Table,
		EngineType:      req.Engine.EngineType,
	}, nil
}

func graphOverviewRows(info *datatype.GraphInfo) ([]string, []map[string]interface{}) {
	columns := []string{"kind", "name", "count", "patterns", "properties"}
	rows := make([]map[string]interface{}, 0, len(info.NodeShapes)+len(info.RelationshipShapes))
	for _, shape := range info.NodeShapes {
		rows = append(rows, map[string]interface{}{
			"kind":       "node_shape",
			"name":       graphNodeShapeName(shape),
			"count":      graphCountValue(shape.Count),
			"patterns":   "",
			"properties": strings.Join(graphFieldNames(shape.Properties), ", "),
		})
	}
	for _, shape := range info.RelationshipShapes {
		rows = append(rows, map[string]interface{}{
			"kind":       "relationship_shape",
			"name":       shape.Type,
			"count":      graphCountValue(shape.Count),
			"patterns":   strings.Join(graphPatternLabels(shape.Patterns), "; "),
			"properties": strings.Join(graphFieldNames(shape.Properties), ", "),
		})
	}
	return columns, rows
}

func graphNodeShapeName(shape datatype.GraphNodeShapeInfo) string {
	if shape.Name != "" {
		return shape.Name
	}
	return strings.Join(shape.Labels, "+")
}

func graphCountValue(count *int64) interface{} {
	if count == nil {
		return nil
	}
	return *count
}

func graphFieldNames(fields []datatype.FieldInfo) []string {
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.Name != "" {
			result = append(result, field.Name)
		}
	}
	return result
}

func graphPatternLabels(patterns []datatype.GraphRelationshipPatternInfo) []string {
	result := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		result = append(result, fmt.Sprintf("(%s)->(%s)", graphEndpointLabel(pattern.From), graphEndpointLabel(pattern.To)))
	}
	return result
}

func graphEndpointLabel(endpoint datatype.GraphEndpointInfo) string {
	if endpoint.ShapeName != "" {
		return endpoint.ShapeName
	}
	return strings.Join(endpoint.Labels, "+")
}

func flattenGraphEntityRows(source []map[string]interface{}, key string) ([]string, []map[string]interface{}) {
	if len(source) == 0 {
		return []string{}, []map[string]interface{}{}
	}

	columnSet := map[string]struct{}{}
	rows := make([]map[string]interface{}, 0, len(source))

	for _, raw := range source {
		row := map[string]interface{}{}
		entity, _ := raw[key].(map[string]interface{})
		for _, field := range graphEntityDisplayFields(entity) {
			if v, ok := entity[field]; ok {
				row[field] = v
				columnSet[field] = struct{}{}
			}
		}
		props, _ := entity["properties"].(map[string]interface{})
		for k, v := range props {
			row[k] = v
			columnSet[k] = struct{}{}
		}
		rows = append(rows, row)
	}

	columns := make([]string, 0, len(columnSet))
	for k := range columnSet {
		columns = append(columns, k)
	}
	// 维持稳定输出，便于前端渲染
	sortStrings(columns)

	for i := range rows {
		for _, col := range columns {
			if _, ok := rows[i][col]; !ok {
				rows[i][col] = nil
			}
		}
	}

	return columns, rows
}

func graphEntityDisplayFields(entity map[string]interface{}) []string {
	if entity == nil {
		return []string{}
	}
	if _, ok := entity["type"]; ok {
		return []string{"id", "type"}
	}
	return []string{"id", "labels"}
}

func escapeCypherIdentifier(v string) string {
	return strings.ReplaceAll(v, "`", "``")
}

func normalizePageSize(pageSize int) int {
	if pageSize <= 0 {
		return 50
	}
	if pageSize > 50 {
		return 50
	}
	return pageSize
}

func maxInt(v, min int) int {
	if v < min {
		return min
	}
	return v
}

func sortStrings(values []string) {
	sort.Strings(values)
}
