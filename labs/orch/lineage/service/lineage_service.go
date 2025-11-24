package service

import (
	"context"
	"encoding/json"
	"fmt"
	"lineage/models"
	"lineage/repository"
	"time"
)

// LineageService 血缘服务（实现 LineageRecorder 接口）
type LineageService struct {
	repo *repository.LineageRepository
}

// NewLineageService 创建服务实例
func NewLineageService(repo *repository.LineageRepository) *LineageService {
	return &LineageService{repo: repo}
}

// Record 记录单条血缘
func (s *LineageService) Record(ctx context.Context, input *models.LineageInput) (*models.DataLineage, error) {
	// 转换 LineageInput 到 DataLineage 模型
	lineage := &models.DataLineage{
		// 外部工作流信息
		ExternalWorkflowID:  input.ExternalWorkflowID,
		ExternalExecutionID: input.ExternalExecutionID,
		WorkflowEngine:      input.WorkflowEngine,

		// 源数据
		SourceItemID:      input.SourceItemID,
		SourceFingerprint: input.SourceFingerprint,
		SourceType:        input.SourceType,
		SourcePath:        input.SourcePath,

		// 目标数据
		TargetItemID:      input.TargetItemID,
		TargetFingerprint: input.TargetFingerprint,
		TargetType:        input.TargetType,
		TargetPath:        input.TargetPath,

		// 转换信息
		LineageType: input.LineageType,
		Status:      input.Status,
		ErrorMessage: input.ErrorMessage,
		StartTime:   input.StartTime,
		EndTime:     &input.EndTime,

		// 指标
		RecordsProcessed: input.RecordsProcessed,
		BytesWritten:     input.BytesWritten,
	}

	// 计算持续时间
	if input.EndTime.After(input.StartTime) {
		lineage.DurationMs = input.EndTime.Sub(input.StartTime).Milliseconds()
	}

	// 序列化 JSON 字段
	if input.TransformConfig != nil {
		configJSON, err := json.Marshal(input.TransformConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal transform_config: %w", err)
		}
		lineage.TransformConfig = string(configJSON)
	}

	if input.Metrics != nil {
		metricsJSON, err := json.Marshal(input.Metrics)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal metrics: %w", err)
		}
		lineage.Metrics = string(metricsJSON)
	}

	// 调用 Repository 持久化
	return s.repo.Create(ctx, lineage)
}

// RecordBatch 批量记录血缘
func (s *LineageService) RecordBatch(ctx context.Context, inputs []*models.LineageInput) error {
	lineages := make([]*models.DataLineage, 0, len(inputs))

	for _, input := range inputs {
		lineage := &models.DataLineage{
			ExternalWorkflowID:  input.ExternalWorkflowID,
			ExternalExecutionID: input.ExternalExecutionID,
			WorkflowEngine:      input.WorkflowEngine,
			SourceItemID:        input.SourceItemID,
			SourceFingerprint:   input.SourceFingerprint,
			SourceType:          input.SourceType,
			SourcePath:          input.SourcePath,
			TargetItemID:        input.TargetItemID,
			TargetFingerprint:   input.TargetFingerprint,
			TargetType:          input.TargetType,
			TargetPath:          input.TargetPath,
			LineageType:         input.LineageType,
			Status:              input.Status,
			ErrorMessage:        input.ErrorMessage,
			StartTime:           input.StartTime,
			EndTime:             &input.EndTime,
			RecordsProcessed:    input.RecordsProcessed,
			BytesWritten:        input.BytesWritten,
		}

		// 计算持续时间
		if input.EndTime.After(input.StartTime) {
			lineage.DurationMs = input.EndTime.Sub(input.StartTime).Milliseconds()
		}

		// 序列化 JSON 字段
		if input.TransformConfig != nil {
			configJSON, err := json.Marshal(input.TransformConfig)
			if err != nil {
				return fmt.Errorf("failed to marshal transform_config: %w", err)
			}
			lineage.TransformConfig = string(configJSON)
		}

		if input.Metrics != nil {
			metricsJSON, err := json.Marshal(input.Metrics)
			if err != nil {
				return fmt.Errorf("failed to marshal metrics: %w", err)
			}
			lineage.Metrics = string(metricsJSON)
		}

		lineages = append(lineages, lineage)
	}

	return s.repo.CreateBatch(ctx, lineages)
}

// QueryUpstream 查询上游血缘（数据来源）
func (s *LineageService) QueryUpstream(ctx context.Context, itemID uint) ([]*models.DataLineage, error) {
	return s.repo.FindByTargetItemID(ctx, itemID)
}

// QueryDownstream 查询下游血缘（数据去向）
func (s *LineageService) QueryDownstream(ctx context.Context, itemID uint) ([]*models.DataLineage, error) {
	return s.repo.FindBySourceItemID(ctx, itemID)
}

// Query 灵活查询
func (s *LineageService) Query(ctx context.Context, query *models.LineageQuery) ([]*models.DataLineage, error) {
	return s.repo.Query(ctx, query)
}

// QueryByWorkflowID 根据工作流 ID 查询血缘
func (s *LineageService) QueryByWorkflowID(ctx context.Context, workflowID string) ([]*models.DataLineage, error) {
	return s.repo.FindByExternalWorkflowID(ctx, workflowID)
}

// QueryByExecutionID 根据执行 ID 查询血缘
func (s *LineageService) QueryByExecutionID(ctx context.Context, executionID string) ([]*models.DataLineage, error) {
	return s.repo.FindByExternalExecutionID(ctx, executionID)
}

// GetByID 根据 ID 查询单条血缘
func (s *LineageService) GetByID(ctx context.Context, id uint) (*models.DataLineage, error) {
	return s.repo.FindByID(ctx, id)
}

// Count 统计血缘记录数
func (s *LineageService) Count(ctx context.Context, query *models.LineageQuery) (int64, error) {
	return s.repo.Count(ctx, query)
}

// Delete 删除血缘记录（软删除）
func (s *LineageService) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

// BuildLineageGraph 构建血缘图（递归查询上下游）
func (s *LineageService) BuildLineageGraph(ctx context.Context, itemID uint, maxDepth int) (*LineageGraph, error) {
	graph := &LineageGraph{
		Nodes: make(map[uint]*LineageNode),
		Edges: make([]*LineageEdge, 0),
	}

	// 递归构建上游
	if err := s.buildUpstreamGraph(ctx, itemID, 0, maxDepth, graph); err != nil {
		return nil, err
	}

	// 递归构建下游
	if err := s.buildDownstreamGraph(ctx, itemID, 0, maxDepth, graph); err != nil {
		return nil, err
	}

	return graph, nil
}

func (s *LineageService) buildUpstreamGraph(ctx context.Context, itemID uint, currentDepth, maxDepth int, graph *LineageGraph) error {
	if currentDepth >= maxDepth {
		return nil
	}

	lineages, err := s.repo.FindByTargetItemID(ctx, itemID)
	if err != nil {
		return err
	}

	for _, lineage := range lineages {
		// 添加节点
		if _, exists := graph.Nodes[lineage.SourceItemID]; !exists {
			graph.Nodes[lineage.SourceItemID] = &LineageNode{
				ItemID:      lineage.SourceItemID,
				Fingerprint: lineage.SourceFingerprint,
				Type:        lineage.SourceType,
				Path:        lineage.SourcePath,
			}
		}

		// 添加边
		graph.Edges = append(graph.Edges, &LineageEdge{
			SourceItemID: lineage.SourceItemID,
			TargetItemID: lineage.TargetItemID,
			LineageType:  lineage.LineageType,
			CreatedAt:    lineage.CreatedAt,
		})

		// 递归查询上游
		if err := s.buildUpstreamGraph(ctx, lineage.SourceItemID, currentDepth+1, maxDepth, graph); err != nil {
			return err
		}
	}

	return nil
}

func (s *LineageService) buildDownstreamGraph(ctx context.Context, itemID uint, currentDepth, maxDepth int, graph *LineageGraph) error {
	if currentDepth >= maxDepth {
		return nil
	}

	lineages, err := s.repo.FindBySourceItemID(ctx, itemID)
	if err != nil {
		return err
	}

	for _, lineage := range lineages {
		// 添加节点
		if _, exists := graph.Nodes[lineage.TargetItemID]; !exists {
			graph.Nodes[lineage.TargetItemID] = &LineageNode{
				ItemID:      lineage.TargetItemID,
				Fingerprint: lineage.TargetFingerprint,
				Type:        lineage.TargetType,
				Path:        lineage.TargetPath,
			}
		}

		// 添加边（避免重复）
		edgeExists := false
		for _, edge := range graph.Edges {
			if edge.SourceItemID == lineage.SourceItemID && edge.TargetItemID == lineage.TargetItemID {
				edgeExists = true
				break
			}
		}
		if !edgeExists {
			graph.Edges = append(graph.Edges, &LineageEdge{
				SourceItemID: lineage.SourceItemID,
				TargetItemID: lineage.TargetItemID,
				LineageType:  lineage.LineageType,
				CreatedAt:    lineage.CreatedAt,
			})
		}

		// 递归查询下游
		if err := s.buildDownstreamGraph(ctx, lineage.TargetItemID, currentDepth+1, maxDepth, graph); err != nil {
			return err
		}
	}

	return nil
}

// LineageGraph 血缘图结构
type LineageGraph struct {
	Nodes map[uint]*LineageNode `json:"nodes"`
	Edges []*LineageEdge        `json:"edges"`
}

// LineageNode 血缘图节点
type LineageNode struct {
	ItemID      uint   `json:"item_id"`
	Fingerprint string `json:"fingerprint"`
	Type        string `json:"type"`
	Path        string `json:"path"`
}

// LineageEdge 血缘图边
type LineageEdge struct {
	SourceItemID uint      `json:"source_item_id"`
	TargetItemID uint      `json:"target_item_id"`
	LineageType  string    `json:"lineage_type"`
	CreatedAt    time.Time `json:"created_at"`
}
