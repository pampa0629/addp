package service

import (
	"fmt"

	"github.com/addp/common/dataquality"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

type ElementService struct {
	repo *repository.ElementRepository
	refs *repository.TenantReferenceRepository
}

func NewElementService(repo *repository.ElementRepository, refs *repository.TenantReferenceRepository) *ElementService {
	return &ElementService{repo: repo, refs: refs}
}

func (s *ElementService) CreateElement(req *models.CreateElementRequest, tenantID, userID int64) (*models.Element, error) {
	qualityRules, err := normalizeQualityRules(req.QualityRules)
	if err != nil {
		return nil, err
	}
	if err := s.validateReferences(tenantID, req.DomainID, req.UnitID, req.ClassificationID, req.CodeSetID); err != nil {
		return nil, err
	}
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
		QualityRules:     qualityRules,
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

func (s *ElementService) validateReferences(tenantID int64, domainID, unitID, classificationID, codeSetID *int64) error {
	for _, validate := range []func() error{
		func() error { return s.refs.RequireDomain(tenantID, domainID) },
		func() error { return s.refs.RequireUnit(tenantID, unitID) },
		func() error { return s.refs.RequireClassification(tenantID, classificationID) },
		func() error { return s.refs.RequireCodeSet(tenantID, codeSetID) },
	} {
		if err := validate(); err != nil {
			return err
		}
	}
	return nil
}

func (s *ElementService) GetElement(id, tenantID int64) (*models.Element, error) {
	element, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if element.QualityRules == nil {
		document := dataquality.EmptyDocument()
		normalized, normalizeErr := dataquality.ToMap(document)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		element.QualityRules = models.JSONB(normalized)
	}
	return element, nil
}

func (s *ElementService) ListElements(tenantID int64, opts repository.ListElementOptions) ([]models.Element, int64, error) {
	elements, total, err := s.repo.List(tenantID, opts)
	if err != nil {
		return nil, 0, err
	}
	for index := range elements {
		if elements[index].QualityRules == nil {
			document := dataquality.EmptyDocument()
			normalized, normalizeErr := dataquality.ToMap(document)
			if normalizeErr != nil {
				return nil, 0, normalizeErr
			}
			elements[index].QualityRules = models.JSONB(normalized)
		}
	}
	return elements, total, nil
}

func (s *ElementService) UpdateElement(id, tenantID, userID int64, req *models.UpdateElementRequest) (*models.Element, error) {
	element, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.validateReferences(tenantID, req.DomainID, req.UnitID, req.ClassificationID, req.CodeSetID); err != nil {
		return nil, err
	}
	var qualityRules models.JSONB
	if req.QualityRules != nil {
		qualityRules, err = normalizeQualityRules(req.QualityRules)
		if err != nil {
			return nil, err
		}
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
		element.QualityRules = qualityRules
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

func (s *ElementService) GetQualityRules(id, tenantID int64) (*dataquality.Document, error) {
	element, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if element.QualityRules == nil {
		document := dataquality.EmptyDocument()
		return &document, nil
	}
	document, err := dataquality.FromValue(element.QualityRules)
	if err != nil {
		return nil, err
	}
	return &document, nil
}

func normalizeQualityRules(value map[string]interface{}) (models.JSONB, error) {
	document := dataquality.EmptyDocument()
	var err error
	if value != nil {
		document, err = dataquality.FromValue(value)
		if err != nil {
			return nil, err
		}
	}
	normalized, err := dataquality.ToMap(document)
	if err != nil {
		return nil, err
	}
	return models.JSONB(normalized), nil
}
