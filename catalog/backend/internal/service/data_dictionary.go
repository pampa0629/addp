package service

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const DataDictionarySchemaVersion = "catalog.data_dictionary/v1"

type MetaFieldResolver interface {
	ResolveItemFields(context.Context, int64, int64) ([]datatype.FieldInfo, error)
}

type StandardElementRevisionResolver interface {
	ResolveElementRevisionSnapshots(context.Context, int64, []int64, time.Time) (map[int64]*commonClient.ElementRevisionBinding, error)
}

type metaClientFieldResolver struct{ client *commonClient.MetaClient }

func NewMetaClientFieldResolver(client *commonClient.MetaClient) MetaFieldResolver {
	return &metaClientFieldResolver{client: client}
}

func (r *metaClientFieldResolver) ResolveItemFields(ctx context.Context, tenantID, itemID int64) ([]datatype.FieldInfo, error) {
	if r == nil || r.client == nil || tenantID <= 0 || itemID <= 0 {
		return nil, fmt.Errorf("Meta field resolver is unavailable")
	}
	return r.client.WithTenantID(uint(tenantID)).GetItemFieldsByID(ctx, uint(itemID), true)
}

type standardClientElementRevisionResolver struct{ client *commonClient.StandardClient }

func NewStandardClientElementRevisionResolver(client *commonClient.StandardClient) StandardElementRevisionResolver {
	return &standardClientElementRevisionResolver{client: client}
}

func (r *standardClientElementRevisionResolver) ResolveElementRevisionSnapshots(
	ctx context.Context,
	tenantID int64,
	elementIDs []int64,
	asOf time.Time,
) (map[int64]*commonClient.ElementRevisionBinding, error) {
	if r == nil || r.client == nil || tenantID <= 0 {
		return nil, fmt.Errorf("Standard element revision resolver is unavailable")
	}
	return r.client.WithTenantID(uint(tenantID)).ResolveElementRevisionSnapshots(ctx, elementIDs, asOf)
}

type DataDictionary struct {
	SchemaVersion string                `json:"schema_version"`
	EntryID       uuid.UUID             `json:"entry_id" format:"uuid"`
	AsOf          time.Time             `json:"as_of"`
	GeneratedAt   time.Time             `json:"generated_at"`
	Fields        []DataDictionaryField `json:"fields"`
}

type DataDictionaryField struct {
	ComponentID *uuid.UUID                     `json:"component_id,omitempty" format:"uuid"`
	ElementID   *int64                         `json:"element_id,omitempty,string" swaggertype:"string"`
	Physical    datatype.FieldInfo             `json:"physical"`
	Standard    *DataDictionaryElementRevision `json:"standard,omitempty"`
}

// DataDictionaryElementRevision is Catalog's public projection of the exact
// Standard snapshot. A distinct named type keeps the Catalog API contract from
// exposing an implementation client type while preserving the owner payload.
type DataDictionaryElementRevision commonClient.ElementRevisionBinding

func (s *EntryService) WithDataDictionaryResolvers(meta MetaFieldResolver, standard StandardElementRevisionResolver) *EntryService {
	s.metaFields = meta
	s.elementRevisions = standard
	return s
}

func (s *EntryService) GetDataDictionary(
	ctx context.Context,
	tenantID int64,
	access EntryAccess,
	entryID uuid.UUID,
	asOf time.Time,
) (*DataDictionary, error) {
	if s == nil || s.db == nil || tenantID <= 0 || entryID == uuid.Nil || asOf.IsZero() {
		return nil, ErrInvalidPage
	}
	var entry models.Entry
	if err := s.visibleEntriesQuery(ctx, tenantID, access).Where("entries.id = ?", entryID).First(&entry).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrEntryNotFound
		}
		return nil, fmt.Errorf("get data dictionary Catalog entry: %w", err)
	}
	var source models.SourceBinding
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND catalog_entry_id = ? AND is_current = ?", tenantID, entryID, true).
		First(&source).Error; err != nil {
		return nil, fmt.Errorf("get data dictionary source: %w", err)
	}
	itemID, itemIDOK := numericInt64(source.ObservedSnapshot["item_id"])
	if entry.EntryStatus != models.EntryStatusActive || entry.EntryType != models.EntryTypeDataItem ||
		source.SourceModule != models.SourceModuleMeta || source.SourceType != models.SourceTypeDataItem ||
		source.SourceStatus != models.SourceStatusActive || !itemIDOK || itemID <= 0 {
		return nil, ErrDataDictionaryNotApplicable
	}
	if s.metaFields == nil || s.elementRevisions == nil {
		return nil, ErrDataDictionaryDependencyUnavailable
	}

	var components []models.Component
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND catalog_entry_id = ? AND component_status = ?", tenantID, entryID, models.SourceStatusActive).
		Order("ordinal ASC, component_key ASC").
		Find(&components).Error; err != nil {
		return nil, fmt.Errorf("get data dictionary components: %w", err)
	}
	componentByKey := make(map[string]models.Component, len(components))
	componentIDs := make([]uuid.UUID, 0, len(components))
	for _, component := range components {
		componentByKey[component.ComponentKey] = component
		componentIDs = append(componentIDs, component.ID)
	}
	associations := []models.ComponentElementAssociation{}
	if len(componentIDs) > 0 {
		if err := s.db.WithContext(ctx).
			Where("tenant_id = ? AND catalog_entry_id = ? AND component_id IN ?", tenantID, entryID, componentIDs).
			Find(&associations).Error; err != nil {
			return nil, fmt.Errorf("get data dictionary element associations: %w", err)
		}
	}
	associationByComponentID := make(map[uuid.UUID]models.ComponentElementAssociation, len(associations))
	for _, association := range associations {
		if _, duplicate := associationByComponentID[association.ComponentID]; duplicate {
			return nil, fmt.Errorf("component %s has multiple data element associations", association.ComponentID)
		}
		associationByComponentID[association.ComponentID] = association
	}

	fields, err := s.metaFields.ResolveItemFields(ctx, tenantID, itemID)
	if err != nil {
		return nil, fmt.Errorf("%w: Meta fields: %v", ErrDataDictionaryDependencyUnavailable, err)
	}
	elementIDs := make([]int64, 0, len(fields))
	seenElementIDs := make(map[int64]struct{}, len(fields))
	for _, field := range fields {
		component, ok := componentByKey[field.Name]
		if !ok {
			continue
		}
		association, ok := associationByComponentID[component.ID]
		if !ok {
			continue
		}
		if _, seen := seenElementIDs[association.ElementID]; !seen {
			seenElementIDs[association.ElementID] = struct{}{}
			elementIDs = append(elementIDs, association.ElementID)
		}
	}
	resolved := make(map[int64]*commonClient.ElementRevisionBinding, len(elementIDs))
	if len(elementIDs) > 0 {
		resolved, err = s.elementRevisions.ResolveElementRevisionSnapshots(ctx, tenantID, elementIDs, asOf.UTC())
		if err != nil {
			return nil, fmt.Errorf("%w: Standard revisions: %v", ErrDataDictionaryDependencyUnavailable, err)
		}
	}
	rows := make([]DataDictionaryField, 0, len(fields))
	for _, field := range fields {
		row := DataDictionaryField{Physical: field}
		if component, ok := componentByKey[field.Name]; ok {
			componentID := component.ID
			row.ComponentID = &componentID
			if association, associated := associationByComponentID[component.ID]; associated {
				elementID := association.ElementID
				row.ElementID = &elementID
				if snapshot := resolved[elementID]; snapshot != nil {
					standard := DataDictionaryElementRevision(*snapshot)
					row.Standard = &standard
				}
			}
		}
		rows = append(rows, row)
	}
	now := time.Now().UTC()
	return &DataDictionary{
		SchemaVersion: DataDictionarySchemaVersion,
		EntryID:       entryID,
		AsOf:          asOf.UTC(),
		GeneratedAt:   now,
		Fields:        rows,
	}, nil
}
