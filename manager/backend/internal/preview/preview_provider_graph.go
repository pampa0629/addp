package preview

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
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
		var err error
		info, err = p.describeGraph(ctx, req)
		if err != nil {
			return nil, err
		}
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

func (p *GraphPreviewProvider) describeGraph(ctx context.Context, req *PreviewRequest) (*datatype.GraphInfo, error) {
	plug, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}
	graphProvider, ok := plug.(plugin.GraphMetadataProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement GraphMetadataProvider", req.Engine.EngineType)
	}
	path, err := graphPreviewCatalogPath(req)
	if err != nil {
		return nil, err
	}
	info, err := graphProvider.DescribeGraph(ctx, plugin.ConnectionInfo(req.Engine.ConnectionInfo), path, plugin.MetadataOptions{IncludeStatistics: true})
	if err != nil {
		return nil, fmt.Errorf("failed to describe graph: %w", err)
	}
	if info == nil {
		return nil, fmt.Errorf("graph metadata is missing")
	}
	return info, nil
}

func graphPreviewCatalogPath(req *PreviewRequest) (plugin.CatalogPath, error) {
	database := strings.TrimSpace(req.Schema)
	if database == "" {
		database = strings.TrimSpace(plugin.GetString(plugin.ConnectionInfo(req.Engine.ConnectionInfo), "database"))
	}
	if database == "" {
		return plugin.CatalogPath{}, fmt.Errorf("graph preview requires database")
	}
	graphName := strings.TrimSpace(req.Table)
	if graphName == "" {
		graphName = plugin.CatalogKindGraph
	}
	return plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: req.Engine.ID,
		Segments: []plugin.CatalogSegment{
			{Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, Name: database},
			{Term: plugin.CatalogTermGraph, Kind: plugin.CatalogKindGraph, Name: graphName},
		},
	}, nil
}

func graphOverviewRows(info *datatype.GraphInfo) ([]string, []map[string]interface{}) {
	columns := []string{"类型", "名称", "数量", "连接模式", "属性"}
	rows := make([]map[string]interface{}, 0, len(info.NodeShapes)+len(info.RelationshipShapes))
	for _, shape := range info.NodeShapes {
		rows = append(rows, map[string]interface{}{
			"类型":   "节点",
			"名称":   graphDisplayValue(graphNodeShapeName(shape)),
			"数量":   graphCountDisplayValue(shape.Count),
			"连接模式": "-",
			"属性":   graphPropertiesDisplayValue(shape.Properties),
		})
	}
	for _, shape := range info.RelationshipShapes {
		rows = append(rows, graphRelationshipRows(shape)...)
	}
	return columns, rows
}

func graphRelationshipRows(shape datatype.GraphRelationshipShapeInfo) []map[string]interface{} {
	properties := graphPropertiesDisplayValue(shape.Properties)
	if len(shape.Patterns) == 0 {
		return []map[string]interface{}{{
			"类型":   "关系",
			"名称":   graphDisplayValue(shape.Type),
			"数量":   graphCountDisplayValue(shape.Count),
			"连接模式": "-",
			"属性":   properties,
		}}
	}
	rows := make([]map[string]interface{}, 0, len(shape.Patterns))
	for _, pattern := range shape.Patterns {
		rows = append(rows, map[string]interface{}{
			"类型":   "关系",
			"名称":   graphDisplayValue(shape.Type),
			"数量":   graphCountDisplayValue(pattern.Count),
			"连接模式": graphDisplayValue(graphPatternLabel(pattern)),
			"属性":   properties,
		})
	}
	return rows
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

func graphCountDisplayValue(count *int64) interface{} {
	if count == nil {
		return "-"
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

func graphPropertiesDisplayValue(fields []datatype.FieldInfo) string {
	return graphDisplayValue(strings.Join(graphFieldNames(fields), ", "))
}

func graphPatternLabels(patterns []datatype.GraphRelationshipPatternInfo) []string {
	result := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		result = append(result, graphPatternLabel(pattern))
	}
	return result
}

func graphPatternLabel(pattern datatype.GraphRelationshipPatternInfo) string {
	from := graphEndpointLabel(pattern.From)
	to := graphEndpointLabel(pattern.To)
	if from == "" && to == "" {
		return ""
	}
	return fmt.Sprintf("%s -> %s", graphDisplayValue(from), graphDisplayValue(to))
}

func graphEndpointLabel(endpoint datatype.GraphEndpointInfo) string {
	if endpoint.ShapeName != "" {
		return endpoint.ShapeName
	}
	return strings.Join(endpoint.Labels, "+")
}

func graphDisplayValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
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
