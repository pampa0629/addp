package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/query"
	"github.com/addp/common/resourcetree"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
)

// Counts, key validation and failure identities all use the execution's single
// repeatable-read transaction. No sample or count-only acceptance is possible.
func collectFailureEvidence(ctx context.Context, db *gorm.DB, rule planCompiledRule, binding PlanTableBinding, aliases map[string]validationReadItem, counts planCounts, validity map[string]string) (*models.FailureEvidence, error) {
	e := &models.FailureEvidence{}
	if rule.RowCount != nil {
		e.Reason = "aggregate_rule"
		return e, nil
	}
	if len(binding.RecordKey) == 0 {
		e.Reason = "key_not_configured"
		return e, nil
	}
	if counts.FailedCount > models.MaxFailureKeys {
		e.Reason = "too_many_failures"
		return e, nil
	}
	dialect := query.ForDialect(query.DialectPostgreSQL)
	quoted, qualified, nulls := []string{}, []string{}, []string{}
	for _, key := range binding.RecordKey {
		q := dialect.QuoteIdentifier(key)
		quoted = append(quoted, q)
		qualified = append(qualified, "pg_typeof("+rule.Rows.Qualifier+q+")::text", rule.Rows.Qualifier+q)
		nulls = append(nulls, q+" IS NULL")
	}
	reason, checked := validity[binding.Alias]
	if !checked {
		locator, err := resourcetree.ParseURI(aliases[binding.Alias].Locator)
		if err != nil {
			return nil, err
		}
		table := dialect.QualifiedTable(locator.Path[0], locator.Path[1])
		var invalid bool
		err = db.WithContext(ctx).Raw(fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM %s WHERE %s) OR EXISTS (SELECT 1 FROM %s GROUP BY %s HAVING COUNT(*) > 1)", table, strings.Join(nulls, " OR "), table, strings.Join(quoted, ", "))).Scan(&invalid).Error
		if err != nil {
			return nil, err
		}
		if invalid {
			reason = "key_not_unique_or_null"
		}
		validity[binding.Alias] = reason
	}
	if reason != "" {
		e.Reason = reason
		return e, nil
	}
	scopeJSON, err := json.Marshal(struct {
		Rule      PlanRule
		RecordKey []string
	}{rule.Rule, binding.RecordKey})
	if err != nil {
		return nil, err
	}
	scope := sha256.Sum256(scopeJSON)
	e.Scope = hex.EncodeToString(scope[:])
	if counts.FailedCount == 0 {
		e.Keys = []string{}
		return e, nil
	}
	// Typed JSON arrays avoid delimiter ambiguity and include each field's type.
	identity := "encode(sha256(convert_to(jsonb_build_array(" + strings.Join(qualified, ", ") + ")::text, 'UTF8')), 'hex')"
	err = db.WithContext(ctx).Raw(fmt.Sprintf("SELECT %s FROM %s WHERE (%s) IS TRUE LIMIT %d", identity, rule.Rows.From, rule.Rows.Failure, models.MaxFailureKeys+1), rule.Args...).Scan(&e.Keys).Error
	if err != nil {
		return nil, err
	}
	if int64(len(e.Keys)) != counts.FailedCount {
		return nil, fmt.Errorf("failure identity count differs from complete rule count")
	}
	sort.Strings(e.Keys)
	for i := 1; i < len(e.Keys); i++ {
		if e.Keys[i] == e.Keys[i-1] {
			return nil, fmt.Errorf("duplicate failure identity")
		}
	}
	return e, nil
}
