package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

func TestNormalizeStandardStableCode(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		max     int
		want    string
		wantErr bool
	}{
		{name: "snake case", value: "outdoor_activity_count", max: 100, want: "outdoor_activity_count"},
		{name: "surrounding whitespace", value: "  outdoor_activity_count  ", max: 100, want: "outdoor_activity_count"},
		{name: "uppercase", value: "Outdoor_activity", max: 100, wantErr: true},
		{name: "hyphen", value: "outdoor-activity", max: 100, wantErr: true},
		{name: "digit prefix", value: "1st_activity", max: 100, wantErr: true},
		{name: "empty", value: "  ", max: 100, wantErr: true},
		{name: "too long", value: "a" + strings.Repeat("b", 50), max: 50, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeStandardStableCode(tt.value, tt.max)
			if errors.Is(err, ErrInvalidStandardCode) != tt.wantErr {
				t.Fatalf("error = %v, want invalid=%v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("code = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPublicCreateEntryPointsRejectInvalidStableCodes(t *testing.T) {
	refs := repository.NewTenantReferenceRepository(nil)
	invalid := "Invalid-Code"
	tests := []struct {
		name string
		call func() error
	}{
		{name: "domain", call: func() error {
			_, err := NewDomainService(nil, refs, nil).CreateDomain(&models.CreateDomainRequest{Code: invalid}, 7, 9)
			return err
		}},
		{name: "standard collection", call: func() error {
			_, err := NewStandardCollectionService(nil, refs, nil).Create(t.Context(), 7, 9, &models.CreateStandardCollectionRequest{Code: invalid})
			return err
		}},
		{name: "glossary", call: func() error {
			_, err := NewGlossaryService(nil, refs).CreateGlossary(&models.CreateGlossaryRequest{ScopeType: models.StandardScopeTenantCommon, Code: invalid}, 7, 9)
			return err
		}},
		{name: "element", call: func() error {
			_, err := NewElementService(nil, nil, refs, nil).CreateElement(&models.CreateElementRequest{ScopeType: models.StandardScopeTenantCommon, Code: invalid}, 7, 9)
			return err
		}},
		{name: "code set", call: func() error {
			_, err := NewCodeSetService(nil, refs).CreateCodeSet(7, 9, &models.CreateCodeSetRequest{ScopeType: models.StandardScopeTenantCommon, Code: invalid})
			return err
		}},
		{name: "metric category", call: func() error {
			_, err := NewMetricService(nil, nil, refs, nil).CreateCategory(&models.CreateMetricCategoryRequest{Code: invalid}, 7, 9)
			return err
		}},
		{name: "metric", call: func() error {
			_, err := NewMetricService(nil, nil, refs, nil).CreateMetric(&models.CreateMetricRequest{ScopeType: models.StandardScopeTenantCommon, Code: invalid}, 7, 9)
			return err
		}},
		{name: "document", call: func() error {
			_, err := (&DocumentService{refs: refs}).CreateDocument(&models.CreateDocumentRequest{ScopeType: models.StandardScopeTenantCommon, Code: invalid}, 7, 9)
			return err
		}},
		{name: "measurement category", call: func() error {
			_, err := NewUnitService(nil, nil).CreateCategory(&models.CreateMeasurementCategoryRequest{Code: invalid}, 7)
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, ErrInvalidStandardCode) {
				t.Fatalf("error = %v, want ErrInvalidStandardCode", err)
			}
		})
	}
}
