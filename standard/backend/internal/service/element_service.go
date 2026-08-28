package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataquality"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ElementService struct {
	repo     *repository.ElementRepository
	codeSets *repository.CodeSetRepository
	refs     *repository.TenantReferenceRepository
	deletion *StandardReferenceDeletionService
}

func NewElementService(repo *repository.ElementRepository, codeSets *repository.CodeSetRepository, refs *repository.TenantReferenceRepository, deletion *StandardReferenceDeletionService) *ElementService {
	return &ElementService{repo: repo, codeSets: codeSets, refs: refs, deletion: deletion}
}

func (s *ElementService) CreateElement(req *models.CreateElementRequest, tenantID, userID int64) (*models.ElementAggregate, error) {
	if err := s.validateStableReferences(tenantID, req.DomainID); err != nil {
		return nil, err
	}
	revision, err := s.revisionFromCreate(req, tenantID, userID)
	if err != nil {
		return nil, err
	}
	exists, err := s.repo.ExistsByCode(strings.TrimSpace(req.Code), tenantID, 0)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, commonapi.ErrConflict
	}
	element := &models.Element{TenantID: tenantID, DomainID: req.DomainID, Code: strings.TrimSpace(req.Code), StewardID: req.StewardID, Tags: req.Tags, CreatedBy: userID, LifecycleState: "active"}
	if err := s.repo.Create(element, revision); err != nil {
		return nil, err
	}
	return s.repo.GetAggregate(element.ID, tenantID)
}

func (s *ElementService) GetElement(id, tenantID int64) (*models.ElementAggregate, error) {
	return s.repo.GetAggregate(id, tenantID)
}

func (s *ElementService) ListElements(tenantID int64, opts repository.ListElementOptions) ([]models.ElementAggregate, int64, error) {
	return s.repo.List(tenantID, opts)
}

func (s *ElementService) UpdateElement(id, tenantID, userID int64, req *models.UpdateElementRequest) (*models.ElementAggregate, error) {
	if err := s.validateStableReferences(tenantID, req.DomainID); err != nil {
		return nil, err
	}
	element, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	element.DomainID, element.StewardID, element.Tags, element.UpdatedBy = req.DomainID, req.StewardID, req.Tags, &userID
	if err := s.repo.UpdateIdentity(element, req.Version); err != nil {
		return nil, err
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *ElementService) ListRevisions(id, tenantID int64) ([]models.ElementRevision, error) {
	return s.repo.ListRevisions(id, tenantID)
}
func (s *ElementService) GetRevision(id, revisionID, tenantID int64) (*models.ElementRevision, error) {
	return s.repo.GetRevision(id, revisionID, tenantID)
}

func (s *ElementService) CreateRevision(id, tenantID, userID int64, req *models.CreateElementRevisionRequest) (*models.ElementAggregate, error) {
	_, err := s.repo.CreateDraft(id, tenantID, userID, req.Version, strings.TrimSpace(req.ChangeSummary))
	if err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *ElementService) UpdateRevision(id, revisionID, tenantID, userID int64, req *models.UpdateElementRevisionRequest) (*models.ElementAggregate, error) {
	revision, err := s.revisionFromUpdate(id, revisionID, req, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateDraft(id, revisionID, tenantID, userID, req.Version, revision); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *ElementService) SubmitRevision(id, revisionID, tenantID, userID, version int64) (*models.ElementAggregate, error) {
	revision, err := s.repo.GetRevision(id, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.validateRevision(revision, tenantID); err != nil {
		return nil, err
	}
	if err := s.repo.TransitionRevision(id, revisionID, tenantID, userID, version, models.RevisionStatusDraft, models.RevisionStatusInReview); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *ElementService) ReturnRevision(id, revisionID, tenantID, userID, version int64) (*models.ElementAggregate, error) {
	if err := s.repo.TransitionRevision(id, revisionID, tenantID, userID, version, models.RevisionStatusInReview, models.RevisionStatusDraft); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *ElementService) PublishRevision(id, revisionID, tenantID, userID, version int64) (*models.ElementAggregate, error) {
	revision, err := s.repo.GetRevision(id, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if revision.Status != models.RevisionStatusInReview {
		return nil, ErrInvalidRevisionTransition
	}
	if err := s.validateRevision(revision, tenantID); err != nil {
		return nil, err
	}
	compiled, err := s.compileQualityRules(id, revision, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.PublishRevision(id, revisionID, tenantID, userID, version, compiled); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *ElementService) WithdrawRevision(id, revisionID, tenantID, userID, version int64) (*models.ElementAggregate, error) {
	if err := s.repo.WithdrawPublished(id, revisionID, tenantID, userID, version); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *ElementService) DeleteElement(ctx context.Context, id, tenantID int64) error {
	return s.deletion.Delete(ctx, tenantID, "element", id, func(tx *gorm.DB, resourceID, resourceTenantID int64) error {
		return s.repo.DeleteTx(tx, resourceID, resourceTenantID)
	})
}

func (s *ElementService) GetPublishedQualityRules(id, tenantID int64) (*models.ElementRevision, *dataquality.Document, error) {
	revision, err := s.repo.GetPublishedRevision(id, tenantID)
	if err != nil {
		return nil, nil, err
	}
	document, err := dataquality.FromValue(revision.CompiledQualityRules)
	if err != nil {
		return nil, nil, err
	}
	return revision, &document, nil
}

func (s *ElementService) validateStableReferences(tenantID int64, domainID *int64) error {
	return s.refs.RequireDomain(tenantID, domainID)
}

func (s *ElementService) revisionFromCreate(req *models.CreateElementRequest, tenantID, userID int64) (*models.ElementRevision, error) {
	extra, err := normalizeExtraQualityRules(req.ExtraQualityRules)
	if err != nil {
		return nil, err
	}
	revision := &models.ElementRevision{
		Name: strings.TrimSpace(req.Name), Definition: strings.TrimSpace(req.Definition), DataType: strings.TrimSpace(req.DataType), Length: req.Length,
		PrecisionNum: req.PrecisionNum, Scale: req.Scale, Nullable: req.Nullable, DefaultValue: req.DefaultValue, Format: req.Format,
		ValueDomainKind: req.ValueDomainKind, RangeConstraint: req.RangeConstraint, CodeSetRevisionID: req.CodeSetRevisionID,
		UnitID: req.UnitID, SecurityLevel: req.SecurityLevel, ClassificationID: req.ClassificationID, ExampleValues: req.ExampleValues,
		ExtraQualityRules: extra, ChangeSummary: strings.TrimSpace(req.ChangeSummary), EffectiveFrom: req.EffectiveFrom, EffectiveTo: req.EffectiveTo, CreatedBy: userID,
	}
	if err := s.validateRevision(revision, tenantID); err != nil {
		return nil, err
	}
	return revision, nil
}

func (s *ElementService) revisionFromUpdate(elementID, revisionID int64, req *models.UpdateElementRevisionRequest, tenantID, userID int64) (*models.ElementRevision, error) {
	extra, err := normalizeExtraQualityRules(req.ExtraQualityRules)
	if err != nil {
		return nil, err
	}
	revision := &models.ElementRevision{
		ID: revisionID, ElementID: elementID, Name: strings.TrimSpace(req.Name), Definition: strings.TrimSpace(req.Definition), DataType: strings.TrimSpace(req.DataType),
		Length: req.Length, PrecisionNum: req.PrecisionNum, Scale: req.Scale, Nullable: req.Nullable, DefaultValue: req.DefaultValue, Format: req.Format,
		ValueDomainKind: req.ValueDomainKind, RangeConstraint: req.RangeConstraint, CodeSetRevisionID: req.CodeSetRevisionID,
		UnitID: req.UnitID, SecurityLevel: req.SecurityLevel, ClassificationID: req.ClassificationID, ExampleValues: req.ExampleValues,
		ExtraQualityRules: extra, ChangeSummary: strings.TrimSpace(req.ChangeSummary), EffectiveFrom: req.EffectiveFrom, EffectiveTo: req.EffectiveTo, UpdatedBy: &userID,
	}
	if err := s.validateRevision(revision, tenantID); err != nil {
		return nil, err
	}
	return revision, nil
}

func (s *ElementService) validateRevision(revision *models.ElementRevision, tenantID int64) error {
	if revision == nil || strings.TrimSpace(revision.Name) == "" || strings.TrimSpace(revision.Definition) == "" || strings.TrimSpace(revision.ChangeSummary) == "" {
		return ErrInvalidStandardRevision
	}
	dataType := strings.TrimSpace(revision.DataType)
	validType := map[string]bool{"string": true, "int": true, "bigint": true, "float": true, "decimal": true, "date": true, "datetime": true, "bool": true, "json": true, "text": true}
	if !validType[dataType] {
		return fmt.Errorf("%w: unsupported data_type %q", ErrInvalidStandardRevision, dataType)
	}
	if revision.Length != nil && (*revision.Length <= 0 || (dataType != "string" && dataType != "text")) {
		return fmt.Errorf("%w: length is only valid for string or text", ErrInvalidStandardRevision)
	}
	if revision.PrecisionNum != nil && (*revision.PrecisionNum <= 0 || dataType != "decimal") {
		return fmt.Errorf("%w: precision_num is only valid for decimal", ErrInvalidStandardRevision)
	}
	if revision.Scale != nil && (*revision.Scale < 0 || dataType != "decimal") {
		return fmt.Errorf("%w: scale is only valid for decimal", ErrInvalidStandardRevision)
	}
	if revision.PrecisionNum != nil && revision.Scale != nil && *revision.Scale > *revision.PrecisionNum {
		return fmt.Errorf("%w: scale must not exceed precision_num", ErrInvalidStandardRevision)
	}
	if revision.Format != "" && dataType != "string" && dataType != "text" && dataType != "date" && dataType != "datetime" {
		return fmt.Errorf("%w: format is incompatible with data_type", ErrInvalidStandardRevision)
	}
	if revision.SecurityLevel != "" && revision.SecurityLevel != "L1" && revision.SecurityLevel != "L2" && revision.SecurityLevel != "L3" && revision.SecurityLevel != "L4" {
		return fmt.Errorf("%w: invalid security_level", ErrInvalidStandardRevision)
	}
	if revision.EffectiveFrom != nil && revision.EffectiveTo != nil && !revision.EffectiveFrom.Before(*revision.EffectiveTo) {
		return fmt.Errorf("%w: effective_from must precede effective_to", ErrInvalidStandardRevision)
	}
	if err := s.refs.RequireUnit(tenantID, revision.UnitID); err != nil {
		return err
	}
	if err := s.refs.RequireClassification(tenantID, revision.ClassificationID); err != nil {
		return err
	}
	switch revision.ValueDomainKind {
	case models.ValueDomainUnrestricted:
		if revision.RangeConstraint != nil || revision.CodeSetRevisionID != nil {
			return fmt.Errorf("%w: unrestricted value domain cannot define range or code set", ErrInvalidStandardRevision)
		}
	case models.ValueDomainRange:
		if revision.RangeConstraint == nil || revision.CodeSetRevisionID != nil {
			return fmt.Errorf("%w: range value domain requires only range_constraint", ErrInvalidStandardRevision)
		}
		if dataType != "int" && dataType != "bigint" && dataType != "float" && dataType != "decimal" {
			return fmt.Errorf("%w: range value domain requires a numeric data_type", ErrInvalidStandardRevision)
		}
		if revision.RangeConstraint.Min == nil && revision.RangeConstraint.Max == nil {
			return fmt.Errorf("%w: range_constraint requires min or max", ErrInvalidStandardRevision)
		}
		if _, err := rangeNumbers(revision.RangeConstraint); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidStandardRevision, err)
		}
	case models.ValueDomainEnumeration:
		if revision.RangeConstraint != nil || revision.CodeSetRevisionID == nil {
			return fmt.Errorf("%w: enumeration value domain requires only code_set_revision_id", ErrInvalidStandardRevision)
		}
		codeSetRevision, err := s.codeSets.GetPublishedRevision(*revision.CodeSetRevisionID, tenantID)
		if err != nil {
			return ErrPublishedRevisionRequired
		}
		if !compatibleValueTypes(dataType, codeSetRevision.ValueType) {
			return fmt.Errorf("%w: code set value_type is incompatible with data_type", ErrInvalidStandardRevision)
		}
	default:
		return fmt.Errorf("%w: invalid value_domain_kind", ErrInvalidStandardRevision)
	}
	return validateDefaultValue(dataType, revision.DefaultValue)
}

func (s *ElementService) compileQualityRules(elementID int64, revision *models.ElementRevision, tenantID int64) (models.JSONB, error) {
	document, err := dataquality.FromValue(revision.ExtraQualityRules)
	if err != nil {
		return nil, err
	}
	add := func(kind string, params dataquality.Parameters) {
		document.Rules = append(document.Rules, dataquality.Rule{RuleKey: stableRuleKey(elementID, kind), Type: kind, Enabled: true, Severity: dataquality.SeverityError, Message: "", Params: params})
	}
	if !revision.Nullable {
		add(dataquality.RuleTypeNotNull, dataquality.Parameters{})
	}
	if revision.Length != nil {
		value := json.Number(strconv.Itoa(*revision.Length))
		add(dataquality.RuleTypeLength, dataquality.Parameters{Max: &value})
	}
	if strings.TrimSpace(revision.Format) != "" {
		pattern := revision.Format
		add(dataquality.RuleTypeFormat, dataquality.Parameters{Pattern: &pattern})
	}
	if revision.ValueDomainKind == models.ValueDomainRange {
		add(dataquality.RuleTypeValueRange, dataquality.Parameters{Min: revision.RangeConstraint.Min, Max: revision.RangeConstraint.Max, MinInclusive: revision.RangeConstraint.MinInclusive, MaxInclusive: revision.RangeConstraint.MaxInclusive})
	}
	if revision.ValueDomainKind == models.ValueDomainEnumeration {
		codeSet, err := s.codeSets.GetPublishedRevision(*revision.CodeSetRevisionID, tenantID)
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(codeSet.Items))
		for _, item := range codeSet.Items {
			if item.Status == models.CodeItemStatusActive {
				values = append(values, item.Code)
			}
		}
		if len(values) == 0 {
			return nil, fmt.Errorf("%w: enumeration code set has no active items", ErrInvalidStandardRevision)
		}
		add(dataquality.RuleTypeAllowedValues, dataquality.Parameters{Values: values})
	}
	value, err := dataquality.ToMap(document)
	return models.JSONB(value), err
}

func normalizeExtraQualityRules(value map[string]interface{}) (models.JSONB, error) {
	document := dataquality.EmptyDocument()
	var err error
	if value != nil {
		document, err = dataquality.FromValue(value)
		if err != nil {
			return nil, err
		}
	}
	for _, rule := range document.Rules {
		if rule.Type != dataquality.RuleTypeUnique {
			return nil, fmt.Errorf("%w: structural quality rule %q must be defined by data element fields", ErrInvalidStandardRevision, rule.Type)
		}
	}
	normalized, err := dataquality.ToMap(document)
	return models.JSONB(normalized), err
}

func stableRuleKey(elementID int64, kind string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("addp.standard.element:%d:%s", elementID, kind))).String()
}

func compatibleValueTypes(elementType, codeSetType string) bool {
	if codeSetType == "string" {
		return elementType == "string" || elementType == "text"
	}
	if codeSetType == "int" {
		return elementType == "int"
	}
	return codeSetType == "bigint" && (elementType == "bigint" || elementType == "int")
}

func rangeNumbers(value *models.RangeConstraint) (float64, error) {
	var min, max float64
	var err error
	if value.Min != nil {
		min, err = strconv.ParseFloat(value.Min.String(), 64)
		if err != nil {
			return 0, errors.New("range min must be numeric")
		}
	}
	if value.Max != nil {
		max, err = strconv.ParseFloat(value.Max.String(), 64)
		if err != nil {
			return 0, errors.New("range max must be numeric")
		}
	}
	if value.Min != nil && value.Max != nil && min > max {
		return 0, errors.New("range min must not exceed max")
	}
	return 0, nil
}

func validateDefaultValue(dataType, value string) error {
	if value == "" {
		return nil
	}
	switch dataType {
	case "int", "bigint":
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return fmt.Errorf("%w: default_value is not an integer", ErrInvalidStandardRevision)
		}
	case "float", "decimal":
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return fmt.Errorf("%w: default_value is not numeric", ErrInvalidStandardRevision)
		}
	case "bool":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%w: default_value is not boolean", ErrInvalidStandardRevision)
		}
	case "json":
		if !json.Valid([]byte(value)) {
			return fmt.Errorf("%w: default_value is not valid JSON", ErrInvalidStandardRevision)
		}
	}
	return nil
}

func mapRevisionError(err error) error {
	switch {
	case errors.Is(err, repository.ErrDraftAlreadyExists):
		return ErrDraftRevisionExists
	case errors.Is(err, repository.ErrInvalidRevisionTransition), errors.Is(err, repository.ErrRevisionNotEditable):
		return ErrInvalidRevisionTransition
	default:
		return err
	}
}
