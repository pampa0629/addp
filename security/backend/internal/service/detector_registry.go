package service

import (
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/resourcetree"
	"github.com/addp/security/internal/models"
)

const (
	detectorTargetFieldMetadata = "field_metadata"
	detectorTargetDocumentText  = "document_text"
	detectorEvidenceMetadata    = "metadata"
	detectorEvidenceSample      = "controlled_sample"
)

var installedDetectorCapabilities = []models.DetectorCapability{
	{
		Key: models.FindingDetectorPhoneMetadataV2, Code: "phone_metadata", Version: "v2",
		NameI18nKey: "security.detectorCapabilities.phoneMetadata.name", DescriptionI18nKey: "security.detectorCapabilities.phoneMetadata.description",
		MethodI18nKey: "security.detectorCapabilities.phoneMetadata.method", PrivacyI18nKey: "security.detectorCapabilities.phoneMetadata.privacy",
		LimitationsI18nKey: "security.detectorCapabilities.phoneMetadata.limitations",
		TargetKind:         detectorTargetFieldMetadata, EvidenceSource: detectorEvidenceMetadata,
		SupportedItemTypes:   []string{string(resourcetree.TypeTable), string(resourcetree.TypeCollection)},
		SupportedFieldTypes:  []string{string(datatype.FieldTypeString)},
		RecommendedThreshold: 0.9,
	},
	{
		Key: models.FindingDetectorEmailMetadataV1, Code: "email_metadata", Version: "v1",
		NameI18nKey: "security.detectorCapabilities.emailMetadata.name", DescriptionI18nKey: "security.detectorCapabilities.emailMetadata.description",
		MethodI18nKey: "security.detectorCapabilities.emailMetadata.method", PrivacyI18nKey: "security.detectorCapabilities.emailMetadata.privacy",
		LimitationsI18nKey: "security.detectorCapabilities.emailMetadata.limitations",
		TargetKind:         detectorTargetFieldMetadata, EvidenceSource: detectorEvidenceMetadata,
		SupportedItemTypes:   []string{string(resourcetree.TypeTable), string(resourcetree.TypeCollection)},
		SupportedFieldTypes:  []string{string(datatype.FieldTypeString)},
		RecommendedThreshold: 0.9,
	},
	{
		Key: models.FindingDetectorPhoneDocumentV1, Code: "phone_document_text", Version: "v1",
		NameI18nKey: "security.detectorCapabilities.phoneDocument.name", DescriptionI18nKey: "security.detectorCapabilities.phoneDocument.description",
		MethodI18nKey: "security.detectorCapabilities.phoneDocument.method", PrivacyI18nKey: "security.detectorCapabilities.phoneDocument.privacy",
		LimitationsI18nKey: "security.detectorCapabilities.phoneDocument.limitations",
		TargetKind:         detectorTargetDocumentText, EvidenceSource: detectorEvidenceSample,
		SupportedItemTypes:   []string{string(resourcetree.TypeFile)},
		SupportedFieldTypes:  []string{},
		RecommendedThreshold: 0.9,
	},
}

var fieldMetadataDetectorAliases = map[string]map[string]struct{}{
	models.FindingDetectorPhoneMetadataV2: {
		"phone": {}, "mobile": {}, "mobilephone": {}, "phonenumber": {}, "telephone": {}, "手机号": {}, "手机号码": {},
	},
	models.FindingDetectorEmailMetadataV1: {
		"email": {}, "emailaddress": {}, "邮箱": {}, "电子邮箱": {},
	},
}

func fieldMetadataAliases(capabilityKey string) (map[string]struct{}, bool) {
	aliases, ok := fieldMetadataDetectorAliases[strings.TrimSpace(capabilityKey)]
	return aliases, ok
}

func ListDetectorCapabilities() []models.DetectorCapability {
	result := make([]models.DetectorCapability, len(installedDetectorCapabilities))
	copy(result, installedDetectorCapabilities)
	for index := range result {
		result[index].SupportedItemTypes = append([]string(nil), result[index].SupportedItemTypes...)
		result[index].SupportedFieldTypes = append([]string(nil), result[index].SupportedFieldTypes...)
	}
	return result
}

func detectorCapability(key string) (models.DetectorCapability, bool) {
	key = strings.TrimSpace(key)
	for _, capability := range installedDetectorCapabilities {
		if capability.Key == key {
			return capability, true
		}
	}
	return models.DetectorCapability{}, false
}

func capabilitySupportsItemType(capability models.DetectorCapability, itemType string) bool {
	for _, supported := range capability.SupportedItemTypes {
		if supported == itemType {
			return true
		}
	}
	return false
}
