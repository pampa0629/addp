package service

import (
	"errors"
	"strings"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

const recommendedDefinitionProfileKey = "addp.recommended_data_security/v1"

type localizedDefinition struct {
	Code            string
	NameZhCN        string
	NameEn          string
	DescriptionZhCN string
	DescriptionEn   string
	ParentCode      string
	Order           int
}

type definitionProfileSpec struct {
	Profile         models.DefinitionProfile
	Classifications []localizedDefinition
	Grades          []localizedDefinition
}

var installedDefinitionProfiles = []definitionProfileSpec{
	{
		Profile: models.DefinitionProfile{
			Key:                 recommendedDefinitionProfileKey,
			NameI18nKey:         "security.definitionProfile.recommended.name",
			DescriptionI18nKey:  "security.definitionProfile.recommended.description",
			ClassificationCount: 5,
			GradeCount:          4,
		},
		Classifications: []localizedDefinition{
			{Code: "personal_information", NameZhCN: "个人信息", NameEn: "Personal information", DescriptionZhCN: "与已识别或可识别自然人有关的信息", DescriptionEn: "Information relating to an identified or identifiable natural person", Order: 10},
			{Code: "sensitive_personal_information", NameZhCN: "敏感个人信息", NameEn: "Sensitive personal information", DescriptionZhCN: "一旦泄露或滥用容易危害人身、财产或人格权益的个人信息", DescriptionEn: "Personal information whose leakage or misuse may harm personal, property, or dignity interests", ParentCode: "personal_information", Order: 20},
			{Code: "organization_information", NameZhCN: "组织经营信息", NameEn: "Organization information", DescriptionZhCN: "组织运营、管理和业务活动产生的信息", DescriptionEn: "Information produced by organizational operations, management, and business activities", Order: 30},
			{Code: "trade_secret", NameZhCN: "商业秘密", NameEn: "Trade secret", DescriptionZhCN: "不为公众所知悉、具有商业价值并采取保密措施的信息", DescriptionEn: "Non-public commercially valuable information protected by confidentiality measures", ParentCode: "organization_information", Order: 40},
			{Code: "public_interest_information", NameZhCN: "公共与行业重要信息", NameEn: "Public and sector-critical information", DescriptionZhCN: "泄露或破坏可能影响公共利益或行业运行的信息", DescriptionEn: "Information whose disclosure or damage may affect public interests or sector operations", Order: 50},
		},
		Grades: []localizedDefinition{
			{Code: "l1", NameZhCN: "基础保护", NameEn: "Baseline protection", DescriptionZhCN: "影响范围有限，执行基础访问和使用控制", DescriptionEn: "Limited impact requiring baseline access and use controls", Order: 1},
			{Code: "l2", NameZhCN: "加强保护", NameEn: "Enhanced protection", DescriptionZhCN: "可能造成明显影响，需要加强访问和流转控制", DescriptionEn: "Material impact requiring enhanced access and transfer controls", Order: 2},
			{Code: "l3", NameZhCN: "严格保护", NameEn: "Strict protection", DescriptionZhCN: "可能造成严重影响，需要严格限制使用和披露", DescriptionEn: "Serious impact requiring strict use and disclosure restrictions", Order: 3},
			{Code: "l4", NameZhCN: "最高保护", NameEn: "Maximum protection", DescriptionZhCN: "可能造成特别严重影响，采用最高强度控制", DescriptionEn: "Critical impact requiring the strongest available controls", Order: 4},
		},
	},
}

func ListDefinitionProfiles() []models.DefinitionProfile {
	profiles := make([]models.DefinitionProfile, 0, len(installedDefinitionProfiles))
	for _, profile := range installedDefinitionProfiles {
		profiles = append(profiles, profile.Profile)
	}
	return profiles
}

func definitionProfile(key string) (definitionProfileSpec, bool) {
	key = strings.TrimSpace(key)
	for _, profile := range installedDefinitionProfiles {
		if profile.Profile.Key == key {
			return profile, true
		}
	}
	return definitionProfileSpec{}, false
}

func localizedDefinitionText(definition localizedDefinition, lang string) (string, string) {
	if lang == commoni18n.LangEn {
		return definition.NameEn, definition.DescriptionEn
	}
	return definition.NameZhCN, definition.DescriptionZhCN
}

func (s *DefinitionService) ListDefinitionProfiles() []models.DefinitionProfile {
	return ListDefinitionProfiles()
}

func (s *DefinitionService) ApplyDefinitionProfile(profileKey, lang string, tenantID, userID int64) (*models.DefinitionProfileApplication, error) {
	profile, ok := definitionProfile(profileKey)
	if !ok || tenantID <= 0 || userID <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	result := &models.DefinitionProfileApplication{ProfileKey: profile.Profile.Key}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		classificationIDs := make(map[string]int64, len(profile.Classifications))
		for _, definition := range profile.Classifications {
			var row models.SecurityClassification
			err := tx.Where("tenant_id = ? AND code = ?", tenantID, definition.Code).First(&row).Error
			if err == nil {
				classificationIDs[definition.Code] = row.ID
				continue
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			var parentID *int64
			if definition.ParentCode != "" {
				id, exists := classificationIDs[definition.ParentCode]
				if !exists {
					return commonapi.ErrBadRequest
				}
				parentID = &id
			}
			name, description := localizedDefinitionText(definition, lang)
			row = models.SecurityClassification{TenantID: tenantID, Code: definition.Code, Name: name, Description: description, ParentID: parentID, SortOrder: definition.Order, CreatedBy: userID}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			classificationIDs[definition.Code] = row.ID
			result.CreatedClassifications++
		}
		for _, definition := range profile.Grades {
			var count int64
			if err := tx.Model(&models.SecurityGrade{}).Where("tenant_id = ? AND code = ?", tenantID, definition.Code).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				continue
			}
			name, description := localizedDefinitionText(definition, lang)
			row := models.SecurityGrade{TenantID: tenantID, Code: definition.Code, Name: name, Description: description, RiskOrder: definition.Order, CreatedBy: userID}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			result.CreatedGrades++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
