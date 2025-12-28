package service

import (
	"context"
	"fmt"

	commonClient "github.com/addp/common/client"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type schemaPreviewProvider struct {
	metadataRepo *repository.MetadataRepository
	metaClient   *commonClient.MetaClient
	priority     int
}

func NewSchemaPreviewProvider(metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient) PreviewProvider {
	return &schemaPreviewProvider{
		metadataRepo: metadataRepo,
		metaClient:   metaClient,
		priority:     90,
	}
}

func (p *schemaPreviewProvider) Name() string {
	return "builtin:schema-node"
}

func (p *schemaPreviewProvider) Priority() int {
	return p.priority
}

func (p *schemaPreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Resource == nil {
		return false
	}
	if req.Schema == "" {
		return false
	}
	return req.Table == ""
}

func (p *schemaPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	_ = ctx

	node, err := p.metadataRepo.GetNodeByName(req.Resource.ID, req.Schema, p.metaClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get node info: %w", err)
	}

	isObjectStorage := isObjectStorageType(req.Resource.EngineType)

	nodeType := "schema"
	if isObjectStorage {
		nodeType = "bucket"
	}

	bucket := ""
	if isObjectStorage {
		bucket = req.Schema
	}

	children := make([]models.ObjectPreviewChild, 0)

	if childNodes, err := p.metadataRepo.GetChildNodes(node.ID, p.metaClient); err == nil {
		for _, child := range childNodes {
			children = append(children, models.ObjectPreviewChild{
				Name:      child.Name,
				Path:      child.FullName,
				Type:      child.NodeType,
				SizeBytes: child.TotalSizeBytes,
			})
		}
	}

	if items, err := p.metadataRepo.GetNodeItems(node.ID, p.metaClient); err == nil {
		for _, item := range items {
			child := models.ObjectPreviewChild{
				Name: item.Name,
				Path: item.FullName,
				Type: item.ItemType,
			}
			if item.SizeBytes != nil {
				child.SizeBytes = *item.SizeBytes
			}
			if item.ObjectSizeBytes != nil {
				child.SizeBytes = *item.ObjectSizeBytes
			}
			if v, ok := item.Attributes["content_type"].(string); ok && v != "" {
				child.ContentType = v
			}
			children = append(children, child)
		}
	}

	preview := &models.TablePreview{
		Mode:            PreviewModeNode,
		Columns:         []string{},
		Rows:            []map[string]interface{}{},
		Total:           0,
		Page:            1,
		PageSize:        1,
		GeometryColumns: []string{},
		Object: &models.ObjectPreview{
			Bucket:      bucket,
			NodeType:    nodeType,
			Path:        req.Schema,
			ObjectCount: int64(node.ItemCount),
			SizeBytes:   node.TotalSizeBytes,
			Children:    children,
		},
	}
	if req != nil && req.Resource != nil {
		preview.Object.EngineID = req.Resource.ID
	}

	return preview, nil
}
