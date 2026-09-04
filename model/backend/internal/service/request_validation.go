package service

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
)

var modelCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func invalidRequest() error {
	return apperrors.Validation("invalid_request", i18n.MsgValidationFailed)
}

func validOptionalID(value *int64) bool {
	return value == nil || *value > 0
}

func validRequiredString(value string, maxLength int) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= maxLength
}

func validOptionalStringLength(value string, maxLength int) bool {
	return utf8.RuneCountInString(value) <= maxLength
}

func validValue(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateCreateEntityRequest(req *models.CreateEntityRequest) error {
	if req == nil || !validOptionalID(req.DomainID) || !validRequiredString(req.Name, 200) ||
		!modelCodePattern.MatchString(req.Code) || utf8.RuneCountInString(req.Code) > 100 {
		return invalidRequest()
	}
	return nil
}

func validateCreateEntityAttributeRequest(req *models.CreateEntityAttributeRequest) error {
	if req == nil || !validOptionalID(req.ElementID) || !validRequiredString(req.Name, 200) ||
		!modelCodePattern.MatchString(req.ColumnName) || utf8.RuneCountInString(req.ColumnName) > 200 ||
		!validValue(req.DataType, modelDataTypes...) || req.SortOrder < 0 {
		return invalidRequest()
	}
	return nil
}

func validateCreateEntityRelationRequest(req *models.CreateEntityRelationRequest) error {
	if req == nil || req.SourceEntity <= 0 || req.TargetEntity <= 0 ||
		!validValue(req.RelationType, "one_to_one", "one_to_many", "many_to_many") || !validOptionalStringLength(req.Name, 200) {
		return invalidRequest()
	}
	return nil
}

func validateCreateLogicalTableRequest(req *models.CreateLogicalTableRequest) error {
	if req == nil || !validOptionalID(req.DomainID) || !validOptionalID(req.EntityID) ||
		!validRequiredString(req.Name, 200) || !modelCodePattern.MatchString(req.Code) || utf8.RuneCountInString(req.Code) > 200 ||
		!validValue(req.TableType, "entity", "fact", "dimension") || !validRequiredString(req.Layer, 20) {
		return invalidRequest()
	}
	return nil
}

func validateCreateLogicalFieldRequest(req *models.CreateLogicalFieldRequest) error {
	if req == nil || !validOptionalID(req.ElementID) ||
		!validRequiredString(req.Name, 200) || !modelCodePattern.MatchString(req.ColumnName) || utf8.RuneCountInString(req.ColumnName) > 200 ||
		!validValue(req.DataType, modelDataTypes...) || req.SortOrder < 0 ||
		(req.Length != nil && *req.Length <= 0) {
		return invalidRequest()
	}
	if req.FieldRole != "" && !validValue(req.FieldRole, modelFieldRoles...) {
		return invalidRequest()
	}
	return nil
}

func validateCreateTableRelationRequest(req *models.CreateTableRelationRequest) error {
	if req == nil || req.TargetTable <= 0 || req.SourceField <= 0 || req.TargetField <= 0 ||
		(req.RelationType != "" && !validValue(req.RelationType, "fk", "join")) {
		return invalidRequest()
	}
	return nil
}

func validateCreateDWLayerRequest(req *models.CreateDWLayerRequest) error {
	if req == nil || !modelCodePattern.MatchString(req.LayerCode) || utf8.RuneCountInString(req.LayerCode) > 20 ||
		!validRequiredString(req.LayerName, 100) || req.SortOrder < 0 {
		return invalidRequest()
	}
	return nil
}

func validListStatus(status string) bool {
	return status == "" || validValue(status, "draft", "approved")
}

var modelDataTypes = []string{"string", "int", "bigint", "float", "decimal", "date", "datetime", "bool", "json", "text", "geometry"}

var modelFieldRoles = []string{"regular", "measure_additive", "measure_semi", "measure_non", "dimension_fk", "degenerate_dim"}
