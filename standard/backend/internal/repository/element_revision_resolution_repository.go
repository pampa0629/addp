package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/standard/internal/models"
)

type ResolvedCodeSetRevision struct {
	CodeSet  models.CodeSet
	Revision models.CodeSetRevision
}

func (r *ElementRepository) ResolveEffectiveRevisions(
	ctx context.Context,
	tenantID int64,
	elementIDs []int64,
	asOf time.Time,
) ([]models.Element, []models.ElementRevision, error) {
	elements := []models.Element{}
	if len(elementIDs) == 0 {
		return elements, []models.ElementRevision{}, nil
	}
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND lifecycle_state = ? AND id IN ?", tenantID, "active", elementIDs).
		Find(&elements).Error; err != nil {
		return nil, nil, fmt.Errorf("resolve active data elements: %w", err)
	}
	revisions := []models.ElementRevision{}
	query := r.db.WithContext(ctx).Table("standard.element_revisions AS er").Select("er.*").
		Joins("JOIN standard.elements e ON e.id = er.element_id").
		Where("e.tenant_id = ? AND e.lifecycle_state = ? AND er.element_id IN ?", tenantID, "active", elementIDs)
	if err := effectiveAt(query, "er", asOf).
		Order("er.element_id ASC, er.effective_from DESC, er.revision_no DESC").
		Find(&revisions).Error; err != nil {
		return nil, nil, fmt.Errorf("resolve effective data element revisions: %w", err)
	}
	return elements, revisions, nil
}

func (r *CodeSetRepository) ResolveRevisionSnapshots(
	ctx context.Context,
	tenantID int64,
	revisionIDs []int64,
) ([]ResolvedCodeSetRevision, error) {
	if len(revisionIDs) == 0 {
		return []ResolvedCodeSetRevision{}, nil
	}
	revisions := []models.CodeSetRevision{}
	if err := r.db.WithContext(ctx).Table("standard.code_set_revisions AS csr").Select("csr.*").
		Joins("JOIN standard.code_sets cs ON cs.id = csr.code_set_id").
		Where("cs.tenant_id = ? AND csr.id IN ?", tenantID, revisionIDs).
		Find(&revisions).Error; err != nil {
		return nil, fmt.Errorf("resolve code set revisions: %w", err)
	}
	codeSetIDs := make([]int64, 0, len(revisions))
	resolvedRevisionIDs := make([]int64, 0, len(revisions))
	for _, revision := range revisions {
		codeSetIDs = append(codeSetIDs, revision.CodeSetID)
		resolvedRevisionIDs = append(resolvedRevisionIDs, revision.ID)
	}
	codeSets := []models.CodeSet{}
	if len(codeSetIDs) > 0 {
		if err := r.db.WithContext(ctx).Where("tenant_id = ? AND id IN ?", tenantID, codeSetIDs).Find(&codeSets).Error; err != nil {
			return nil, fmt.Errorf("resolve code set identities: %w", err)
		}
	}
	items := []models.CodeSetRevisionItem{}
	if len(resolvedRevisionIDs) > 0 {
		if err := r.db.WithContext(ctx).Where("code_set_revision_id IN ?", resolvedRevisionIDs).
			Order("code_set_revision_id ASC, sort_order ASC, id ASC").Find(&items).Error; err != nil {
			return nil, fmt.Errorf("resolve code set revision items: %w", err)
		}
	}
	identityByID := make(map[int64]models.CodeSet, len(codeSets))
	for _, codeSet := range codeSets {
		identityByID[codeSet.ID] = codeSet
	}
	itemsByRevisionID := make(map[int64][]models.CodeSetRevisionItem, len(revisions))
	for _, item := range items {
		itemsByRevisionID[item.CodeSetRevisionID] = append(itemsByRevisionID[item.CodeSetRevisionID], item)
	}
	result := make([]ResolvedCodeSetRevision, 0, len(revisions))
	for _, revision := range revisions {
		identity, ok := identityByID[revision.CodeSetID]
		if !ok {
			return nil, fmt.Errorf("code set revision %d has no tenant identity", revision.ID)
		}
		revision.Items = itemsByRevisionID[revision.ID]
		result = append(result, ResolvedCodeSetRevision{CodeSet: identity, Revision: revision})
	}
	return result, nil
}
