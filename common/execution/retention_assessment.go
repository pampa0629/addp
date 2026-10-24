package execution

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const OverviewRetention = 180 * 24 * time.Hour

type RetentionReason string

const (
	RetentionOwnerUnverified     RetentionReason = "owner_release_unverified"
	RetentionConsumersUnverified RetentionReason = "consumer_references_unverified"
	RetentionActive              RetentionReason = "active_execution"
	RetentionUnknownStatus       RetentionReason = "status_unknown"
	RetentionMissingCompletion   RetentionReason = "completion_missing"
	RetentionNotExpired          RetentionReason = "not_expired"
	RetentionActiveLease         RetentionReason = "lease_active"
	RetentionRelationship        RetentionReason = "relationship_anomaly"
	RetentionExecutionReference  RetentionReason = "execution_reference"
	RetentionTreeBudget          RetentionReason = "tree_budget_exceeded"
	RetentionReadBudget          RetentionReason = "read_budget_exceeded"
)

// RetentionAssessmentOptions bounds one maintenance observation, not a deletion.
// Zero limits use defaults; callers may lower, but never raise, these budgets.
type RetentionAssessmentOptions struct {
	AfterID    int64
	SeedLimit  int
	TreeLimit  int
	ReadLimit  int
	QueryLimit int
}

type RetentionTreeAssessment struct {
	TenantID          int               `json:"tenant_id"`
	AnchorExecutionID string            `json:"anchor_execution_id"`
	ObservedMembers   int               `json:"observed_members"`
	Complete          bool              `json:"complete"`
	AgeEligible       bool              `json:"age_eligible"`
	Reasons           []RetentionReason `json:"reasons"`
}

// RetentionAssessment is internal maintenance evidence, never a User read scope
// or permission to delete. Trees may overlap between successive seed pages.
type RetentionAssessment struct {
	ReadOnly         bool                      `json:"read_only"`
	AsOf             time.Time                 `json:"as_of"`
	ExpiredBefore    time.Time                 `json:"expired_before"`
	NextAfterID      int64                     `json:"next_after_id"`
	HasMore          bool                      `json:"has_more"`
	SeedsEvaluated   int                       `json:"seeds_evaluated"`
	RowsRead         int                       `json:"rows_read"`
	Queries          int                       `json:"queries"`
	CompleteTrees    int                       `json:"complete_trees"`
	AgeEligibleTrees int                       `json:"age_eligible_trees"`
	BudgetExhausted  bool                      `json:"budget_exhausted"`
	ReasonCounts     map[RetentionReason]int   `json:"reason_counts"`
	Trees            []RetentionTreeAssessment `json:"trees"`
}

// Deliberately excludes config, results, errors, actor and authorization facts.
type retentionRow struct {
	ID                 int64
	TenantID           int
	ExecutionID        string
	ParentExecutionID  *string
	RetryOfExecutionID *string
	RecoveredFrom      *string
	Status             string
	CompletedAt        *time.Time
	LeaseExpiresAt     *time.Time
}

const retentionColumns = `id, tenant_id, execution_id, parent_execution_id,
 retry_of_execution_id, CASE
 WHEN jsonb_typeof(metadata -> 'recovered_from_execution_id') = 'string'
 AND octet_length(metadata ->> 'recovered_from_execution_id') <= 255
 THEN metadata ->> 'recovered_from_execution_id'
 WHEN metadata ? 'recovered_from_execution_id' THEN '' END AS recovered_from,
 status, completed_at, lease_expires_at`

var errRetentionReadBudget = errors.New("execution retention assessment read budget exhausted")

type retentionReader struct {
	db      *gorm.DB
	options RetentionAssessmentOptions
	rows    int
	queries int
}

func (r *retentionReader) read(query *gorm.DB, limit int) ([]retentionRow, error) {
	remaining := r.options.ReadLimit - r.rows
	if remaining <= 0 || r.queries >= r.options.QueryLimit {
		return nil, errRetentionReadBudget
	}
	if limit > remaining {
		limit = remaining
	}
	var rows []retentionRow
	err := query.Select(retentionColumns).Order("id").Limit(limit).Scan(&rows).Error
	r.queries++
	r.rows += len(rows)
	return rows, err
}

func (o RetentionAssessmentOptions) normalized() (RetentionAssessmentOptions, error) {
	if o.AfterID < 0 {
		return o, fmt.Errorf("execution retention assessment cursor must be nonnegative")
	}
	limits := []struct {
		value   *int
		maximum int
	}{
		{&o.SeedLimit, 100}, {&o.TreeLimit, 1000}, {&o.ReadLimit, 10000}, {&o.QueryLimit, 500},
	}
	for _, limit := range limits {
		if *limit.value == 0 {
			*limit.value = limit.maximum
		}
		if *limit.value < 1 || *limit.value > limit.maximum {
			return o, fmt.Errorf("invalid execution retention assessment budget")
		}
	}
	return o, nil
}

// AssessRetention reads one bounded, consistent PostgreSQL snapshot. There is
// intentionally no Owner callback or deletion route in this first phase.
func AssessRetention(ctx context.Context, db *gorm.DB, options RetentionAssessmentOptions) (*RetentionAssessment, error) {
	options, err := options.normalized()
	if err != nil {
		return nil, err
	}
	if db == nil || db.Dialector.Name() != "postgres" {
		return nil, fmt.Errorf("execution retention assessment requires PostgreSQL")
	}
	// GORM nested transactions ignore TxOptions. Never turn an inherited writable
	// transaction into a report that falsely claims database-enforced read-only.
	if _, nested := db.Statement.ConnPool.(gorm.TxCommitter); nested {
		return nil, fmt.Errorf("execution retention assessment requires an independent read-only transaction")
	}
	var report *RetentionAssessment
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var now time.Time
		if err := tx.Raw("SELECT transaction_timestamp()").Scan(&now).Error; err != nil {
			return err
		}
		r := &retentionReader{db: tx, options: options}
		report = &RetentionAssessment{ReadOnly: true, AsOf: now.UTC(), ExpiredBefore: now.UTC().Add(-OverviewRetention),
			NextAfterID: options.AfterID, ReasonCounts: map[RetentionReason]int{}, Trees: []RetentionTreeAssessment{}}
		seeds, err := r.read(tx.Table("common.task_executions").Where("id > ?", options.AfterID), options.SeedLimit+1)
		if err != nil {
			return err
		}
		report.HasMore = len(seeds) > options.SeedLimit || r.rows == options.ReadLimit
		if len(seeds) > options.SeedLimit {
			seeds = seeds[:options.SeedLimit]
		}
		seen := map[string]bool{}
		groups := map[string]int{}
		for index, seed := range seeds {
			if r.rows >= options.ReadLimit || r.queries >= options.QueryLimit {
				report.HasMore, report.BudgetExhausted = true, true
				break
			}
			report.NextAfterID = seed.ID
			report.SeedsEvaluated++
			if seen[seed.ExecutionID] {
				continue
			}
			tree, members, err := r.tree(seed, report.AsOf)
			if err != nil {
				return err
			}
			for id := range members {
				seen[id] = true
				groups[id] = len(report.Trees)
			}
			report.Trees = append(report.Trees, tree)
			if r.rows >= options.ReadLimit || r.queries >= options.QueryLimit {
				report.HasMore = report.HasMore || index+1 < len(seeds)
				report.BudgetExhausted = true
			}
		}
		// One bounded reverse-reference scan per batch, never one full JSON scan
		// per tree. Unchecked domain references remain explicitly unverified.
		if err := r.incomingReferences(report, groups); err != nil {
			return err
		}
		for _, tree := range report.Trees {
			if tree.Complete {
				report.CompleteTrees++
			}
			if tree.AgeEligible {
				report.AgeEligibleTrees++
			}
			for _, reason := range tree.Reasons {
				report.ReasonCounts[reason]++
				if reason == RetentionTreeBudget || reason == RetentionReadBudget {
					report.BudgetExhausted = true
				}
			}
		}
		report.RowsRead, report.Queries = r.rows, r.queries
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return report, nil
}

func (r *retentionReader) tree(seed retentionRow, now time.Time) (RetentionTreeAssessment, map[string]retentionRow, error) {
	tree := RetentionTreeAssessment{TenantID: seed.TenantID, AnchorExecutionID: seed.ExecutionID, Complete: true}
	members := map[string]retentionRow{seed.ExecutionID: seed}
	reasons := map[RetentionReason]bool{RetentionOwnerUnverified: true, RetentionConsumersUnverified: true}
	frontier := []retentionRow{seed}
	for len(frontier) > 0 {
		ids, parents := []string{}, []string{}
		for _, row := range frontier {
			ids = append(ids, row.ExecutionID)
			if row.ParentExecutionID != nil && members[*row.ParentExecutionID].ExecutionID == "" {
				parents = append(parents, *row.ParentExecutionID)
			}
		}
		known := retentionIDs(members)
		query := r.db.Table("common.task_executions").Where("(parent_execution_id IN ? OR execution_id IN ?) AND execution_id NOT IN ?", ids, parents, known)
		rows, err := r.read(query, r.options.TreeLimit-len(members)+1)
		if errors.Is(err, errRetentionReadBudget) {
			reasons[RetentionReadBudget], tree.Complete = true, false
			break
		}
		if err != nil {
			return tree, members, err
		}
		frontier = nil
		for _, row := range rows {
			if row.TenantID != seed.TenantID {
				reasons[RetentionRelationship] = true
				continue
			}
			if len(members) == r.options.TreeLimit {
				reasons[RetentionTreeBudget], tree.Complete = true, false
				break
			}
			members[row.ExecutionID] = row
			frontier = append(frontier, row)
		}
		if !tree.Complete {
			break
		}
		if r.rows >= r.options.ReadLimit {
			reasons[RetentionReadBudget], tree.Complete = true, false
			break
		}
	}
	if tree.Complete {
		if err := r.references(members, seed.TenantID, reasons); errors.Is(err, errRetentionReadBudget) {
			reasons[RetentionReadBudget], tree.Complete = true, false
		} else if err != nil {
			return tree, members, err
		}
	}
	assessRetentionMembers(&tree, members, now, reasons)
	return tree, members, nil
}

func retentionIDs(members map[string]retentionRow) []string {
	ids := make([]string, 0, len(members))
	for id := range members {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func canonicalExecutionID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed != uuid.Nil && parsed.String() == id
}

func (r *retentionReader) references(members map[string]retentionRow, tenantID int, reasons map[RetentionReason]bool) error {
	targets := []string{}
	for _, row := range members {
		for _, ref := range []*string{row.RetryOfExecutionID, row.RecoveredFrom} {
			if ref == nil {
				continue
			}
			if !canonicalExecutionID(*ref) || *ref == row.ExecutionID {
				reasons[RetentionRelationship] = true
			} else if _, exists := members[*ref]; !exists {
				targets = append(targets, *ref)
			}
		}
	}
	if len(targets) > 0 {
		rows, err := r.read(r.db.Table("common.task_executions").Where("execution_id IN ?", targets), len(targets))
		if err != nil {
			return err
		}
		if r.rows >= r.options.ReadLimit {
			return errRetentionReadBudget
		}
		found := map[string]bool{}
		for _, row := range rows {
			found[row.ExecutionID] = row.TenantID == tenantID
		}
		for _, target := range targets {
			if !found[target] {
				reasons[RetentionRelationship] = true
			}
		}
	}
	return nil
}

func addRetentionReason(tree *RetentionTreeAssessment, reason RetentionReason) {
	for _, existing := range tree.Reasons {
		if existing == reason {
			return
		}
	}
	tree.Reasons = append(tree.Reasons, reason)
	sort.Slice(tree.Reasons, func(i, j int) bool { return tree.Reasons[i] < tree.Reasons[j] })
}

func (r *retentionReader) incomingReferences(report *RetentionAssessment, groups map[string]int) error {
	if len(groups) == 0 {
		return nil
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	remaining := r.options.ReadLimit - r.rows
	rows, err := r.read(r.db.Table("common.task_executions").Where("retry_of_execution_id IN ? OR metadata ->> 'recovered_from_execution_id' IN ?", ids, ids), remaining)
	if err != nil && !errors.Is(err, errRetentionReadBudget) {
		return err
	}
	if errors.Is(err, errRetentionReadBudget) || r.rows >= r.options.ReadLimit {
		report.BudgetExhausted = true
		for i := range report.Trees {
			addRetentionReason(&report.Trees[i], RetentionReadBudget)
			report.Trees[i].Complete = false
			report.Trees[i].AgeEligible = false
		}
	}
	for _, row := range rows {
		for _, ref := range []*string{row.RetryOfExecutionID, row.RecoveredFrom} {
			if ref == nil {
				continue
			}
			if index, exists := groups[*ref]; exists {
				tree := &report.Trees[index]
				if own, sameTree := groups[row.ExecutionID]; !sameTree || own != index {
					addRetentionReason(tree, RetentionExecutionReference)
				}
				if row.TenantID != tree.TenantID {
					addRetentionReason(tree, RetentionRelationship)
					tree.AgeEligible = false
				}
			}
		}
	}
	return nil
}

func assessRetentionMembers(tree *RetentionTreeAssessment, members map[string]retentionRow, now time.Time, reasons map[RetentionReason]bool) {
	cutoff, roots := now.Add(-OverviewRetention), 0
	for _, row := range members {
		if row.TenantID <= 0 || !canonicalExecutionID(row.ExecutionID) {
			reasons[RetentionRelationship] = true
		}
		if row.ParentExecutionID == nil {
			roots++
		} else if !canonicalExecutionID(*row.ParentExecutionID) || *row.ParentExecutionID == row.ExecutionID || (tree.Complete && members[*row.ParentExecutionID].ExecutionID == "") {
			reasons[RetentionRelationship] = true
		}
		switch row.Status {
		case ExecutionStatusSuccess, ExecutionStatusFailed, ExecutionStatusTimeout, ExecutionStatusCancelled:
		case ExecutionStatusPending, ExecutionStatusRunning:
			reasons[RetentionActive] = true
		default:
			reasons[RetentionUnknownStatus] = true
		}
		if row.CompletedAt == nil {
			reasons[RetentionMissingCompletion] = true
		} else if !row.CompletedAt.Before(cutoff) {
			reasons[RetentionNotExpired] = true
		}
		if row.LeaseExpiresAt != nil && row.LeaseExpiresAt.After(now) {
			reasons[RetentionActiveLease] = true
		}
	}
	if tree.Complete && roots != 1 {
		reasons[RetentionRelationship] = true
	}
	tree.ObservedMembers = len(members)
	tree.AgeEligible = tree.Complete
	for reason := range reasons {
		tree.Reasons = append(tree.Reasons, reason)
		if reason != RetentionOwnerUnverified && reason != RetentionConsumersUnverified && reason != RetentionExecutionReference {
			tree.AgeEligible = false
		}
	}
	sort.Slice(tree.Reasons, func(i, j int) bool { return tree.Reasons[i] < tree.Reasons[j] })
}
