package service

import (
	"errors"
	"testing"

	"github.com/addp/graph/internal/models"
	"gorm.io/datatypes"
)

func TestNormalizeEntityTypeDisplayPropertyForcesOwnPropertySearchable(t *testing.T) {
	entityType := &models.EntityType{
		Name:            "Person",
		DisplayProperty: "full_name",
		Properties:      datatypes.JSON(`[{"name":"full_name","data_type":"string","searchable":false}]`),
	}

	if err := normalizeEntityTypeDisplayProperty(entityType, nil); err != nil {
		t.Fatalf("normalize display property: %v", err)
	}
	properties, err := entityType.ParsedProperties()
	if err != nil || len(properties) != 1 || !properties[0].Searchable {
		t.Fatalf("properties = %#v, err = %v", properties, err)
	}
}

func TestNormalizeEntityTypeDisplayPropertyAcceptsInheritedStringProperty(t *testing.T) {
	parentID := uint(1)
	parent := &models.EntityType{
		ID:         parentID,
		Name:       "Person",
		Properties: datatypes.JSON(`[{"name":"full_name","data_type":"string"}]`),
	}
	child := &models.EntityType{
		ID:              2,
		Name:            "Employee",
		ParentID:        &parentID,
		DisplayProperty: "full_name",
		Properties:      datatypes.JSON(`[]`),
	}

	if err := normalizeEntityTypeDisplayProperty(child, map[uint]*models.EntityType{parentID: parent}); err != nil {
		t.Fatalf("normalize inherited display property: %v", err)
	}
}

func TestNormalizeEntityTypeDisplayPropertyUsesChildOverride(t *testing.T) {
	parentID := uint(1)
	parent := &models.EntityType{
		ID:         parentID,
		Name:       "Person",
		Properties: datatypes.JSON(`[{"name":"code","data_type":"string"}]`),
	}
	child := &models.EntityType{
		ID:              2,
		Name:            "Employee",
		ParentID:        &parentID,
		DisplayProperty: "code",
		Properties:      datatypes.JSON(`[{"name":"code","data_type":"integer"}]`),
	}

	if err := normalizeEntityTypeDisplayProperty(child, map[uint]*models.EntityType{parentID: parent}); !errors.Is(err, ErrDisplayPropertyNotString) {
		t.Fatalf("error = %v, want %v", err, ErrDisplayPropertyNotString)
	}
}

func TestNormalizeEntityTypeDisplayPropertyRejectsInvalidProperties(t *testing.T) {
	tests := []struct {
		name            string
		displayProperty string
		properties      datatypes.JSON
		want            error
	}{
		{name: "missing", displayProperty: "missing", properties: datatypes.JSON(`[]`), want: ErrDisplayPropertyNotFound},
		{name: "non-string", displayProperty: "age", properties: datatypes.JSON(`[{"name":"age","data_type":"integer"}]`), want: ErrDisplayPropertyNotString},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entityType := &models.EntityType{Name: "Person", DisplayProperty: tt.displayProperty, Properties: tt.properties}
			if err := normalizeEntityTypeDisplayProperty(entityType, nil); !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}
