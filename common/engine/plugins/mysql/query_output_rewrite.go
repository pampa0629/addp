package mysql

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"github.com/dolthub/vitess/go/vt/sqlparser"
)

func (p *MySQLPlugin) SQLQueryOutputCompiler() plugin.SQLQueryOutputCompiler { return p }

func (p *MySQLPlugin) CompileQueryOutput(query string, args []any, operations []plugin.QueryOutputTransform) (*plugin.SQLQueryOutput, error) {
	fail := func() (*plugin.SQLQueryOutput, error) { return nil, plugin.ErrQueryOutputTransformUnsupported }
	if len(query) > 1<<20 || len(operations) == 0 || strings.Contains(query, "/*!") || strings.Contains(query, "/*+") || strings.ContainsAny(query, "\\\"!") || strings.Contains(query, "||") {
		return fail()
	}
	// Only positional parameters from the frozen request are accepted. Named
	// ValArgs created below are compiler-private and cannot capture user input.
	if names, err := commonquery.SQLReferences(query); err != nil || len(names) != 0 {
		return fail()
	}
	parsed, err := sqlparser.Parse(query)
	if err != nil {
		return fail()
	}
	statement, ok := parsed.(*sqlparser.Select)
	if !ok || statement.With != nil || statement.QueryOpts.Distinct || statement.QueryOpts.SQLCalcFoundRows || len(statement.GroupBy) != 0 || statement.Having != nil || len(statement.Window) != 0 || statement.Lock != "" || statement.Into != nil {
		return fail()
	}
	parameters := map[string]any{}
	seenArgs := map[int]bool{}
	budget := 100000
	err = sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		budget--
		if budget < 0 {
			return false, plugin.ErrQueryOutputTransformUnsupported
		}
		// The first slice excludes SQL-mode-sensitive expressions. In
		// particular, parsing || or NOT under a different mode could alter the
		// original predicate even though only the SELECT list was edited.
		if expression, ok := node.(sqlparser.Expr); ok && !mySQLOutputInputExpression(expression) {
			return false, plugin.ErrQueryOutputTransformUnsupported
		}
		switch n := node.(type) {
		case *sqlparser.Select:
			if n != statement {
				return false, plugin.ErrQueryOutputTransformUnsupported
			}
		case *sqlparser.Subquery, *sqlparser.SetOp, *sqlparser.FuncExpr, *sqlparser.AssignmentExpr, *sqlparser.SetVarExpr, *sqlparser.Nextval:
			return false, plugin.ErrQueryOutputTransformUnsupported
		case *sqlparser.AliasedTableExpr:
			if _, ok := n.Expr.(sqlparser.TableName); !ok || n.AsOf != nil || n.Lateral {
				return false, plugin.ErrQueryOutputTransformUnsupported
			}
		case *sqlparser.SQLVal:
			switch n.Type {
			case sqlparser.ValArg:
				index, err := strconv.Atoi(strings.TrimPrefix(string(n.Val), ":v"))
				if err != nil || index < 1 || index > len(args) || string(n.Val) != fmt.Sprintf(":v%d", index) {
					return false, plugin.ErrQueryOutputTransformUnsupported
				}
				seenArgs[index] = true
				parameters[fmt.Sprintf("v%d", index)] = args[index-1]
			case sqlparser.IntVal, sqlparser.FloatVal:
			default:
				return false, plugin.ErrQueryOutputTransformUnsupported
			}
		}
		return true, nil
	}, statement)
	if err != nil || len(seenArgs) != len(args) {
		return fail()
	}
	var soleQualifier sqlparser.TableName
	if len(statement.From) == 1 {
		if table, ok := statement.From[0].(*sqlparser.AliasedTableExpr); ok {
			soleQualifier = table.Expr.(sqlparser.TableName)
			if !table.As.IsEmpty() {
				soleQualifier = sqlparser.TableName{Name: table.As}
			}
		}
	}
	targets := map[string]*sqlparser.AliasedExpr{}
	originals := map[string]*sqlparser.ColName{}
	columns := []string{}
	seenNames := map[string]bool{}
	for _, expr := range statement.SelectExprs {
		target, ok := expr.(*sqlparser.AliasedExpr)
		if !ok {
			return fail()
		}
		column, ok := target.Expr.(*sqlparser.ColName)
		if !ok || column.StoredProcVal != nil {
			return fail()
		}
		original := *column
		if original.Qualifier.IsEmpty() {
			if soleQualifier.IsEmpty() {
				return fail()
			}
			original.Qualifier = soleQualifier
		}
		name := target.As.String()
		if name == "" {
			name = column.Name.String()
		}
		if name == "" || seenNames[strings.ToLower(name)] {
			return fail()
		}
		seenNames[strings.ToLower(name)] = true
		targets[name], originals[name] = target, &original
		columns = append(columns, name)
		// Parser source fragments must never override the modified AST.
		target.InputExpression = ""
	}
	for _, order := range statement.OrderBy {
		if column, ok := order.Expr.(*sqlparser.ColName); ok && column.Qualifier.IsEmpty() {
			for name, original := range originals {
				if strings.EqualFold(name, column.Name.String()) {
					copy := *original
					order.Expr = &copy
					break
				}
			}
		} else if value, ok := order.Expr.(*sqlparser.SQLVal); ok && value.Type == sqlparser.IntVal {
			index, err := strconv.Atoi(string(value.Val))
			if err != nil || index < 1 || index > len(columns) {
				return fail()
			}
			copy := *originals[columns[index-1]]
			order.Expr = &copy
		} else if _, ok := order.Expr.(*sqlparser.ColName); !ok {
			return fail()
		}
	}
	omitted := []string{}
	invalid := []plugin.SQLQueryOutputInvalid{}
	seenOperations := map[string]bool{}
	for index, operation := range operations {
		target := targets[operation.Column]
		if target == nil || seenOperations[operation.Column] {
			return fail()
		}
		seenOperations[operation.Column] = true
		source := sqlparser.String(originals[operation.Column])
		parameter := fmt.Sprintf("addp_output_%d", index)
		var expression string
		switch operation.Kind {
		case "omit":
			expression = "NULL"
			omitted = append(omitted, operation.Column)
		case "constant_text":
			if !utf8.ValidString(operation.Replacement) || strings.ContainsRune(operation.Replacement, 0) {
				return fail()
			}
			parameters[parameter] = operation.Replacement
			expression = fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE CAST(:%s AS CHAR CHARACTER SET utf8mb4) END", source, parameter)
		case "keep_prefix_suffix":
			if operation.Prefix < 0 || operation.Suffix < 0 || int64(operation.Prefix) > math.MaxInt32-int64(operation.Suffix) || !utf8.ValidString(operation.MaskRune) || utf8.RuneCountInString(operation.MaskRune) != 1 || strings.ContainsRune(operation.MaskRune, 0) || (operation.InvalidValue != "omit" && operation.InvalidValue != "reject") {
				return fail()
			}
			parameters[parameter] = operation.MaskRune
			length := fmt.Sprintf("CHAR_LENGTH(%s)", source)
			bad := fmt.Sprintf("(%s IS NOT NULL AND %s <= %d)", source, length, operation.Prefix+operation.Suffix)
			expression = fmt.Sprintf("CASE WHEN %s IS NULL OR %s THEN NULL ELSE CONCAT(LEFT(%s,%d),REPEAT(:%s,%s-%d),RIGHT(%s,%d)) END", source, bad, source, operation.Prefix, parameter, length, operation.Prefix+operation.Suffix, source, operation.Suffix)
			flagIndex := len(invalid)
			flag := fmt.Sprintf("__addp_output_invalid_%d", flagIndex)
			for seenNames[strings.ToLower(flag)] {
				flagIndex++
				flag = fmt.Sprintf("__addp_output_invalid_%d", flagIndex)
			}
			seenNames[strings.ToLower(flag)] = true
			flagExpr, err := parseMySQLOutputExpression(bad)
			if err != nil {
				return fail()
			}
			flagExpr.As = sqlparser.NewColIdent(flag)
			statement.SelectExprs = append(statement.SelectExprs, flagExpr)
			invalid = append(invalid, plugin.SQLQueryOutputInvalid{Flag: flag, Column: operation.Column, Reject: operation.InvalidValue == "reject"})
		default:
			return fail()
		}
		replacement, err := parseMySQLOutputExpression(expression)
		if err != nil {
			return fail()
		}
		target.Expr, target.As = replacement.Expr, sqlparser.NewColIdent(operation.Column)
	}
	physical, boundArgs, err := commonquery.BindSQL(sqlparser.String(statement), parameters, commonquery.SQLPlaceholderQuestion)
	if err != nil {
		return fail()
	}
	return &plugin.SQLQueryOutput{Query: physical, Args: boundArgs, Finalize: func(result *plugin.QueryResult) error {
		return plugin.FinalizeSQLQueryOutput(result, columns, omitted, invalid, func(field datatype.FieldInfo, value any) (bool, bool) {
			if field.Type != datatype.FieldTypeInt && field.Type != datatype.FieldTypeBigInt {
				return false, false
			}
			if value == nil {
				return false, true
			}
			flag, ok := value.(int64)
			return flag == 1, ok && (flag == 0 || flag == 1)
		})
	}}, nil
}

func mySQLOutputInputExpression(expression sqlparser.Expr) bool {
	switch value := expression.(type) {
	case *sqlparser.ColName, *sqlparser.SQLVal, *sqlparser.NullVal, sqlparser.BoolVal, *sqlparser.AndExpr, *sqlparser.OrExpr, *sqlparser.ParenExpr:
		return true
	case *sqlparser.ComparisonExpr:
		switch value.Operator {
		case "=", "<>", "<", ">", "<=", ">=", "<=>":
			return true
		}
	case *sqlparser.IsExpr:
		return value.Operator == "is null" || value.Operator == "is not null"
	}
	return false
}

func parseMySQLOutputExpression(expression string) (*sqlparser.AliasedExpr, error) {
	parsed, err := sqlparser.Parse("SELECT " + expression)
	if err != nil {
		return nil, err
	}
	return parsed.(*sqlparser.Select).SelectExprs[0].(*sqlparser.AliasedExpr), nil
}
