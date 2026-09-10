package service

import (
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/security/internal/models"
)

// createTypeWithoutBaseline constructs anomaly states used by focused service
// tests. It is compiled only for tests; production has one atomic create path.
func (s *DefinitionService) createTypeWithoutBaseline(req models.SensitiveDataTypeRequest, tenantID, userID int64) (*models.SensitiveDataType, error) {
	if err := s.validateTypeRefs(req.SecurityClassificationID, req.DefaultSecurityGradeID, tenantID); err != nil {
		return nil, err
	}
	row := &models.SensitiveDataType{
		TenantID: tenantID, Code: strings.TrimSpace(req.Code), Name: strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description), SecurityClassificationID: req.SecurityClassificationID,
		DefaultSecurityGradeID: req.DefaultSecurityGradeID, CreatedBy: userID,
	}
	if row.Code == "" || row.Name == "" {
		return nil, commonapi.ErrBadRequest
	}
	if err := s.types.Create(row); err != nil {
		return nil, err
	}
	return row, nil
}
