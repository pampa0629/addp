package service

import (
	commonapi "github.com/addp/common/api"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"regexp"
	"strings"
	"unicode/utf8"
)

type UnitService struct {
	catRepo  *repository.MeasurementCategoryRepository
	unitRepo *repository.UnitRepository
}

func NewUnitService(c *repository.MeasurementCategoryRepository, u *repository.UnitRepository) *UnitService {
	return &UnitService{catRepo: c, unitRepo: u}
}

var unitLocalePattern = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})*$`)

func validateUnitTranslations(values models.UnitTranslations) error {
	for _, lang := range []string{"zh-cn", "en"} {
		if strings.TrimSpace(values[lang].Name) == "" {
			return commonapi.ErrBadRequest
		}
	}
	if len(values) > 64 {
		return commonapi.ErrBadRequest
	}
	for lang, v := range values {
		if !unitLocalePattern.MatchString(lang) || strings.TrimSpace(v.Name) == "" || utf8.RuneCountInString(v.Name) > 100 || utf8.RuneCountInString(v.Symbol) > 30 || utf8.RuneCountInString(v.Description) > 4096 {
			return commonapi.ErrBadRequest
		}
	}
	return nil
}
func validateUnitOwner(owner models.UnitContext) error {
	if !owner.Valid() {
		return commonapi.ErrForbidden
	}
	return nil
}
func (s *UnitService) ListCategories(owner models.UnitContext) ([]models.MeasurementCategory, error) {
	if err := validateUnitOwner(owner); err != nil {
		return nil, err
	}
	return s.catRepo.List(owner)
}
func (s *UnitService) CreateCategory(req *models.CreateMeasurementCategoryRequest, owner models.UnitContext) (*models.MeasurementCategory, error) {
	code, err := normalizeStandardStableCode(req.Code, maxStandardCategoryCodeLength)
	if err != nil {
		return nil, err
	}
	if err = validateUnitOwner(owner); err != nil {
		return nil, err
	}
	if owner.Type == "platform" {
		if err = validateUnitTranslations(req.Translations); err != nil {
			return nil, err
		}
	} else if strings.TrimSpace(req.Name) == "" || len(req.Translations) > 0 {
		return nil, commonapi.ErrBadRequest
	}
	exists, err := s.catRepo.ExistsByCode(code, owner, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}
	row := &models.MeasurementCategory{TenantID: owner.OwnerID(), Name: strings.TrimSpace(req.Name), Code: code, Description: req.Description, SortOrder: req.SortOrder}
	if owner.Type == "platform" {
		row.Name = ""
		row.Description = ""
		row.Translations = req.Translations
	}
	if err = s.catRepo.Create(row); err != nil {
		return nil, err
	}
	return row, nil
}
func (s *UnitService) UpdateCategory(id int64, owner models.UnitContext, req *models.UpdateMeasurementCategoryRequest) (*models.MeasurementCategory, error) {
	if err := validateUnitOwner(owner); err != nil {
		return nil, err
	}
	row, err := s.catRepo.GetByID(id, owner)
	if err != nil {
		return nil, err
	}
	if !owner.Owns(row.TenantID) {
		return nil, ErrPlatformCategoryReadOnly
	}
	if owner.Type == "platform" {
		if err = validateUnitTranslations(req.Translations); err != nil {
			return nil, err
		}
		row.Translations = req.Translations
	} else {
		if len(req.Translations) > 0 {
			return nil, commonapi.ErrBadRequest
		}
		if req.Name != "" {
			row.Name = strings.TrimSpace(req.Name)
		}
		row.Description = req.Description
	}
	row.SortOrder = req.SortOrder
	if err = s.catRepo.Update(row, req.Version); err != nil {
		return nil, err
	}
	return s.catRepo.GetByID(id, owner)
}
func (s *UnitService) DeleteCategory(id int64, owner models.UnitContext) error {
	if err := validateUnitOwner(owner); err != nil {
		return err
	}
	row, err := s.catRepo.GetByID(id, owner)
	if err != nil {
		return err
	}
	if !owner.Owns(row.TenantID) {
		return ErrPlatformCategoryReadOnly
	}
	return mapDeleteConflict(s.catRepo.Delete(id, owner), ErrMeasurementCategoryReferenced)
}
func (s *UnitService) ListUnits(owner models.UnitContext, categoryID *int64) ([]models.Unit, error) {
	if err := validateUnitOwner(owner); err != nil {
		return nil, err
	}
	return s.unitRepo.List(owner, categoryID)
}
func (s *UnitService) GetUnit(id int64, owner models.UnitContext) (*models.Unit, error) {
	if err := validateUnitOwner(owner); err != nil {
		return nil, err
	}
	return s.unitRepo.GetByID(id, owner)
}
func (s *UnitService) CreateUnit(req *models.CreateUnitRequest, owner models.UnitContext) (*models.Unit, error) {
	if err := validateUnitOwner(owner); err != nil {
		return nil, err
	}
	row := &models.Unit{TenantID: owner.OwnerID(), CategoryID: req.CategoryID, Name: strings.TrimSpace(req.Name), Symbol: req.Symbol, Description: req.Description, SortOrder: req.SortOrder}
	if owner.Type == "platform" {
		code, err := normalizeStandardStableCode(req.Code, 100)
		if err != nil {
			return nil, err
		}
		if err = validateUnitTranslations(req.Translations); err != nil {
			return nil, err
		}
		row.Code = code
		row.Translations = req.Translations
		row.Name = ""
		row.Symbol = ""
		row.Description = ""
	} else if row.Name == "" || req.Code != "" || len(req.Translations) > 0 {
		return nil, commonapi.ErrBadRequest
	}
	if _, err := s.catRepo.GetByID(req.CategoryID, owner); err != nil {
		return nil, err
	}
	if err := s.unitRepo.Create(row); err != nil {
		return nil, err
	}
	return s.unitRepo.GetByID(row.ID, owner)
}
func (s *UnitService) UpdateUnit(id int64, owner models.UnitContext, req *models.UpdateUnitRequest) (*models.Unit, error) {
	if err := validateUnitOwner(owner); err != nil {
		return nil, err
	}
	row, err := s.unitRepo.GetByID(id, owner)
	if err != nil {
		return nil, err
	}
	if !owner.Owns(row.TenantID) {
		return nil, ErrPlatformUnitReadOnly
	}
	if owner.Type == "platform" {
		if err = validateUnitTranslations(req.Translations); err != nil {
			return nil, err
		}
		row.Translations = req.Translations
	} else {
		if len(req.Translations) > 0 {
			return nil, commonapi.ErrBadRequest
		}
		if req.Name != "" {
			row.Name = strings.TrimSpace(req.Name)
		}
		row.Symbol = req.Symbol
		row.Description = req.Description
	}
	row.SortOrder = req.SortOrder
	if err = s.unitRepo.Update(row, req.Version); err != nil {
		return nil, err
	}
	return s.unitRepo.GetByID(id, owner)
}
func (s *UnitService) DeleteUnit(id int64, owner models.UnitContext) error {
	if err := validateUnitOwner(owner); err != nil {
		return err
	}
	row, err := s.unitRepo.GetByID(id, owner)
	if err != nil {
		return err
	}
	if !owner.Owns(row.TenantID) {
		return ErrPlatformUnitReadOnly
	}
	return mapDeleteConflict(s.unitRepo.Delete(id, owner), ErrUnitReferenced)
}
