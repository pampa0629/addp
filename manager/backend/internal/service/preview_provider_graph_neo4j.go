package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
)

// GraphLabelPreviewProvider Neo4j label 预览
// 使用 GraphQueryPlugin 采样节点属性，输出表格预览
// 优先级高于通用数据库预览，避免被 database-table provider 抢占
type GraphLabelPreviewProvider struct {
	priority int
}

func NewGraphLabelPreviewProvider() PreviewProvider {
	return &GraphLabelPreviewProvider{priority: 102}
}

func (p *GraphLabelPreviewProvider) Name() string { return "builtin:graph-label" }

func (p *GraphLabelPreviewProvider) Priority() int { return p.priority }

func (p *GraphLabelPreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Engine == nil {
		return false
	}
	if !strings.EqualFold(req.Engine.EngineType, "neo4j") {
		return false
	}
	if req.ItemType == "" {
		return false
	}
	return req.ItemType == "label"
}

func (p *GraphLabelPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	graphPlugin, connInfo, database, targetName, err := resolveNeo4jGraphQuery(req)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf("MATCH (n:`%s`) RETURN n LIMIT 50", escapeCypherIdentifier(targetName))
	result, err := graphPlugin.ExecuteGraphQuery(ctx, connInfo, query)
	if err != nil {
		return nil, fmt.Errorf("failed to preview label: %w", err)
	}

	columns, rows := flattenGraphEntityRows(result.QueryResult.Rows, "n")
	if len(columns) == 0 {
		columns = []string{"_empty"}
	}

	return &models.TablePreview{
		Mode:           PreviewModeTable,
		Columns:        columns,
		Rows:           rows,
		Total:          len(rows),
		Page:           maxInt(req.Page, 1),
		PageSize:       normalizePageSize(req.PageSize),
		GeometryColumns: []string{},
		EngineID:       req.Engine.ID,
		Schema:         database,
		Table:          req.Table,
		EngineType:     req.Engine.EngineType,
	}, nil
}

// GraphRelationshipPreviewProvider Neo4j relationship 预览
// 使用 GraphQueryPlugin 采样关系属性，输出表格预览
type GraphRelationshipPreviewProvider struct {
	priority int
}

func NewGraphRelationshipPreviewProvider() PreviewProvider {
	return &GraphRelationshipPreviewProvider{priority: 102}
}

func (p *GraphRelationshipPreviewProvider) Name() string { return "builtin:graph-relationship" }

func (p *GraphRelationshipPreviewProvider) Priority() int { return p.priority }

func (p *GraphRelationshipPreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Engine == nil {
		return false
	}
	if !strings.EqualFold(req.Engine.EngineType, "neo4j") {
		return false
	}
	if req.ItemType == "" {
		return false
	}
	return req.ItemType == "relationship"
}

func (p *GraphRelationshipPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	graphPlugin, connInfo, database, targetName, err := resolveNeo4jGraphQuery(req)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf("MATCH ()-[r:`%s`]->() RETURN r LIMIT 50", escapeCypherIdentifier(targetName))
	result, err := graphPlugin.ExecuteGraphQuery(ctx, connInfo, query)
	if err != nil {
		return nil, fmt.Errorf("failed to preview relationship: %w", err)
	}

	columns, rows := flattenGraphEntityRows(result.QueryResult.Rows, "r")
	if len(columns) == 0 {
		columns = []string{"_empty"}
	}

	return &models.TablePreview{
		Mode:           PreviewModeTable,
		Columns:        columns,
		Rows:           rows,
		Total:          len(rows),
		Page:           maxInt(req.Page, 1),
		PageSize:       normalizePageSize(req.PageSize),
		GeometryColumns: []string{},
		EngineID:       req.Engine.ID,
		Schema:         database,
		Table:          req.Table,
		EngineType:     req.Engine.EngineType,
	}, nil
}

func resolveNeo4jGraphQuery(req *PreviewRequest) (plugin.GraphQueryPlugin, plugin.ConnectionInfo, string, string, error) {
	if req == nil || req.Engine == nil {
		return nil, nil, "", "", fmt.Errorf("invalid preview request")
	}

	plug, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, nil, "", "", fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}

	graphPlugin, ok := plug.(plugin.GraphQueryPlugin)
	if !ok {
		return nil, nil, "", "", fmt.Errorf("engine %s does not implement GraphQueryPlugin", req.Engine.EngineType)
	}

	database := req.Schema
	if database == "" {
		database = "neo4j"
	}

	targetName := req.Table
	if strings.HasPrefix(targetName, database+".") {
		targetName = strings.TrimPrefix(targetName, database+".")
	}
	if targetName == "" {
		return nil, nil, "", "", fmt.Errorf("target label/relationship is required")
	}

	connInfo := plugin.ConnectionInfo(req.Engine.ConnectionInfo)
	if connInfo == nil {
		connInfo = plugin.ConnectionInfo{}
	}
	connInfo["database"] = database

	return graphPlugin, connInfo, database, targetName, nil
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
