package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

const MaxElementRevisionResolutionBatchSize = 200

var ErrInvalidElementRevisionResolutionRequest = errors.New("invalid data element revision resolution request")

type CodeItemSnapshot struct {
	Code              string `json:"code"`
	Label             string `json:"label"`
	Definition        string `json:"definition,omitempty"`
	SortOrder         int    `json:"sort_order"`
	Status            string `json:"status" enums:"active,deprecated"`
	ReplacementItemID *int64 `json:"replacement_item_id,omitempty,string" swaggertype:"string"`
}

type CodeSetRevisionSnapshot struct {
	CodeSetID     int64              `json:"code_set_id,string" swaggertype:"string"`
	RevisionID    int64              `json:"revision_id,string" swaggertype:"string"`
	RevisionNo    int64              `json:"revision_no"`
	Code          string             `json:"code"`
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	ValueType     string             `json:"value_type"`
	Status        string             `json:"status" enums:"draft,in_review,published,withdrawn"`
	EffectiveFrom *time.Time         `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time         `json:"effective_to,omitempty"`
	Items         []CodeItemSnapshot `json:"items"`
}

type ElementRevisionSnapshot struct {
	ElementID         int64                    `json:"element_id,string" swaggertype:"string"`
	ElementRevisionID int64                    `json:"element_revision_id,string" swaggertype:"string"`
	RevisionNo        int64                    `json:"revision_no"`
	DomainID          *int64                   `json:"domain_id,omitempty,string" swaggertype:"string"`
	Code              string                   `json:"code"`
	Name              string                   `json:"name"`
	Definition        string                   `json:"definition"`
	DataType          string                   `json:"data_type"`
	Length            *int                     `json:"length,omitempty"`
	PrecisionNum      *int                     `json:"precision_num,omitempty"`
	Scale             *int                     `json:"scale,omitempty"`
	Nullable          bool                     `json:"nullable"`
	DefaultValue      string                   `json:"default_value"`
	Format            string                   `json:"format"`
	ValueDomainKind   string                   `json:"value_domain_kind" enums:"unrestricted,range,enumeration"`
	RangeConstraint   *models.RangeConstraint  `json:"range_constraint,omitempty"`
	CodeSetRevision   *CodeSetRevisionSnapshot `json:"code_set_revision,omitempty"`
	UnitID            *int64                   `json:"unit_id,omitempty,string" swaggertype:"string"`
	ExampleValues     []string                 `json:"example_values"`
	EffectiveFrom     time.Time                `json:"effective_from"`
	EffectiveTo       *time.Time               `json:"effective_to,omitempty"`
}

type ElementRevisionResolution struct {
	ElementID int64                    `json:"element_id,string" swaggertype:"string"`
	Found     bool                     `json:"found"`
	Snapshot  *ElementRevisionSnapshot `json:"snapshot,omitempty"`
}

type ElementRevisionResolutionService struct {
	elements *repository.ElementRepository
	codeSets *repository.CodeSetRepository
}

func NewElementRevisionResolutionService(
	elements *repository.ElementRepository,
	codeSets *repository.CodeSetRepository,
) *ElementRevisionResolutionService {
	return &ElementRevisionResolutionService{elements: elements, codeSets: codeSets}
}

func (s *ElementRevisionResolutionService) Resolve(
	ctx context.Context,
	tenantID int64,
	elementIDs []int64,
	asOf time.Time,
) ([]ElementRevisionResolution, error) {
	if s == nil || s.elements == nil || s.codeSets == nil || tenantID <= 0 || asOf.IsZero() || len(elementIDs) == 0 || len(elementIDs) > MaxElementRevisionResolutionBatchSize {
		return nil, ErrInvalidElementRevisionResolutionRequest
	}
	seen := make(map[int64]struct{}, len(elementIDs))
	for _, elementID := range elementIDs {
		if elementID <= 0 {
			return nil, ErrInvalidElementRevisionResolutionRequest
		}
		if _, duplicate := seen[elementID]; duplicate {
			return nil, ErrInvalidElementRevisionResolutionRequest
		}
		seen[elementID] = struct{}{}
	}
	elements, revisions, err := s.elements.ResolveEffectiveRevisions(ctx, tenantID, elementIDs, asOf.UTC())
	if err != nil {
		return nil, err
	}
	elementByID := make(map[int64]models.Element, len(elements))
	for _, element := range elements {
		elementByID[element.ID] = element
	}
	revisionByElementID := make(map[int64]models.ElementRevision, len(revisions))
	codeSetRevisionIDs := make([]int64, 0, len(revisions))
	for _, revision := range revisions {
		if _, duplicate := revisionByElementID[revision.ElementID]; duplicate {
			return nil, fmt.Errorf("element %d resolved to multiple effective revisions", revision.ElementID)
		}
		revisionByElementID[revision.ElementID] = revision
		if revision.CodeSetRevisionID != nil {
			codeSetRevisionIDs = append(codeSetRevisionIDs, *revision.CodeSetRevisionID)
		}
	}
	codeSetRecords, err := s.codeSets.ResolveRevisionSnapshots(ctx, tenantID, uniqueInt64s(codeSetRevisionIDs))
	if err != nil {
		return nil, err
	}
	codeSetByRevisionID := make(map[int64]repository.ResolvedCodeSetRevision, len(codeSetRecords))
	for _, record := range codeSetRecords {
		codeSetByRevisionID[record.Revision.ID] = record
	}
	results := make([]ElementRevisionResolution, 0, len(elementIDs))
	for _, elementID := range elementIDs {
		result := ElementRevisionResolution{ElementID: elementID}
		element, elementFound := elementByID[elementID]
		revision, revisionFound := revisionByElementID[elementID]
		if !elementFound || !revisionFound || revision.EffectiveFrom == nil {
			results = append(results, result)
			continue
		}
		snapshot := &ElementRevisionSnapshot{
			ElementID: element.ID, ElementRevisionID: revision.ID, RevisionNo: revision.RevisionNo,
			DomainID: element.DomainID, Code: element.Code, Name: revision.Name, Definition: revision.Definition,
			DataType: revision.DataType, Length: revision.Length, PrecisionNum: revision.PrecisionNum, Scale: revision.Scale,
			Nullable: revision.Nullable, DefaultValue: revision.DefaultValue, Format: revision.Format,
			ValueDomainKind: revision.ValueDomainKind, RangeConstraint: revision.RangeConstraint,
			UnitID:        revision.UnitID,
			ExampleValues: append([]string{}, revision.ExampleValues...), EffectiveFrom: revision.EffectiveFrom.UTC(), EffectiveTo: utcTimePointer(revision.EffectiveTo),
		}
		if revision.CodeSetRevisionID != nil {
			record, ok := codeSetByRevisionID[*revision.CodeSetRevisionID]
			if !ok {
				return nil, fmt.Errorf("element revision %d references missing code set revision %d", revision.ID, *revision.CodeSetRevisionID)
			}
			snapshot.CodeSetRevision = codeSetSnapshot(record)
		}
		result.Found, result.Snapshot = true, snapshot
		results = append(results, result)
	}
	return results, nil
}

func codeSetSnapshot(record repository.ResolvedCodeSetRevision) *CodeSetRevisionSnapshot {
	revision := record.Revision
	items := make([]CodeItemSnapshot, 0, len(revision.Items))
	for _, item := range revision.Items {
		items = append(items, CodeItemSnapshot{
			Code: item.Code, Label: item.Label, Definition: item.Definition, SortOrder: item.SortOrder,
			Status: item.Status, ReplacementItemID: item.ReplacementItemID,
		})
	}
	return &CodeSetRevisionSnapshot{
		CodeSetID: record.CodeSet.ID, RevisionID: revision.ID, RevisionNo: revision.RevisionNo,
		Code: record.CodeSet.Code, Name: revision.Name, Description: revision.Description, ValueType: revision.ValueType,
		Status: revision.Status, EffectiveFrom: utcTimePointer(revision.EffectiveFrom), EffectiveTo: utcTimePointer(revision.EffectiveTo), Items: items,
	}
}

func uniqueInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
