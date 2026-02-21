package service

import (
	"fmt"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

type ElementService struct {
	repo *repository.ElementRepository
}

func NewElementService(repo *repository.ElementRepository) *ElementService {
	return &ElementService{repo: repo}
}

func (s *ElementService) CreateElement(req *models.CreateElementRequest, tenantID, userID int64) (*models.Element, error) {
	exists, err := s.repo.ExistsByCode(req.Code, tenantID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("数据元编码 '%s' 已存在", req.Code)
	}

	element := &models.Element{
		TenantID:         tenantID,
		DomainID:         req.DomainID,
		Name:             req.Name,
		Code:             req.Code,
		DataType:         req.DataType,
		Length:           req.Length,
		PrecisionNum:     req.PrecisionNum,
		Scale:            req.Scale,
		Nullable:         req.Nullable,
		DefaultValue:     req.DefaultValue,
		Format:           req.Format,
		ValueRange:       req.ValueRange,
		UnitID:           req.UnitID,
		SecurityLevel:    req.SecurityLevel,
		ClassificationID: req.ClassificationID,
		CodeSetID:        req.CodeSetID,
		Definition:       req.Definition,
		ExampleValues:    req.ExampleValues,
		QualityRules:     req.QualityRules,
		Status:           "draft",
		StewardID:        req.StewardID,
		Tags:             req.Tags,
		CreatedBy:        userID,
	}

	if err := s.repo.Create(element); err != nil {
		return nil, err
	}
	return element, nil
}

func (s *ElementService) GetElement(id, tenantID int64) (*models.Element, error) {
	return s.repo.GetByID(id, tenantID)
}

func (s *ElementService) ListElements(tenantID int64, opts repository.ListElementOptions) ([]models.Element, int64, error) {
	return s.repo.List(tenantID, opts)
}

func (s *ElementService) UpdateElement(id, tenantID, userID int64, req *models.UpdateElementRequest) (*models.Element, error) {
	element, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		element.Name = req.Name
	}
	if req.DomainID != nil {
		element.DomainID = req.DomainID
	}
	if req.DataType != "" {
		element.DataType = req.DataType
	}
	element.Length = req.Length
	element.PrecisionNum = req.PrecisionNum
	element.Scale = req.Scale
	if req.Nullable != nil {
		element.Nullable = *req.Nullable
	}
	element.DefaultValue = req.DefaultValue
	element.Format = req.Format
	if req.ValueRange != nil {
		element.ValueRange = req.ValueRange
	}
	element.UnitID = req.UnitID
	element.SecurityLevel = req.SecurityLevel
	element.ClassificationID = req.ClassificationID
	element.CodeSetID = req.CodeSetID
	element.Definition = req.Definition
	if req.ExampleValues != nil {
		element.ExampleValues = req.ExampleValues
	}
	if req.QualityRules != nil {
		element.QualityRules = req.QualityRules
	}
	element.StewardID = req.StewardID
	if req.Tags != nil {
		element.Tags = req.Tags
	}
	element.UpdatedBy = &userID

	if err := s.repo.Update(element); err != nil {
		return nil, err
	}
	return element, nil
}

func (s *ElementService) DeleteElement(id, tenantID int64) error {
	return s.repo.Delete(id, tenantID)
}

func (s *ElementService) ApproveElement(id, tenantID, userID int64) error {
	return s.repo.UpdateStatus(id, tenantID, "approved", userID)
}

func (s *ElementService) GetQualityRules(id, tenantID int64) (map[string]interface{}, error) {
	element, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if element.QualityRules == nil {
		return map[string]interface{}{"rules": []interface{}{}}, nil
	}
	return element.QualityRules, nil
}
