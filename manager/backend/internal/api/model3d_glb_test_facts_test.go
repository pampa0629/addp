package api

import (
	"context"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
)

// These existing HTTP tests exercise Actor/confirmation for NFS, not FTP Allow.
type glbNFSEngineFacts struct{}

func (glbNFSEngineFacts) GetEngineForTenant(_ context.Context, tenant, id uint) (*commonModels.Engine, error) {
	return &commonModels.Engine{ID: id, TenantID: &tenant, EngineType: "nfs", LifecycleState: "active", ConnectionStatus: "online"}, nil
}

type glbFTPEngineFacts struct{}

func (glbFTPEngineFacts) GetEngineForTenant(_ context.Context, tenant, id uint) (*commonModels.Engine, error) {
	return &commonModels.Engine{ID: id, TenantID: &tenant, EngineType: "ftp", Version: 1, LifecycleState: "active", ConnectionStatus: "online"}, nil
}
func newGLBTaskServiceWithNFSFacts(repo *repository.Model3DGLBRepository) *service.Model3DGLBTaskService {
	svc := service.NewModel3DGLBTaskService(repo)
	svc.SetSourceEngineClient(glbNFSEngineFacts{})
	return svc
}
