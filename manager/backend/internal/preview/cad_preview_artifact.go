package preview

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/objectcontent"
	"github.com/addp/manager/internal/repository"
)

type cadPreviewRepositorySetter interface {
	SetCADPreviewRepository(*repository.CADPreviewRepository)
}

// ConfigureCADPreviewRepository 为存储对象预览 provider 注入 Manager 受管 CAD 预览查询能力。
func ConfigureCADPreviewRepository(registry *PreviewRegistry, repo *repository.CADPreviewRepository) {
	if registry == nil {
		return
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for _, provider := range registry.providers {
		if setter, ok := provider.(cadPreviewRepositorySetter); ok {
			setter.SetCADPreviewRepository(repo)
		}
	}
}

func isCADObjectContentRequest(req *objectcontent.ObjectContentRequest) bool {
	if req == nil {
		return false
	}
	if format.IsCADFormat(format.NormalizeFormat(req.Format)) {
		return true
	}
	return datatype.ParseDataType(commonJSON.InterfaceString(commonJSON.Value(req.Attributes, "item", "data_type"))) == datatype.CAD
}

func resolveCADPreviewURL(ctx context.Context, repo *repository.CADPreviewRepository, req *PreviewRequest, contentReq *objectcontent.ObjectContentRequest) (string, error) {
	if !isCADObjectContentRequest(contentReq) {
		return "", nil
	}
	if repo == nil || req == nil || req.TenantID == nil || *req.TenantID == 0 || strings.TrimSpace(req.ItemFingerprint) == "" {
		return "", nil
	}
	result, err := repo.GetLatestReadyByFingerprint(ctx, *req.TenantID, req.ItemFingerprint)
	if err != nil || result == nil {
		return "", err
	}
	sourceFormat := format.NormalizeFormat(contentReq.Format)
	if req.Engine == nil || result.SourceEngineID != req.Engine.ID || !format.IsCADFormat(sourceFormat) || !strings.EqualFold(strings.TrimSpace(result.SourceFormat), string(sourceFormat)) {
		return "", nil
	}
	return fmt.Sprintf("/api/v1/manager/cad-previews/%d/manifest", result.ID), nil
}
