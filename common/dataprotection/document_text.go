package dataprotection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DataItemSecuritySampleSchemaV1 = "addp.data_item_security_sample/v1"
	DocumentTextComponentKey       = "$document.text"
	AlgorithmPhoneOccurrencesV1    = "addp.mask.phone_occurrences/v1"
)

type DataItemSecuritySample struct {
	SchemaVersion      string    `json:"schema_version"`
	ItemFingerprint    string    `json:"item_fingerprint"`
	ItemType           string    `json:"item_type"`
	Text               string    `json:"text"`
	Truncated          bool      `json:"truncated"`
	SourceSnapshotHash string    `json:"source_snapshot_hash"`
	ObservedAt         time.Time `json:"observed_at"`
}

func (s DataItemSecuritySample) Validate() error {
	if s.SchemaVersion != DataItemSecuritySampleSchemaV1 || strings.TrimSpace(s.ItemFingerprint) == "" || strings.TrimSpace(s.ItemType) == "" || s.ObservedAt.IsZero() {
		return errors.New("invalid DataItem security sample identity")
	}
	hash, err := DocumentTextSnapshotHash(s.Text, s.Truncated)
	if err != nil {
		return err
	}
	if s.SourceSnapshotHash != hash {
		return errors.New("DataItem security sample snapshot hash mismatch")
	}
	return nil
}

func DocumentTextSnapshotHash(text string, truncated bool) (string, error) {
	payload, err := json.Marshal(struct {
		Schema    string `json:"schema"`
		Text      string `json:"text"`
		Truncated bool   `json:"truncated"`
	}{Schema: "addp.document_text_snapshot/v1", Text: text, Truncated: truncated})
	if err != nil {
		return "", fmt.Errorf("marshal document text snapshot: %w", err)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func DocumentTextComponent() Component {
	payload := []byte(`{"schema":"addp.document_text_component/v1","key":"$document.text","value_type":"string"}`)
	digest := sha256.Sum256(payload)
	return Component{
		Key: DocumentTextComponentKey,
		Path: []PathSegment{
			{Name: "$document", Container: "object"},
			{Name: "text", Container: "scalar"},
		},
		ValueType:         "string",
		SchemaFingerprint: "sha256:" + hex.EncodeToString(digest[:]),
	}
}

func ValidateDocumentTextProjection(projection Projection, action, text string, truncated bool, now time.Time) ([]Rule, error) {
	if projection.State != ProjectionStateActive {
		return nil, errors.New("document text projection is not active")
	}
	if err := projection.Validate(now); err != nil {
		return nil, err
	}
	snapshotHash, err := DocumentTextSnapshotHash(text, truncated)
	if err != nil {
		return nil, err
	}
	if projection.SourceSnapshotHash != snapshotHash {
		return nil, errors.New("document text projection snapshot mismatch")
	}
	component := DocumentTextComponent()
	if projection.Target.ComponentKey != "" && projection.Target.ComponentKey != component.Key {
		return nil, errors.New("document text projection target component mismatch")
	}
	result := make([]Rule, 0, 1)
	for _, rule := range projection.Rules {
		if rule.Action != action {
			continue
		}
		if rule.Component.Key != component.Key || rule.Component.SchemaFingerprint != component.SchemaFingerprint || rule.Component.ValueType != component.ValueType {
			return nil, errors.New("document text projection component mismatch")
		}
		result = append(result, rule)
	}
	if len(result) == 0 {
		return nil, errors.New("document text projection action is missing")
	}
	return result, nil
}

func MaskPhoneOccurrences(text string, decision Decision) (string, error) {
	if decision.Effect != EffectMask || decision.Algorithm != AlgorithmPhoneOccurrencesV1 {
		return "", errors.New("invalid phone occurrence masking decision")
	}
	if err := validateKeepPrefixSuffixParameters(decision.Parameters); err != nil {
		return "", err
	}
	prefix, _ := integerParameter(decision.Parameters, "prefix_runes")
	suffix, _ := integerParameter(decision.Parameters, "suffix_runes")
	exact, _ := integerParameter(decision.Parameters, "exact_runes")
	replacement := decision.Parameters["replacement"].(string)
	runes := []rune(text)
	var output strings.Builder
	for index := 0; index < len(runes); {
		if runes[index] < '0' || runes[index] > '9' {
			output.WriteRune(runes[index])
			index++
			continue
		}
		end := index
		for end < len(runes) && runes[end] >= '0' && runes[end] <= '9' {
			end++
		}
		if end-index == exact {
			output.WriteString(string(runes[index : index+prefix]))
			output.WriteString(replacement)
			output.WriteString(string(runes[end-suffix : end]))
		} else {
			output.WriteString(string(runes[index:end]))
		}
		index = end
	}
	return output.String(), nil
}
