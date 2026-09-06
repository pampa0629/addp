package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/addp/common/dataquality"
	"github.com/addp/common/query"
)

// SQLGenerator compiles the PostgreSQL-only quality rule contract.
type SQLGenerator struct {
	dialect query.Dialect
}

type CompiledCheck struct {
	SQL  string
	Args []interface{}
}

type CheckCounts struct {
	TotalCount  int64 `gorm:"column:total_count"`
	FailedCount int64 `gorm:"column:failed_count"`
}

func NewSQLGenerator() *SQLGenerator {
	return &SQLGenerator{dialect: query.ForDialect(query.DialectPostgreSQL)}
}

// GenerateCheckSQL produces one aggregate query for a rule. Identifiers are
// quoted by the dialect and every user value is returned separately for binding.
func (g *SQLGenerator) GenerateCheckSQL(schemaName, tableName, columnName string, rule dataquality.Rule) (CompiledCheck, error) {
	if strings.TrimSpace(schemaName) == "" || strings.TrimSpace(tableName) == "" || strings.TrimSpace(columnName) == "" {
		return CompiledCheck{}, fmt.Errorf("schema, table and column are required")
	}
	if err := rule.Validate(); err != nil {
		return CompiledCheck{}, err
	}
	table := g.dialect.QualifiedTable(schemaName, tableName)
	column := g.dialect.QuoteIdentifier(columnName)

	if rule.Type == dataquality.RuleTypeUnique {
		return CompiledCheck{
			SQL: fmt.Sprintf(`SELECT COUNT(*) AS total_count,
COALESCE(SUM(CASE WHEN %s IS NOT NULL AND duplicate_count > 1 THEN 1 ELSE 0 END), 0) AS failed_count
FROM (SELECT %s, COUNT(*) OVER (PARTITION BY %s) AS duplicate_count FROM %s) AS quality_rows`, column, column, column, table),
		}, nil
	}

	condition, args, err := g.failureCondition(column, rule)
	if err != nil {
		return CompiledCheck{}, err
	}
	return CompiledCheck{
		SQL:  fmt.Sprintf("SELECT COUNT(*) AS total_count, COUNT(*) FILTER (WHERE %s) AS failed_count FROM %s", condition, table),
		Args: args,
	}, nil
}

func (g *SQLGenerator) failureCondition(column string, rule dataquality.Rule) (string, []interface{}, error) {
	params := rule.Params
	switch rule.Type {
	case dataquality.RuleTypeNotNull:
		return column + " IS NULL", nil, nil
	case dataquality.RuleTypeFormat:
		return column + " IS NOT NULL AND " + column + "::text !~ $1", []interface{}{*params.Pattern}, nil
	case dataquality.RuleTypeLength:
		conditions := make([]string, 0, 2)
		args := make([]interface{}, 0, 2)
		if params.Min != nil {
			conditions = append(conditions, column+" IS NOT NULL AND char_length("+column+"::text) < $"+strconv.Itoa(len(args)+1))
			args = append(args, lengthArgument(*params.Min))
		}
		if params.Max != nil {
			conditions = append(conditions, column+" IS NOT NULL AND char_length("+column+"::text) > $"+strconv.Itoa(len(args)+1))
			args = append(args, lengthArgument(*params.Max))
		}
		return "(" + strings.Join(conditions, " OR ") + ")", args, nil
	case dataquality.RuleTypeValueRange:
		conditions := make([]string, 0, 2)
		args := make([]interface{}, 0, 2)
		if params.Min != nil {
			operator := "<"
			if params.MinInclusive != nil && !*params.MinInclusive {
				operator = "<="
			}
			conditions = append(conditions, column+" IS NOT NULL AND "+column+"::numeric "+operator+" $"+strconv.Itoa(len(args)+1))
			args = append(args, params.Min.String())
		}
		if params.Max != nil {
			operator := ">"
			if params.MaxInclusive != nil && !*params.MaxInclusive {
				operator = ">="
			}
			conditions = append(conditions, column+" IS NOT NULL AND "+column+"::numeric "+operator+" $"+strconv.Itoa(len(args)+1))
			args = append(args, params.Max.String())
		}
		return "(" + strings.Join(conditions, " OR ") + ")", args, nil
	case dataquality.RuleTypeAllowedValues:
		placeholders := make([]string, len(params.Values))
		args := make([]interface{}, len(params.Values))
		for index, value := range params.Values {
			placeholders[index] = "$" + strconv.Itoa(index+1)
			args[index] = value
		}
		return column + " IS NOT NULL AND " + column + "::text NOT IN (" + strings.Join(placeholders, ", ") + ")", args, nil
	default:
		return "", nil, fmt.Errorf("unsupported rule type %q", rule.Type)
	}
}

func lengthArgument(value interface{ String() string }) interface{} {
	return value.String()
}
