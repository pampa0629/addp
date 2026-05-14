package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
)

// GraphLabelPreviewProvider 图 label 预览
// 使用 GraphQueryProvider 采样节点属性，输出表格预览
type GraphLabelPreviewProvider struct{}

func NewGraphLabelPreviewProvider() PreviewProvider {
	return &GraphLabelPreviewProvider{}
}

func (p *GraphLabelPreviewProvider) Name() string { return "builtin:graph-label" }

func (p *GraphLabelPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	graphRuntime, connInfo, database, targetName, err := resolveGraphQuery(req)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf("MATCH (n:`%s`) RETURN n LIMIT 50", escapeCypherIdentifier(targetName))
	result, err := graphRuntime.ExecuteGraphQuery(ctx, connInfo, query, plugin.QueryOptions{
		EngineID:   req.Engine.ID,
		EngineType: req.Engine.EngineType,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to preview label: %w", err)
	}

	columns, rows := flattenGraphEntityRows(result.QueryResult.Rows, "n")
	if len(columns) == 0 {
		columns = []string{"_empty"}
	}

	return &models.TablePreview{
		Mode:            PreviewModeTable,
		Columns:         columns,
		Rows:            rows,
		Total:           len(rows),
		Page:            maxInt(req.Page, 1),
		PageSize:        normalizePageSize(req.PageSize),
		GeometryColumns: []string{},
		EngineID:        req.Engine.ID,
		Schema:          database,
		Table:           req.Table,
		EngineType:      req.Engine.EngineType,
	}, nil
}

// GraphRelationshipPreviewProvider 图 relationship 预览
// 使用 GraphQueryProvider 采样关系属性，输出表格预览
type GraphRelationshipPreviewProvider struct{}

func NewGraphRelationshipPreviewProvider() PreviewProvider {
	return &GraphRelationshipPreviewProvider{}
}

func (p *GraphRelationshipPreviewProvider) Name() string { return "builtin:graph-relationship" }

func (p *GraphRelationshipPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	graphRuntime, connInfo, database, targetName, err := resolveGraphQuery(req)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf("MATCH ()-[r:`%s`]->() RETURN r LIMIT 50", escapeCypherIdentifier(targetName))
	result, err := graphRuntime.ExecuteGraphQuery(ctx, connInfo, query, plugin.QueryOptions{
		EngineID:   req.Engine.ID,
		EngineType: req.Engine.EngineType,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to preview relationship: %w", err)
	}

	columns, rows := flattenGraphEntityRows(result.QueryResult.Rows, "r")
	if len(columns) == 0 {
		columns = []string{"_empty"}
	}

	return &models.TablePreview{
		Mode:            PreviewModeTable,
		Columns:         columns,
		Rows:            rows,
		Total:           len(rows),
		Page:            maxInt(req.Page, 1),
		PageSize:        normalizePageSize(req.PageSize),
		GeometryColumns: []string{},
		EngineID:        req.Engine.ID,
		Schema:          database,
		Table:           req.Table,
		EngineType:      req.Engine.EngineType,
	}, nil
}

func resolveGraphQuery(req *PreviewRequest) (plugin.GraphQueryProvider, plugin.ConnectionInfo, string, string, error) {
	if req == nil || req.Engine == nil {
		return nil, nil, "", "", fmt.Errorf("invalid preview request")
	}

	plug, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, nil, "", "", fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}

	graphRuntime, ok := plug.(plugin.GraphQueryProvider)
	if !ok {
		return nil, nil, "", "", fmt.Errorf("engine %s does not implement GraphQueryProvider", req.Engine.EngineType)
	}

	database := req.Schema
	if database == "" {
		database = strings.TrimSpace(fmt.Sprint(req.Engine.ConnectionInfo["database"]))
	}

	targetName := req.Table
	if database != "" && strings.HasPrefix(targetName, database+".") {
		targetName = strings.TrimPrefix(targetName, database+".")
	}
	if targetName == "" {
		return nil, nil, "", "", fmt.Errorf("target label/relationship is required")
	}

	connInfo := plugin.ConnectionInfo(req.Engine.ConnectionInfo)
	if connInfo == nil {
		connInfo = plugin.ConnectionInfo{}
	}
	if database != "" {
		connInfo["database"] = database
	}

	return graphRuntime, connInfo, database, targetName, nil
}

func graphPreviewKind(req *PreviewRequest) string {
	if req == nil {
		return ""
	}
	itemType := strings.ToLower(strings.TrimSpace(req.ItemType))
	if itemType == "label" || itemType == "relationship" {
		return itemType
	}
	nodeType := strings.ToLower(strings.TrimSpace(req.NodeType))
	if nodeType == "label" || nodeType == "relationship" {
		return nodeType
	}
	return ""
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
