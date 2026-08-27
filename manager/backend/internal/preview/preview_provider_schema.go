package preview

import (
	"context"
	"fmt"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/resourceutil"
)

type schemaPreviewProvider struct {
	metadataRepo *repository.MetadataRepository
	metaClient   *commonClient.MetaClient
}

func NewSchemaPreviewProvider(metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient) PreviewProvider {
	return &schemaPreviewProvider{
		metadataRepo: metadataRepo,
		metaClient:   metaClient,
	}
}

func (p *schemaPreviewProvider) Name() string {
	return "builtin:schema-node"
}

func (p *schemaPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	_ = ctx

	metaClient := p.metaClient
	if metaClient != nil && req.TenantID != nil {
		metaClient = metaClient.WithTenantID(*req.TenantID)
	}

	node, err := p.metadataRepo.GetNodeByName(req.Engine.ID, req.Schema, metaClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get node info: %w", err)
	}

	nodeType := "schema"
	if req != nil && strings.TrimSpace(req.NodeType) != "" {
		nodeType = req.NodeType
	} else if isObjectStorageType(req.Engine.EngineType) {
		nodeType = "bucket"
	}

	bucket := ""
	if isObjectStorageType(req.Engine.EngineType) {
		bucket = req.Schema
	}

	children := make([]models.ObjectPreviewChild, 0)

	if childNodes, err := p.metadataRepo.GetChildNodes(node.ID, metaClient); err == nil {
		for _, child := range childNodes {
			children = append(children, models.ObjectPreviewChild{
				Name:      child.Name,
				Path:      child.FullName,
				Type:      child.NodeType,
				SizeBytes: child.TotalSizeBytes,
			})
		}
	}

	if items, err := p.metadataRepo.GetNodeItems(node.ID, metaClient); err == nil {
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
			if contentType := resourceutil.StringAttribute(item.Attributes, "content_type"); contentType != "" {
				child.ContentType = contentType
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
	if req != nil && req.Engine != nil {
		preview.Object.EngineID = req.Engine.ID
	}

	return preview, nil
}
