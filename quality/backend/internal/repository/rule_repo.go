package repository

import (
	"context"
	"database/sql"
	"fmt"
	commonAPI "github.com/addp/common/api"
	commonRepository "github.com/addp/common/repository"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
	"time"
)

type RuleRepository struct{ db *gorm.DB }

var ErrVersionConflict = fmt.Errorf("%w: resource version changed", commonAPI.ErrConflict)

func NewRuleRepository(db *gorm.DB) *RuleRepository { return &RuleRepository{db: db} }

func (r *RuleRepository) List(ctx context.Context, tenantID int64, search string, page, size int) ([]models.QualityRule, int64, error) {
	page, size = normalizePage(page, size)
	q := r.db.WithContext(ctx).Model(&models.QualityRule{}).Where("tenant_id=?", tenantID)
	if search != "" {
		q = q.Where("LOWER(name) LIKE LOWER(?) OR LOWER(code) LIKE LOWER(?)", "%"+search+"%", "%"+search+"%")
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	items := []models.QualityRule{}
	if err := q.Order("updated_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	for i := range items {
		if err := r.db.WithContext(ctx).Model(&models.PlanCheckItem{}).Where("tenant_id=? AND rule_id=?", tenantID, items[i].ID).Distinct("plan_id").Count(&items[i].PlanCount).Error; err != nil {
			return nil, 0, err
		}
	}
	return items, count, nil
}
func (r *RuleRepository) Get(ctx context.Context, tenantID, id int64) (*models.QualityRule, error) {
	var rule models.QualityRule
	if err := r.db.WithContext(ctx).Where("tenant_id=? AND id=?", tenantID, id).First(&rule).Error; err != nil {
		return nil, commonRepository.WrapDBError(err)
	}
	if err := r.db.WithContext(ctx).Model(&models.PlanCheckItem{}).Where("tenant_id=? AND rule_id=?", tenantID, id).Distinct("plan_id").Count(&rule.PlanCount).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}
func saveRuleRevision(tx *gorm.DB, rule *models.QualityRule) error {
	return tx.Create(&models.RuleRevision{TenantID: rule.TenantID, RuleID: rule.ID, RevisionNo: rule.RevisionNo, RuleContent: rule.RuleContent, CreatedAt: rule.UpdatedAt, CreatedBy: rule.UpdatedBy}).Error
}
func (r *RuleRepository) Create(ctx context.Context, rule *models.QualityRule) error {
	return commonRepository.WrapDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rule.Version = 1
		rule.RevisionNo = 1
		if err := tx.Create(rule).Error; err != nil {
			return err
		}
		return saveRuleRevision(tx, rule)
	}))
}
func (r *RuleRepository) Replace(ctx context.Context, rule *models.QualityRule, version int64) error {
	return commonRepository.WrapDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.QualityRule
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", rule.TenantID, rule.ID).First(&current).Error; err != nil {
			return err
		}
		if current.Version != version {
			return ErrVersionConflict
		}
		if current.Code != rule.Code {
			return fmt.Errorf("%w: rule code is immutable", commonAPI.ErrBadRequest)
		}
		current.RuleContent = rule.RuleContent
		current.Version++
		current.RevisionNo++
		current.UpdatedAt = time.Now().UTC()
		current.UpdatedBy = rule.UpdatedBy
		if err := tx.Save(&current).Error; err != nil {
			return err
		}
		if err := saveRuleRevision(tx, &current); err != nil {
			return err
		}
		*rule = current
		return nil
	}))
}
func (r *RuleRepository) Delete(ctx context.Context, tenantID, id, version int64) error {
	return commonRepository.WrapDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rule models.QualityRule
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", tenantID, id).First(&rule).Error; err != nil {
			return err
		}
		if rule.Version != version {
			return ErrVersionConflict
		}
		var count int64
		if err := tx.Model(&models.PlanCheckItem{}).Where("tenant_id=? AND rule_id=?", tenantID, id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrRuleReferenced
		}
		if err := tx.Where("tenant_id=? AND rule_id=?", tenantID, id).Delete(&models.RuleRevision{}).Error; err != nil {
			return err
		}
		return tx.Delete(&rule).Error
	}))
}

var ErrRuleReferenced = fmt.Errorf("%w: quality rule is referenced by plans", commonAPI.ErrConflict)

func (r *RuleRepository) Plans(ctx context.Context, tenantID, id int64, page, size int) ([]models.QualityPlan, int64, error) {
	var items []models.QualityPlan
	var total int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		items, total, err = NewRuleRepository(tx).plans(ctx, tenantID, id, page, size)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return items, total, commonRepository.WrapDBError(err)
}
func (r *RuleRepository) plans(ctx context.Context, tenantID, id int64, page, size int) ([]models.QualityPlan, int64, error) {
	if _, err := r.Get(ctx, tenantID, id); err != nil {
		return nil, 0, err
	}
	page, size = normalizePage(page, size)
	sub := r.db.Model(&models.PlanCheckItem{}).Select("plan_id").Where("tenant_id=? AND rule_id=?", tenantID, id)
	q := r.db.WithContext(ctx).Model(&models.QualityPlan{}).Where("tenant_id=? AND id IN (?)", tenantID, sub)
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	items := []models.QualityPlan{}
	if err := q.Order("id").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	for i := range items {
		if err := loadPlanItems(r.db.WithContext(ctx), &items[i]); err != nil {
			return nil, 0, err
		}
	}
	return items, count, nil
}

// Immutable revisions are loaded under owner SHARE locks before references are
// saved. Concurrent rule deletion cannot race a new plan reference.
func loadCheckRevisions(tx *gorm.DB, tenantID int64, items []models.PlanCheckItem, lock bool) error {
	ids := []int64{}
	seen := map[int64]bool{}
	for _, item := range items {
		if !seen[item.RuleID] {
			ids = append(ids, item.RuleID)
			seen[item.RuleID] = true
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) == 0 {
		return nil
	}
	latest := map[int64]int64{}
	var owners []models.QualityRule
	q := tx.Where("tenant_id=? AND id IN ?", tenantID, ids).Order("id")
	if lock {
		q = q.Clauses(clause.Locking{Strength: "SHARE"})
	}
	if err := q.Find(&owners).Error; err != nil {
		return err
	}
	if len(owners) != len(ids) {
		return gorm.ErrRecordNotFound
	}
	for _, owner := range owners {
		latest[owner.ID] = owner.RevisionNo
	}
	pairs := make([][]interface{}, 0, len(items))
	for _, item := range items {
		pairs = append(pairs, []interface{}{item.RuleID, item.RevisionNo})
	}
	var revisions []models.RuleRevision
	if err := tx.Where("tenant_id=? AND (rule_id,revision_no) IN ?", tenantID, pairs).Find(&revisions).Error; err != nil {
		return err
	}
	byKey := map[[2]int64]models.RuleRevision{}
	for _, rev := range revisions {
		rev.LatestRevisionNo = latest[rev.RuleID]
		byKey[[2]int64{rev.RuleID, rev.RevisionNo}] = rev
	}
	for i := range items {
		rev, ok := byKey[[2]int64{items[i].RuleID, items[i].RevisionNo}]
		if !ok {
			return gorm.ErrRecordNotFound
		}
		items[i].Rule = &rev
	}
	return nil
}
func (r *RuleRepository) Resolve(ctx context.Context, tenantID int64, items []models.PlanCheckItem) error {
	return loadCheckRevisions(r.db.WithContext(ctx), tenantID, items, false)
}
func loadPlanItems(tx *gorm.DB, plan *models.QualityPlan) error {
	plan.CheckItems = []models.PlanCheckItem{}
	if err := tx.Where("tenant_id=? AND plan_id=?", plan.TenantID, plan.ID).Order("position").Find(&plan.CheckItems).Error; err != nil {
		return err
	}
	if err := loadCheckRevisions(tx, plan.TenantID, plan.CheckItems, false); err != nil {
		return err
	}
	return plan.ResolveRules()
}
func replacePlanItems(tx *gorm.DB, plan *models.QualityPlan) error {
	if err := loadCheckRevisions(tx, plan.TenantID, plan.CheckItems, true); err != nil {
		return err
	}
	if err := tx.Where("tenant_id=? AND plan_id=?", plan.TenantID, plan.ID).Delete(&models.PlanCheckItem{}).Error; err != nil {
		return err
	}
	for i := range plan.CheckItems {
		plan.CheckItems[i].TenantID = plan.TenantID
		plan.CheckItems[i].PlanID = plan.ID
		plan.CheckItems[i].Position = i
	}
	if len(plan.CheckItems) > 0 {
		return tx.Create(&plan.CheckItems).Error
	}
	return nil
}
