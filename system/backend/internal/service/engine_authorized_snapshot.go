package service

import (
	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/models"
)

// DecryptAuthorizedExecutionSnapshot is a pure projection for the composition
// root. The caller must have qualified this exact Engine in its authoritative
// transaction. No lookup, connection test, policy fallback or cache is used.
func (s *EngineService) DecryptAuthorizedExecutionSnapshot(engine *models.Engine) (*models.Engine, error) {
	if s == nil || engine == nil || engine.ID == 0 || engine.TenantID == nil || *engine.TenantID == 0 || engine.Version <= 0 || engine.EngineType != "ftp" || engine.LifecycleState != models.EngineLifecycleActive || engine.DeletedAt != nil {
		return nil, commonapi.ErrForbidden
	}
	connection, err := s.decryptStoredConnectionInfo(engine.EngineType, engine.ConnectionInfo)
	if err != nil {
		return nil, commonapi.ErrForbidden
	}
	copy := *engine
	copy.ConnectionInfo = connection
	return &copy, nil
}
