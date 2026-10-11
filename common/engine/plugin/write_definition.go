package plugin

import (
	"fmt"

	"github.com/addp/common/datatype"
)

// TableWriteDefinitionChecker is connection-free. It must never read rows,
// access catalogs, prepare a table or perform I/O.
type TableWriteDefinitionChecker interface {
	CheckTableWriteDefinition(TableWriteDefinition) TableWriteDefinitionReport
}

type DefinitionCheckStatus string

const (
	DefinitionSupported   DefinitionCheckStatus = "supported"
	DefinitionUnsupported DefinitionCheckStatus = "unsupported"
	DefinitionUnknown     DefinitionCheckStatus = "unknown"
)

type TableWriteConstant struct {
	Value interface{} `json:"value"`
}

type TableWriteFieldDefinition struct {
	Target datatype.FieldInfo  `json:"target"`
	Source *datatype.FieldInfo `json:"source,omitempty"`
	// Constant distinguishes an explicitly known NULL from no known value.
	Constant *TableWriteConstant `json:"constant,omitempty"`
}

type TableWriteDefinition struct {
	Fields      []TableWriteFieldDefinition `json:"fields"`
	SpatialInfo *datatype.SpatialInfo       `json:"spatial_info,omitempty"`
	// Existing is an observed snapshot, never proof of current absence/presence.
	Existing *datatype.TableInfo `json:"existing,omitempty"`
}

type TableWriteDefinitionCheck struct {
	Field        string                `json:"field,omitempty"`
	Check        string                `json:"check"`
	Status       DefinitionCheckStatus `json:"status"`
	Code         string                `json:"code"`
	PhysicalType string                `json:"physical_type,omitempty"`
}

type TableWriteDefinitionReport struct {
	Checks []TableWriteDefinitionCheck `json:"checks"`
}

func (r TableWriteDefinitionReport) UnsupportedError() error {
	for _, check := range r.Checks {
		if check.Status == DefinitionUnsupported {
			return fmt.Errorf("table write column %q: %s (%s)", check.Field, check.Code, check.Check)
		}
	}
	return nil
}

// TableWriteDefinitionFromOptions keeps runtime validation on the same pure
// Provider contract without claiming that source values were checked.
func TableWriteDefinitionFromOptions(opts TableWriteOptions) TableWriteDefinition {
	definition := TableWriteDefinition{SpatialInfo: opts.SpatialInfo}
	for _, field := range opts.Fields {
		definition.Fields = append(definition.Fields, TableWriteFieldDefinition{Target: field})
	}
	return definition
}
