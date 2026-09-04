package repository

import (
	"errors"
	"strings"
	"time"

	commonrepo "github.com/addp/common/repository"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrMetricDependencyCycle = errors.New("metric dependency cycle")

const standardMetricDependencyLockBase int64 = 2026081100000000

type MetricCategoryRepository struct{ db *gorm.DB }

func NewMetricCategoryRepository(db *gorm.DB) *MetricCategoryRepository {
	return &MetricCategoryRepository{db: db}
}
func (r *MetricCategoryRepository) List(tenantID int64) ([]models.MetricCategory, error) {
	var list []models.MetricCategory
	err := r.db.Where("tenant_id = ?", tenantID).Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}
func (r *MetricCategoryRepository) GetByID(id, tenantID int64) (*models.MetricCategory, error) {
	var item models.MetricCategory
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error
	return &item, commonrepo.WrapDBError(err)
}
func (r *MetricCategoryRepository) Create(item *models.MetricCategory) error {
	return wrapDBError(r.db.Create(item).Error)
}
func (r *MetricCategoryRepository) Update(item *models.MetricCategory, expectedVersion int64) error {
	if err := updateVersioned(r.db, item, item.ID, item.TenantID, expectedVersion, map[string]interface{}{
		"name": item.Name, "description": item.Description, "parent_id": item.ParentID, "sort_order": item.SortOrder, "updated_by": item.UpdatedBy,
	}); err != nil {
		return err
	}
	item.Version = expectedVersion + 1
	return nil
}
func (r *MetricCategoryRepository) Delete(id, tenantID int64) error {
	return deleteInTransaction(r.db, &models.MetricCategory{}, "id = ? AND tenant_id = ?", id, tenantID)
}
func (r *MetricCategoryRepository) ExistsByCode(code string, tenantID int64) (bool, error) {
	var count int64
	err := r.db.Model(&models.MetricCategory{}).Where("code = ? AND tenant_id = ?", code, tenantID).Count(&count).Error
	return count > 0, err
}

type MetricRepository struct{ db *gorm.DB }

func NewMetricRepository(db *gorm.DB) *MetricRepository { return &MetricRepository{db: db} }

type ListMetricOptions struct {
	CategoryID    *int64
	OwnerDomainID *int64
	ScopeType     string
	MetricType    string
	Status        string
	Keyword       string
	Page          int
	PageSize      int
	AsOf          time.Time
}

func (r *MetricRepository) Create(identity *models.MetricDefinition, revision *models.MetricDefinitionRevision, dependencies []models.MetricDefinitionRevisionDependency) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(identity).Error; err != nil {
			return err
		}
		revision.MetricDefinitionID, revision.RevisionNo, revision.Status = identity.ID, 1, models.RevisionStatusDraft
		if err := tx.Omit("Dependencies").Create(revision).Error; err != nil {
			return err
		}
		if err := replaceMetricRevisionDependencies(tx, revision.ID, dependencies, false); err != nil {
			return err
		}
		identity.DraftRevisionID = &revision.ID
		return tx.Model(&models.MetricDefinition{}).Where("id = ? AND tenant_id = ?", identity.ID, identity.TenantID).Update("draft_revision_id", revision.ID).Error
	}))
}

func (r *MetricRepository) GetByID(id, tenantID int64) (*models.MetricDefinition, error) {
	var item models.MetricDefinition
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error
	return &item, commonrepo.WrapDBError(err)
}

func (r *MetricRepository) GetAggregate(id, tenantID int64) (*models.MetricDefinitionAggregate, error) {
	return r.GetAggregateAt(id, tenantID, time.Time{})
}
func (r *MetricRepository) GetAggregateAt(id, tenantID int64, asOf time.Time) (*models.MetricDefinitionAggregate, error) {
	identity, err := r.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	result := &models.MetricDefinitionAggregate{MetricDefinition: *identity}
	if revision, loadErr := r.getEffectiveRevision(r.db, id, asOf); loadErr == nil {
		result.CurrentRevision = revision
	} else if !errors.Is(loadErr, gorm.ErrRecordNotFound) {
		return nil, loadErr
	}
	if identity.DraftRevisionID != nil {
		revision, loadErr := r.getRevisionByID(r.db, *identity.DraftRevisionID, id)
		if loadErr != nil {
			return nil, loadErr
		}
		result.DraftRevision = revision
	}
	return result, nil
}

func (r *MetricRepository) List(tenantID int64, opts ListMetricOptions) ([]models.MetricDefinitionAggregate, int64, error) {
	query := r.db.Model(&models.MetricDefinition{}).Where("metric_definitions.tenant_id = ?", tenantID)
	if opts.CategoryID != nil {
		query = query.Where("metric_definitions.category_id = ?", *opts.CategoryID)
	}
	if opts.OwnerDomainID != nil {
		query = query.Where("metric_definitions.owner_domain_id = ?", *opts.OwnerDomainID)
	}
	if opts.ScopeType != "" {
		query = query.Where("metric_definitions.scope_type = ?", opts.ScopeType)
	}
	if opts.Status != "" || opts.MetricType != "" {
		query = query.Joins("JOIN standard.metric_definition_revisions filter_revision ON filter_revision.metric_definition_id = metric_definitions.id")
		if opts.Status != "" {
			query = query.Where("filter_revision.status = ?", opts.Status)
		}
		if opts.MetricType != "" {
			query = query.Where("filter_revision.metric_type = ?", opts.MetricType)
		}
	}
	if keyword := strings.TrimSpace(opts.Keyword); keyword != "" {
		pattern := "%" + keyword + "%"
		query = query.Where(`metric_definitions.code ILIKE ? OR EXISTS (SELECT 1 FROM standard.metric_definition_revisions mr WHERE mr.metric_definition_id = metric_definitions.id AND (mr.name ILIKE ? OR mr.definition ILIKE ? OR mr.statistical_caliber ILIKE ?))`, pattern, pattern, pattern, pattern)
	}
	var total int64
	if err := query.Distinct("metric_definitions.id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}
	var identities []models.MetricDefinition
	if err := query.Distinct("metric_definitions.*").Order("metric_definitions.created_at DESC").Offset((opts.Page - 1) * opts.PageSize).Limit(opts.PageSize).Find(&identities).Error; err != nil {
		return nil, 0, err
	}
	items := make([]models.MetricDefinitionAggregate, 0, len(identities))
	for _, identity := range identities {
		aggregate, err := r.GetAggregateAt(identity.ID, tenantID, opts.AsOf)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *aggregate)
	}
	return items, total, nil
}

func (r *MetricRepository) GetByIDs(ids []int64, tenantID int64) ([]models.MetricDefinitionAggregate, error) {
	if len(ids) == 0 {
		return []models.MetricDefinitionAggregate{}, nil
	}
	items := make([]models.MetricDefinitionAggregate, 0, len(ids))
	for _, id := range ids {
		item, err := r.GetAggregate(id, tenantID)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (r *MetricRepository) UpdateIdentity(item *models.MetricDefinition, expectedVersion int64) error {
	if err := updateVersioned(r.db, item, item.ID, item.TenantID, expectedVersion, map[string]interface{}{
		"category_id": item.CategoryID, "scope_type": item.ScopeType, "owner_domain_id": item.OwnerDomainID,
		"steward_id": item.StewardID, "tags": item.Tags, "updated_by": item.UpdatedBy,
	}); err != nil {
		return err
	}
	item.Version = expectedVersion + 1
	return nil
}

func (r *MetricRepository) ListRevisions(metricID, tenantID int64) ([]models.MetricDefinitionRevision, error) {
	if _, err := r.GetByID(metricID, tenantID); err != nil {
		return nil, err
	}
	var revisions []models.MetricDefinitionRevision
	err := r.db.Preload("Dependencies").Where("metric_definition_id = ?", metricID).Order("revision_no DESC").Find(&revisions).Error
	return revisions, wrapDBError(err)
}

func (r *MetricRepository) GetRevision(metricID, revisionID, tenantID int64) (*models.MetricDefinitionRevision, error) {
	var revision models.MetricDefinitionRevision
	err := r.db.Table("standard.metric_definition_revisions AS mr").Select("mr.*").
		Joins("JOIN standard.metric_definitions m ON m.id = mr.metric_definition_id").
		Where("mr.id = ? AND mr.metric_definition_id = ? AND m.tenant_id = ?", revisionID, metricID, tenantID).First(&revision).Error
	if err == nil {
		err = r.db.Where("metric_definition_revision_id = ?", revision.ID).Order("id ASC").Find(&revision.Dependencies).Error
	}
	return &revision, commonrepo.WrapDBError(err)
}

func (r *MetricRepository) CreateDraft(metricID, tenantID, userID, expectedVersion int64, changeSummary string) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var identity models.MetricDefinition
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", metricID, tenantID).First(&identity).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if identity.Version != expectedVersion {
			return ErrVersionConflict
		}
		if identity.DraftRevisionID != nil {
			return ErrDraftAlreadyExists
		}
		var source models.MetricDefinitionRevision
		if err := tx.Where("metric_definition_id = ?", metricID).Order("revision_no DESC").First(&source).Error; err != nil {
			return err
		}
		var dependencies []models.MetricDefinitionRevisionDependency
		if err := tx.Where("metric_definition_revision_id = ?", source.ID).Find(&dependencies).Error; err != nil {
			return err
		}
		source.ID, source.RevisionNo, source.Status = 0, source.RevisionNo+1, models.RevisionStatusDraft
		source.ChangeSummary = changeSummary
		source.SubmittedBy, source.SubmittedAt, source.PublishedBy, source.PublishedAt = nil, nil, nil, nil
		source.CreatedBy, source.UpdatedBy, source.CreatedAt, source.UpdatedAt = userID, nil, time.Time{}, time.Time{}
		if err := tx.Omit("Dependencies").Create(&source).Error; err != nil {
			return err
		}
		for index := range dependencies {
			dependencies[index].ID = 0
			dependencies[index].MetricDefinitionRevisionID = source.ID
			dependencies[index].DependencyRevisionID = nil
			dependencies[index].CreatedAt = time.Time{}
		}
		if err := replaceMetricRevisionDependencies(tx, source.ID, dependencies, false); err != nil {
			return err
		}
		return updateVersioned(tx, &models.MetricDefinition{}, metricID, tenantID, expectedVersion, map[string]interface{}{"draft_revision_id": source.ID, "updated_by": userID})
	}))
}

func (r *MetricRepository) UpdateDraft(metricID, revisionID, tenantID, userID, expectedVersion int64, revision *models.MetricDefinitionRevision, dependencies []models.MetricDefinitionRevisionDependency) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockMetricDependencies(tx, tenantID); err != nil {
			return err
		}
		if err := r.requireRevisionState(tx, metricID, revisionID, tenantID, models.RevisionStatusDraft, true); err != nil {
			return err
		}
		if cycle, err := metricDependencyCycle(tx, metricID, tenantID, dependencies); err != nil {
			return err
		} else if cycle {
			return ErrMetricDependencyCycle
		}
		if err := updateVersioned(tx, &models.MetricDefinition{}, metricID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
			return err
		}
		if err := requireAffectedRow(tx.Model(&models.MetricDefinitionRevision{}).Where("id = ? AND metric_definition_id = ? AND status = ?", revisionID, metricID, models.RevisionStatusDraft).Updates(map[string]interface{}{
			"metric_type": revision.MetricType, "name": revision.Name, "definition": revision.Definition, "statistical_caliber": revision.StatisticalCaliber,
			"semantic_formula": revision.SemanticFormula, "unit_id": revision.UnitID, "change_summary": revision.ChangeSummary,
			"effective_from": revision.EffectiveFrom, "effective_to": revision.EffectiveTo, "updated_by": userID,
		})); err != nil {
			return err
		}
		return replaceMetricRevisionDependencies(tx, revisionID, dependencies, false)
	}))
}

func (r *MetricRepository) TransitionRevision(metricID, revisionID, tenantID, userID, expectedVersion int64, from, to string) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := r.requireRevisionState(tx, metricID, revisionID, tenantID, from, from != models.RevisionStatusPublished); err != nil {
			return err
		}
		if err := updateVersioned(tx, &models.MetricDefinition{}, metricID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
			return err
		}
		updates := map[string]interface{}{"status": to, "updated_by": userID}
		if to == models.RevisionStatusInReview {
			updates["submitted_by"] = userID
			updates["submitted_at"] = gorm.Expr("CURRENT_TIMESTAMP")
		}
		if to == models.RevisionStatusDraft {
			updates["submitted_by"] = nil
			updates["submitted_at"] = nil
		}
		return requireAffectedRow(tx.Model(&models.MetricDefinitionRevision{}).Where("id = ? AND metric_definition_id = ? AND status = ?", revisionID, metricID, from).Updates(updates))
	}))
}

func (r *MetricRepository) PublishRevision(metricID, revisionID, tenantID, userID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockMetricDependencies(tx, tenantID); err != nil {
			return err
		}
		var identity models.MetricDefinition
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", metricID, tenantID).First(&identity).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if identity.Version != expectedVersion {
			return ErrVersionConflict
		}
		if identity.DraftRevisionID == nil || *identity.DraftRevisionID != revisionID {
			return ErrInvalidRevisionTransition
		}
		var revision models.MetricDefinitionRevision
		if err := tx.Where("id = ? AND metric_definition_id = ? AND status = ?", revisionID, metricID, models.RevisionStatusInReview).First(&revision).Error; err != nil {
			return ErrInvalidRevisionTransition
		}
		if revision.EffectiveFrom == nil {
			return ErrInvalidRevisionTransition
		}
		var dependencies []models.MetricDefinitionRevisionDependency
		if err := tx.Where("metric_definition_revision_id = ?", revisionID).Find(&dependencies).Error; err != nil {
			return err
		}
		for index := range dependencies {
			resolved, err := effectiveMetricRevision(tx, dependencies[index].DependencyDefinitionID, tenantID, *revision.EffectiveFrom)
			if err != nil {
				return commonrepo.WrapDBError(err)
			}
			dependencies[index].DependencyRevisionID = &resolved.ID
		}
		if err := replaceMetricRevisionDependencies(tx, revisionID, dependencies, true); err != nil {
			return err
		}
		var published []models.MetricDefinitionRevision
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("metric_definition_id = ? AND status = ?", metricID, models.RevisionStatusPublished).Order("effective_from ASC, revision_no ASC").Find(&published).Error; err != nil {
			return err
		}
		for index := range published {
			candidate := &published[index]
			if candidate.EffectiveFrom == nil {
				return ErrEffectiveIntervalConflict
			}
			if candidate.EffectiveTo == nil && candidate.EffectiveFrom.Before(*revision.EffectiveFrom) {
				if err := tx.Model(&models.MetricDefinitionRevision{}).Where("id = ? AND status = ?", candidate.ID, models.RevisionStatusPublished).Update("effective_to", revision.EffectiveFrom).Error; err != nil {
					return err
				}
				closed := *revision.EffectiveFrom
				candidate.EffectiveTo = &closed
			}
			if intervalsOverlap(*candidate.EffectiveFrom, candidate.EffectiveTo, *revision.EffectiveFrom, revision.EffectiveTo) {
				return ErrEffectiveIntervalConflict
			}
		}
		if err := requireAffectedRow(tx.Model(&models.MetricDefinitionRevision{}).Where("id = ? AND metric_definition_id = ? AND status = ?", revisionID, metricID, models.RevisionStatusInReview).Updates(map[string]interface{}{
			"status": models.RevisionStatusPublished, "published_by": userID, "published_at": gorm.Expr("CURRENT_TIMESTAMP"), "updated_by": userID,
		})); err != nil {
			return err
		}
		return updateVersioned(tx, &models.MetricDefinition{}, metricID, tenantID, expectedVersion, map[string]interface{}{"draft_revision_id": nil, "updated_by": userID})
	}))
}

func (r *MetricRepository) WithdrawPublished(metricID, revisionID, tenantID, userID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := updateVersioned(tx, &models.MetricDefinition{}, metricID, tenantID, expectedVersion, map[string]interface{}{"updated_by": userID}); err != nil {
			return err
		}
		if err := requireAffectedRow(tx.Model(&models.MetricDefinitionRevision{}).Where("id = ? AND metric_definition_id = ? AND status = ?", revisionID, metricID, models.RevisionStatusPublished).Updates(map[string]interface{}{"status": models.RevisionStatusWithdrawn, "updated_by": userID})); err != nil {
			return ErrInvalidRevisionTransition
		}
		return nil
	}))
}

func (r *MetricRepository) GetEffectiveRevision(metricID, tenantID int64, asOf time.Time) (*models.MetricDefinitionRevision, error) {
	revision, err := effectiveMetricRevision(r.db, metricID, tenantID, asOf)
	if err == nil {
		err = r.db.Where("metric_definition_revision_id = ?", revision.ID).Order("id ASC").Find(&revision.Dependencies).Error
	}
	return revision, commonrepo.WrapDBError(err)
}

func (r *MetricRepository) Delete(id, tenantID int64) error {
	return deleteInTransaction(r.db, &models.MetricDefinition{}, "id = ? AND tenant_id = ?", id, tenantID)
}
func (r *MetricRepository) DeleteTx(tx *gorm.DB, id, tenantID int64) error {
	return requireAffectedRow(tx.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.MetricDefinition{}))
}
func (r *MetricRepository) ExistsByCode(code string, tenantID, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.MetricDefinition{}).Where("code = ? AND tenant_id = ?", code, tenantID)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	return count > 0, err
}

func (r *MetricRepository) getRevisionByID(db *gorm.DB, id, metricID int64) (*models.MetricDefinitionRevision, error) {
	var revision models.MetricDefinitionRevision
	err := db.Where("id = ? AND metric_definition_id = ?", id, metricID).First(&revision).Error
	if err == nil {
		err = db.Where("metric_definition_revision_id = ?", id).Order("id ASC").Find(&revision.Dependencies).Error
	}
	return &revision, commonrepo.WrapDBError(err)
}
func (r *MetricRepository) getEffectiveRevision(db *gorm.DB, metricID int64, asOf time.Time) (*models.MetricDefinitionRevision, error) {
	revision, err := effectiveMetricRevision(db, metricID, 0, asOf)
	if err == nil {
		err = db.Where("metric_definition_revision_id = ?", revision.ID).Order("id ASC").Find(&revision.Dependencies).Error
	}
	return revision, err
}
func effectiveMetricRevision(db *gorm.DB, metricID, tenantID int64, asOf time.Time) (*models.MetricDefinitionRevision, error) {
	var revision models.MetricDefinitionRevision
	query := db.Table("standard.metric_definition_revisions AS mr").Select("mr.*").Joins("JOIN standard.metric_definitions m ON m.id = mr.metric_definition_id").Where("m.id = ? AND m.lifecycle_state = ?", metricID, "active")
	if tenantID > 0 {
		query = query.Where("m.tenant_id = ?", tenantID)
	}
	err := effectiveAt(query, "mr", asOf).Order("mr.effective_from DESC, mr.revision_no DESC").First(&revision).Error
	return &revision, err
}
func (r *MetricRepository) requireRevisionState(tx *gorm.DB, metricID, revisionID, tenantID int64, status string, requireDraftPointer bool) error {
	query := tx.Table("standard.metric_definition_revisions AS mr").Joins("JOIN standard.metric_definitions m ON m.id = mr.metric_definition_id").Where("mr.id = ? AND mr.metric_definition_id = ? AND m.tenant_id = ? AND mr.status = ?", revisionID, metricID, tenantID, status)
	if requireDraftPointer {
		query = query.Where("m.draft_revision_id = mr.id")
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrInvalidRevisionTransition
	}
	return nil
}

func replaceMetricRevisionDependencies(tx *gorm.DB, revisionID int64, dependencies []models.MetricDefinitionRevisionDependency, preserveRevision bool) error {
	if err := tx.Where("metric_definition_revision_id = ?", revisionID).Delete(&models.MetricDefinitionRevisionDependency{}).Error; err != nil {
		return err
	}
	for _, dependency := range dependencies {
		dependency.ID, dependency.MetricDefinitionRevisionID, dependency.CreatedAt = 0, revisionID, time.Time{}
		if !preserveRevision {
			dependency.DependencyRevisionID = nil
		}
		if err := tx.Create(&dependency).Error; err != nil {
			return err
		}
	}
	return nil
}
func lockMetricDependencies(db *gorm.DB, tenantID int64) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	return db.Exec("SELECT pg_advisory_xact_lock(?)", standardMetricDependencyLockBase+tenantID).Error
}
func metricDependencyCycle(db *gorm.DB, metricID, tenantID int64, dependencies []models.MetricDefinitionRevisionDependency) (bool, error) {
	type edge struct {
		FromID int64 `gorm:"column:from_id"`
		ToID   int64 `gorm:"column:to_id"`
	}
	var edges []edge
	err := db.Table("standard.metric_definition_revision_dependencies AS d").Select("r.metric_definition_id AS from_id, d.dependency_definition_id AS to_id").Joins("JOIN standard.metric_definition_revisions r ON r.id = d.metric_definition_revision_id").Joins("JOIN standard.metric_definitions m ON m.id = r.metric_definition_id AND m.tenant_id = ?", tenantID).Where("r.metric_definition_id <> ?", metricID).Scan(&edges).Error
	if err != nil {
		return false, err
	}
	graph := map[int64][]int64{}
	for _, item := range edges {
		graph[item.FromID] = append(graph[item.FromID], item.ToID)
	}
	for _, dependency := range dependencies {
		graph[metricID] = append(graph[metricID], dependency.DependencyDefinitionID)
	}
	visiting, visited := map[int64]bool{}, map[int64]bool{}
	var visit func(int64) bool
	visit = func(current int64) bool {
		if visiting[current] {
			return true
		}
		if visited[current] {
			return false
		}
		visiting[current] = true
		for _, next := range graph[current] {
			if visit(next) {
				return true
			}
		}
		visiting[current] = false
		visited[current] = true
		return false
	}
	return visit(metricID), nil
}
