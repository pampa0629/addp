package models

import (
	"encoding/json"
	"time"

	"github.com/addp/common/dataprotection"
)

// ExportProtectionMetadataKey is private execution state, never caller input or
// a professional execution response. Missing pre-feature receipts fail closed.
const ExportProtectionMetadataKey = "export_artifact_protection"

type ExportProtectionVersion struct {
	ProjectionID string `json:"projection_id"`
	Revision     string `json:"revision"`
	Checksum     string `json:"checksum"`
}

type ExportProtectionReceipt struct {
	Ready       bool                               `json:"ready"`
	Invalidated bool                               `json:"invalidated"`
	Targets     []dataprotection.ResourceReference `json:"targets"`
	Versions    []ExportProtectionVersion          `json:"versions"`
	ValidUntil  *time.Time                         `json:"valid_until,omitempty"`
}

func ReadExportProtection(metadata map[string]interface{}) (*ExportProtectionReceipt, error) {
	raw, exists := metadata[ExportProtectionMetadataKey]
	if !exists {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var receipt ExportProtectionReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}
