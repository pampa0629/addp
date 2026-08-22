package service

import (
	"fmt"

	commonClient "github.com/addp/common/client"
	engineselection "github.com/addp/common/engine/selection"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/preview"
)

// ExplorerService 提供 Manager 数据探查页所需的业务入口。
// 资源树事实查询、搜索和刷新统一由 Meta resource-tree API 承担。
type ExplorerService struct {
	systemClient *commonClient.SystemClient
}

func NewExplorerService(
	systemClient *commonClient.SystemClient,
	_ *commonClient.MetaClient,
	_ *preview.PreviewResolver,
) *ExplorerService {
	return &ExplorerService{
		systemClient: systemClient,
	}
}

// GetEngineList 获取可用于探查的存储引擎列表。
func (s *ExplorerService) GetEngineList(tenantID *uint) ([]*commonModels.Engine, error) {
	if s.systemClient == nil {
		return nil, fmt.Errorf("system client not available")
	}

	var tid uint
	if tenantID != nil {
		tid = *tenantID
	}

	engines, err := s.systemClient.ListEngines("", tid)
	if err != nil {
		return nil, fmt.Errorf("failed to list engines: %w", err)
	}

	var storageEngines []*commonModels.Engine
	for i := range engines {
		if engineselection.IsStorageSelectionOption(&engines[i]) {
			storageEngines = append(storageEngines, &engines[i])
		}
	}

	return storageEngines, nil
}
