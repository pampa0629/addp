package protection

import (
	"errors"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
)

func ProtectContentIndexDocument(document *commonClient.ManagerContentDocument, gate projectionstore.GateResult, now time.Time) error {
	if document == nil || document.PayloadKind != commonClient.ManagerContentPayloadExtractedContent {
		return ErrRequired
	}
	if !gate.Managed {
		return nil
	}
	if gate.Err != nil || gate.State != dataprotection.ProjectionStateActive || len(gate.Projections) == 0 {
		return ErrRequired
	}
	rules := make([]dataprotection.Rule, 0, 1)
	for _, projection := range gate.Projections {
		if projection.ConsumerOwner != ConsumerOwner || projection.Target.OwnerModule != "meta" || projection.Target.ResourceType != "data_item" || projection.Target.ResourceIdentity != document.DocumentID {
			return ErrRequired
		}
		projectionRules, err := dataprotection.ValidateDocumentTextProjection(projection, ActionSearchIndex, document.Content, document.ContentTruncated, now)
		if err != nil {
			return ErrRequired
		}
		rules = append(rules, projectionRules...)
	}
	if len(rules) != 1 {
		return ErrRequired
	}
	decision := rules[0].Decision
	switch decision.Effect {
	case dataprotection.EffectMask:
		return maskContentIndexStrings(document, decision)
	case dataprotection.EffectSuppress, dataprotection.EffectDeny:
		return ErrRequired
	default:
		return ErrRequired
	}
}

func maskContentIndexStrings(document *commonClient.ManagerContentDocument, decision dataprotection.Decision) error {
	var err error
	for _, target := range []*string{&document.Content, &document.ContentPreview, &document.Title, &document.Author, &document.Description} {
		*target, err = dataprotection.MaskPhoneOccurrences(*target, decision)
		if err != nil {
			return ErrRequired
		}
	}
	for index := range document.Tags {
		document.Tags[index], err = dataprotection.MaskPhoneOccurrences(document.Tags[index], decision)
		if err != nil {
			return ErrRequired
		}
	}
	for index := range document.Keywords {
		document.Keywords[index], err = dataprotection.MaskPhoneOccurrences(document.Keywords[index], decision)
		if err != nil {
			return ErrRequired
		}
	}
	if document.Metadata != nil {
		protected, err := maskContentIndexValue(document.Metadata, decision)
		if err != nil {
			return ErrRequired
		}
		metadata, ok := protected.(map[string]interface{})
		if !ok {
			return ErrRequired
		}
		document.Metadata = metadata
	}
	return nil
}

func maskContentIndexValue(value interface{}, decision dataprotection.Decision) (interface{}, error) {
	switch typed := value.(type) {
	case string:
		return dataprotection.MaskPhoneOccurrences(typed, decision)
	case []interface{}:
		result := make([]interface{}, len(typed))
		for index, child := range typed {
			protected, err := maskContentIndexValue(child, decision)
			if err != nil {
				return nil, err
			}
			result[index] = protected
		}
		return result, nil
	case map[string]interface{}:
		result := make(map[string]interface{}, len(typed))
		for key, child := range typed {
			protectedKey, err := dataprotection.MaskPhoneOccurrences(key, decision)
			if err != nil {
				return nil, err
			}
			if _, exists := result[protectedKey]; exists && protectedKey != key {
				return nil, errors.New("masked metadata key collision")
			}
			protected, err := maskContentIndexValue(child, decision)
			if err != nil {
				return nil, err
			}
			result[protectedKey] = protected
		}
		return result, nil
	default:
		return value, nil
	}
}
