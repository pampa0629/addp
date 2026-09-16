package service

import (
	"context"
	"database/sql"
	"fmt"
	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataquality"
	"github.com/addp/common/dbbridge"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/query"
	"github.com/addp/common/resourcetree"
	"gorm.io/gorm"
	"strconv"
	"strings"
)

// executePostgreSQLPlan is the first engine adapter. Authorization and execution
// lifecycle stay in the worker; physical paths, SQL and snapshots belong here.
func executePostgreSQLPlan(ctx context.Context, engine *commonModels.Engine, config *planExecutionConfig) (*PlanResult, error) {
	if engine == nil || !strings.EqualFold(engine.EngineType, "postgresql") {
		return nil, failExecution(planUnsupportedEngineCode, fmt.Errorf("quality plan only supports PostgreSQL"))
	}
	targetDB, err := dbbridge.GetOrCreatePool(engine, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, failExecution(planSQLFailedCode, err)
	}
	var result *PlanResult
	err = targetDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var runErr error
		result, runErr = runPlan(ctx, tx, config)
		return runErr
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}

type planCompiledRule struct {
	Rule     PlanRule
	SQL      string
	Args     []interface{}
	RowCount *planRowCountParams
}

type planCounts struct {
	TotalCount  int64 `gorm:"column:total_count"`
	FailedCount int64 `gorm:"column:failed_count"`
}

func runPlan(ctx context.Context, targetDB *gorm.DB, config *planExecutionConfig) (*PlanResult, error) {
	readContext, err := readValidationTables(targetDB.WithContext(ctx), config.TableBindings)
	if err != nil {
		return nil, failExecution(planReadContextFailedCode, err)
	}
	compiled, aliases, err := compilePlan(config, readContext)
	if err != nil {
		return nil, failExecution(planCompileFailedCode, err)
	}
	_ = aliases
	result := &PlanResult{Rules: make([]PlanRuleResult, 0, len(compiled)), Passed: true}
	for _, item := range compiled {
		var counts planCounts
		if err := targetDB.WithContext(ctx).Raw(item.SQL, item.Args...).Scan(&counts).Error; err != nil {
			return nil, failExecution(planSQLFailedCode, fmt.Errorf("execute rule %s: %w", item.Rule.RuleKey, err))
		}
		if counts.TotalCount < 0 || counts.FailedCount < 0 || counts.FailedCount > counts.TotalCount {
			return nil, failExecution(planResultInvalidCode, fmt.Errorf("rule %s returned invalid counts", item.Rule.RuleKey))
		}
		passed := counts.FailedCount == 0
		observed := map[string]interface{}{"total_count": counts.TotalCount}
		if item.RowCount != nil {
			observed["row_count"] = counts.TotalCount
			passed = planRowCountPassed(counts.TotalCount, *item.RowCount)
			if !passed {
				counts.FailedCount = 1
			}
		}
		result.Rules = append(result.Rules, PlanRuleResult{RuleKey: item.Rule.RuleKey, RuleID: item.Rule.RuleID, RevisionNo: item.Rule.RevisionNo, Name: item.Rule.Name, TotalCount: counts.TotalCount, Table: ruleTarget(item.Rule).Table, Columns: ruleTarget(item.Rule).Columns, Type: item.Rule.Type, Severity: item.Rule.Severity, Passed: passed, FailedCount: counts.FailedCount, Observed: observed})
		if !passed && item.Rule.Severity == "error" {
			result.Passed = false
		}
	}
	if !result.Passed {
		return result, failExecution(planRuleFailedCode, fmt.Errorf("one or more error rules failed"))
	}
	return result, nil
}

func compilePlan(config *planExecutionConfig, readContext *validationReadContext) ([]planCompiledRule, map[string]validationReadItem, error) {
	if len(readContext.Items) != len(config.TableBindings) {
		return nil, nil, fmt.Errorf("physical table context does not match bindings")
	}
	aliases := make(map[string]validationReadItem, len(config.TableBindings))
	for index, binding := range config.TableBindings {
		item := readContext.Items[index]
		if item.Locator != binding.Locator {
			return nil, nil, fmt.Errorf("physical table context order changed")
		}
		aliases[binding.Alias] = item
	}
	compiled := make([]planCompiledRule, 0, len(config.Rules.Rules))
	for _, rule := range config.Rules.Rules {
		if rule.Disabled {
			continue
		}
		item, err := compilePlanRule(rule, aliases)
		if err != nil {
			return nil, nil, fmt.Errorf("rule %s: %w", rule.RuleKey, err)
		}
		compiled = append(compiled, item)
	}
	if len(compiled) == 0 {
		return nil, nil, fmt.Errorf("plan has no enabled rules")
	}
	return compiled, aliases, nil
}

func compilePlanRule(rule PlanRule, aliases map[string]validationReadItem) (planCompiledRule, error) {
	dialect := query.ForDialect(query.DialectPostgreSQL)
	tableSQL := func(alias string) (string, map[string]struct{}, error) {
		item, exists := aliases[alias]
		if !exists {
			return "", nil, fmt.Errorf("table alias is not bound")
		}
		locator, err := resourcetree.ParseURI(item.Locator)
		if err != nil || locator.Type != resourcetree.TypeTable || len(locator.Path) != 2 || int64(locator.EngineID) != item.EngineID {
			return "", nil, fmt.Errorf("table locator is invalid")
		}
		columns := make(map[string]struct{}, len(item.Columns))
		for _, column := range item.Columns {
			columns[column.Name] = struct{}{}
		}
		return dialect.QualifiedTable(locator.Path[0], locator.Path[1]), columns, nil
	}
	columnSQL := func(columns map[string]struct{}, column string) (string, error) {
		if _, exists := columns[column]; !exists {
			return "", fmt.Errorf("column %q is not present in physical table", column)
		}
		return dialect.QuoteIdentifier(column), nil
	}
	compiled := planCompiledRule{Rule: rule}
	switch rule.Type {
	case "format", "length", "value_range":
		var params planValueParams
		if err := decodeStrictJSON(rule.Params, &params); err != nil {
			return compiled, err
		}
		_, columns, err := tableSQL(params.Table)
		if err != nil {
			return compiled, err
		}
		if _, err = columnSQL(columns, params.Column); err != nil {
			return compiled, err
		}
		locator, _ := resourcetree.ParseURI(aliases[params.Table].Locator)
		scalar, err := NewSQLGenerator().GenerateCheckSQL(locator.Path[0], locator.Path[1], params.Column, dataquality.Rule{RuleKey: rule.RuleKey, Type: rule.Type, Enabled: true, Severity: rule.Severity, Params: params.Constraint})
		if err != nil {
			return compiled, err
		}
		compiled.SQL = scalar.SQL
		compiled.Args = scalar.Args
	case "not_null":
		var params planNotNullParams
		if err := decodeStrictJSON(rule.Params, &params); err != nil {
			return compiled, err
		}
		table, columns, err := tableSQL(params.Table)
		if err != nil {
			return compiled, err
		}
		column, err := columnSQL(columns, params.Column)
		if err != nil {
			return compiled, err
		}
		compiled.SQL = fmt.Sprintf("SELECT COUNT(*) AS total_count, COUNT(*) FILTER (WHERE %s IS NULL) AS failed_count FROM %s", column, table)
	case "allowed_values":
		var params planAllowedValuesParams
		if err := decodeStrictJSON(rule.Params, &params); err != nil {
			return compiled, err
		}
		table, columns, err := tableSQL(params.Table)
		if err != nil {
			return compiled, err
		}
		column, err := columnSQL(columns, params.Column)
		if err != nil {
			return compiled, err
		}
		placeholders := make([]string, len(params.Values))
		compiled.Args = make([]interface{}, len(params.Values))
		for index, value := range params.Values {
			placeholders[index] = "$" + strconv.Itoa(index+1)
			compiled.Args[index] = value
		}
		compiled.SQL = fmt.Sprintf("SELECT COUNT(*) AS total_count, COUNT(*) FILTER (WHERE %s IS NOT NULL AND %s::text NOT IN (%s)) AS failed_count FROM %s", column, column, strings.Join(placeholders, ", "), table)
	case "unique_key":
		var params planUniqueKeyParams
		if err := decodeStrictJSON(rule.Params, &params); err != nil {
			return compiled, err
		}
		table, columns, err := tableSQL(params.Table)
		if err != nil {
			return compiled, err
		}
		quoted := make([]string, len(params.Columns))
		for i, name := range params.Columns {
			quoted[i], err = columnSQL(columns, name)
			if err != nil {
				return compiled, err
			}
		}
		nonNull := make([]string, len(quoted))
		for i, name := range quoted {
			nonNull[i] = name + " IS NOT NULL"
		}
		compiled.SQL = fmt.Sprintf("SELECT (SELECT COUNT(*) FROM %s) AS total_count, (SELECT COALESCE(SUM(duplicate_count),0) FROM (SELECT COUNT(*) AS duplicate_count FROM %s WHERE %s GROUP BY %s HAVING COUNT(*) > 1) AS duplicate_groups) AS failed_count", table, table, strings.Join(nonNull, " AND "), strings.Join(quoted, ", "))
	case "foreign_key":
		var params planForeignKeyParams
		if err := decodeStrictJSON(rule.Params, &params); err != nil {
			return compiled, err
		}
		child, childColumns, err := tableSQL(params.Table)
		if err != nil {
			return compiled, err
		}
		parent, parentColumns, err := tableSQL(params.ReferenceTable)
		if err != nil {
			return compiled, err
		}
		eligible, matches := make([]string, len(params.Columns)), make([]string, len(params.Columns))
		for i := range params.Columns {
			childColumn, err := columnSQL(childColumns, params.Columns[i])
			if err != nil {
				return compiled, err
			}
			parentColumn, err := columnSQL(parentColumns, params.ReferenceColumns[i])
			if err != nil {
				return compiled, err
			}
			eligible[i] = "child." + childColumn + " IS NOT NULL"
			matches[i] = "parent." + parentColumn + " = child." + childColumn
		}
		compiled.SQL = fmt.Sprintf("SELECT COUNT(*) AS total_count, COUNT(*) FILTER (WHERE %s AND NOT EXISTS (SELECT 1 FROM %s AS parent WHERE %s)) AS failed_count FROM %s AS child", strings.Join(eligible, " AND "), parent, strings.Join(matches, " AND "), child)
	case "predicate_implication":
		var params planPredicateImplicationParams
		if err := decodeStrictJSON(rule.Params, &params); err != nil {
			return compiled, err
		}
		table, columns, err := tableSQL(params.Table)
		if err != nil {
			return compiled, err
		}
		whenSQL, whenArgs, err := compilePlanCondition(params.When, columns, dialect, 1)
		if err != nil {
			return compiled, err
		}
		thenSQL, thenArgs, err := compilePlanCondition(params.Then, columns, dialect, len(whenArgs)+1)
		if err != nil {
			return compiled, err
		}
		compiled.SQL = fmt.Sprintf("SELECT COUNT(*) AS total_count, COUNT(*) FILTER (WHERE (%s) IS TRUE AND NOT ((%s) IS TRUE)) AS failed_count FROM %s", whenSQL, thenSQL, table)
		compiled.Args = append(whenArgs, thenArgs...)
	case "row_count":
		var params planRowCountParams
		if err := decodeStrictJSON(rule.Params, &params); err != nil {
			return compiled, err
		}
		table, _, err := tableSQL(params.Table)
		if err != nil {
			return compiled, err
		}
		compiled.SQL = fmt.Sprintf("SELECT COUNT(*) AS total_count, 0::bigint AS failed_count FROM %s", table)
		compiled.RowCount = &params
	default:
		return compiled, fmt.Errorf("unsupported rule type")
	}
	return compiled, nil
}

func compilePlanCondition(condition planCondition, columns map[string]struct{}, dialect query.Dialect, firstParameter int) (string, []interface{}, error) {
	if _, exists := columns[condition.Column]; !exists {
		return "", nil, fmt.Errorf("condition column is not present in physical table")
	}
	column := dialect.QuoteIdentifier(condition.Column)
	switch condition.Operator {
	case "eq":
		return column + " = $" + strconv.Itoa(firstParameter), []interface{}{condition.Value}, nil
	case "not_eq":
		return column + " <> $" + strconv.Itoa(firstParameter), []interface{}{condition.Value}, nil
	case "is_null":
		return column + " IS NULL", nil, nil
	case "is_not_null":
		return column + " IS NOT NULL", nil, nil
	case "is_true":
		return column + " IS TRUE", nil, nil
	case "is_false":
		return column + " IS FALSE", nil, nil
	default:
		return "", nil, fmt.Errorf("condition op is unsupported")
	}
}

func planRowCountPassed(count int64, params planRowCountParams) bool {
	if params.Exact != nil {
		return count == *params.Exact
	}
	if params.Min != nil && count < *params.Min {
		return false
	}
	if params.Max != nil && count > *params.Max {
		return false
	}
	return true
}

// Validation consumes only explicit, same-engine physical resource identities.
type validationColumn struct{ Name string }
type validationReadItem struct {
	Locator  string
	EngineID int64
	Columns  []validationColumn
}
type validationReadContext struct{ Items []validationReadItem }

func readValidationTables(db *gorm.DB, bindings []PlanTableBinding) (*validationReadContext, error) {
	result := &validationReadContext{Items: make([]validationReadItem, 0, len(bindings))}
	for _, b := range bindings {
		locator, err := resourcetree.ParseURI(b.Locator)
		if err != nil || len(locator.Path) != 2 {
			return nil, fmt.Errorf("invalid PostgreSQL table locator")
		}
		item := validationReadItem{Locator: b.Locator, EngineID: int64(locator.EngineID)}
		if err := db.Raw("SELECT column_name AS name FROM information_schema.columns WHERE table_schema=? AND table_name=? ORDER BY ordinal_position", locator.Path[0], locator.Path[1]).Scan(&item.Columns).Error; err != nil {
			return nil, err
		}
		if len(item.Columns) == 0 {
			return nil, fmt.Errorf("table %s is missing or unreadable", b.Alias)
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *PlanService) validatePostgreSQLTargets(ctx context.Context, tenantID int64, bindings []PlanTableBinding, document *PlanRuleDocument) error {
	if s.systemClient == nil {
		return fmt.Errorf("engine resolver unavailable")
	}
	readContext := &validationReadContext{}
	for _, binding := range bindings {
		locator, err := resourcetree.ParseURI(binding.Locator)
		if err != nil || len(locator.Path) != 2 {
			return fmt.Errorf("%w: PostgreSQL table requires schema and table", commonAPI.ErrBadRequest)
		}
		if err := requirePostgreSQLEngine(ctx, s.systemClient, tenantID, int64(locator.EngineID)); err != nil {
			return err
		}
		table, err := requirePostgreSQLCatalogTable(ctx, s.systemClient, tenantID, int64(locator.EngineID), locator.Path[0], locator.Path[1])
		if err != nil {
			return err
		}
		facts, err := s.systemClient.WithTenantID(uint(tenantID)).DescribeEngineCatalogFacts(ctx, locator.EngineID, commonClient.EngineCatalogDescribeFactsRequest{Path: table.Path})
		if err != nil {
			return err
		}
		if facts == nil || facts.Table == nil {
			return fmt.Errorf("%w: table fields unavailable", commonAPI.ErrBadRequest)
		}
		item := validationReadItem{Locator: binding.Locator, EngineID: int64(locator.EngineID)}
		for _, field := range facts.Table.Fields {
			item.Columns = append(item.Columns, validationColumn{Name: field.Name})
		}
		readContext.Items = append(readContext.Items, item)
	}
	// Compile all rules, including disabled ones, to reject stale physical fields.
	all := *document
	all.Rules = append([]PlanRule(nil), document.Rules...)
	for i := range all.Rules {
		all.Rules[i].Disabled = false
	}
	if _, _, err := compilePlan(&planExecutionConfig{TableBindings: bindings, Rules: all}, readContext); err != nil {
		return fmt.Errorf("%w: %v", commonAPI.ErrBadRequest, err)
	}
	return nil
}
