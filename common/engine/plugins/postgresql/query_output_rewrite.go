package postgresql

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	pgquery "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var errPostgresOutputRewriteUnsupported = fmt.Errorf("%w: PostgreSQL output rewrite", plugin.ErrQueryOutputTransformUnsupported)

// CompileQueryOutput lowers neutral output operations on the same prepared
// request. Protocol-compatible engines require separate native certification.
func (p *PostgreSQLPlugin) CompileQueryOutput(query string, args []any, operations []plugin.QueryOutputTransform) (*plugin.SQLQueryOutput, error) {
	if p == nil || p.identity != nil {
		return nil, plugin.ErrQueryOutputTransformUnsupported
	}
	plan, err := rewritePostgresQueryOutput(query, args, operations)
	if err != nil {
		return nil, err
	}
	return &plugin.SQLQueryOutput{Query: plan.query, Args: plan.args, Finalize: plan.finish}, nil
}

type postgresOutputInvalidColumn struct {
	flag   string
	column string
	reject bool
}

type postgresOutputRewrite struct {
	query   string
	args    []any
	columns []string
	omitted []string
	invalid []postgresOutputInvalidColumn
}

// rewritePostgresQueryOutput changes only direct final projections. It rejects
// DISTINCT/grouping/set operations until equivalence can be proved. In
// particular, output aliases and positional ORDER BY retain their original
// source expressions instead of sorting the transformed values.
func rewritePostgresQueryOutput(query string, args []any, operations []plugin.QueryOutputTransform) (*postgresOutputRewrite, error) {
	fail := func() (*postgresOutputRewrite, error) { return nil, errPostgresOutputRewriteUnsupported }
	parsed, err := pgquery.Parse(query)
	if err != nil || len(parsed.GetStmts()) != 1 || len(operations) == 0 {
		return fail()
	}
	parameters := map[int]bool{}
	if !collectPostgresRewriteParameters(parsed.ProtoReflect(), len(args), parameters) || len(parameters) != len(args) {
		return fail()
	}
	stmt := parsed.GetStmts()[0].GetStmt().GetSelectStmt()
	if stmt == nil || stmt.GetOp() != pgquery.SetOperation_SETOP_NONE || len(stmt.GetDistinctClause()) != 0 ||
		len(stmt.GetGroupClause()) != 0 || stmt.GetHavingClause() != nil || len(stmt.GetWindowClause()) != 0 ||
		stmt.GetIntoClause() != nil || len(stmt.GetLockingClause()) != 0 || stmt.GetWithClause() != nil {
		return fail()
	}
	plan := &postgresOutputRewrite{args: append([]any(nil), args...)}
	targets := map[string]*pgquery.ResTarget{}
	originals := map[string]*pgquery.Node{}
	expressions := map[string]string{}
	for _, node := range stmt.GetTargetList() {
		target := node.GetResTarget()
		if target == nil || target.GetVal().GetColumnRef() == nil {
			return fail()
		}
		fields := target.GetVal().GetColumnRef().GetFields()
		if len(fields) == 0 {
			return fail()
		}
		parts := make([]string, len(fields))
		for index, field := range fields {
			if field.GetString_() == nil { // Wildcards have no proven explicit output name.
				return fail()
			}
			parts[index] = `"` + strings.ReplaceAll(field.GetString_().GetSval(), `"`, `""`) + `"`
		}
		name := target.GetName()
		if name == "" {
			name = fields[len(fields)-1].GetString_().GetSval()
		}
		if name == "" || targets[name] != nil {
			return fail()
		}
		targets[name] = target
		originals[name] = proto.Clone(target.GetVal()).(*pgquery.Node)
		expressions[name] = strings.Join(parts, ".")
		plan.columns = append(plan.columns, name)
	}
	for _, node := range stmt.GetSortClause() {
		sort := node.GetSortBy()
		if sort == nil {
			return fail()
		}
		if column := sort.GetNode().GetColumnRef(); column != nil && len(column.GetFields()) == 1 {
			if original := originals[column.GetFields()[0].GetString_().GetSval()]; original != nil {
				sort.Node = postgresInputSortExpression(original)
			}
		} else if constant := sort.GetNode().GetAConst(); constant != nil && constant.GetIval() != nil {
			index := int(constant.GetIval().GetIval()) - 1
			if index < 0 || index >= len(plan.columns) {
				return fail()
			}
			sort.Node = postgresInputSortExpression(originals[plan.columns[index]])
		}
	}
	seen := map[string]bool{}
	flags := map[string]bool{}
	for _, operation := range operations {
		target := targets[operation.Column]
		if target == nil || seen[operation.Column] {
			return fail()
		}
		seen[operation.Column] = true
		// Identifiers come exclusively from the provider's parsed direct ColRef.
		expression := expressions[operation.Column]
		var replacement string
		switch operation.Kind {
		case "omit":
			replacement = "NULL"
			plan.omitted = append(plan.omitted, operation.Column)
		case "constant_text":
			if !utf8.ValidString(operation.Replacement) || strings.ContainsRune(operation.Replacement, 0) {
				return fail()
			}
			plan.args = append(plan.args, operation.Replacement)
			replacement = fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE $%d::pg_catalog.text END", expression, len(plan.args))
		case "keep_prefix_suffix":
			if operation.Prefix < 0 || operation.Suffix < 0 || int64(operation.Prefix) > math.MaxInt32-int64(operation.Suffix) ||
				!utf8.ValidString(operation.MaskRune) || utf8.RuneCountInString(operation.MaskRune) != 1 || strings.ContainsRune(operation.MaskRune, 0) ||
				(operation.InvalidValue != "omit" && operation.InvalidValue != "reject") {
				return fail()
			}
			plan.args = append(plan.args, operation.MaskRune)
			length := fmt.Sprintf("pg_catalog.char_length(%s)", expression)
			invalid := fmt.Sprintf("(%s IS NOT NULL AND %s OPERATOR(pg_catalog.<=) %d)", expression, length, operation.Prefix+operation.Suffix)
			replacement = fmt.Sprintf("CASE WHEN %s IS NULL OR %s THEN NULL ELSE pg_catalog.concat(pg_catalog.left(%s, %d), pg_catalog.repeat($%d::pg_catalog.text, %s OPERATOR(pg_catalog.-) %d), pg_catalog.right(%s, %d)) END", expression, invalid, expression, operation.Prefix, len(plan.args), length, operation.Prefix+operation.Suffix, expression, operation.Suffix)
			flagIndex := len(plan.invalid)
			flag := fmt.Sprintf("__addp_output_invalid_%d", flagIndex)
			for targets[flag] != nil || flags[flag] {
				flagIndex++
				flag = fmt.Sprintf("__addp_output_invalid_%d", flagIndex)
			}
			flags[flag] = true
			flagTarget, parseErr := parsePostgresOutputExpression(invalid)
			if parseErr != nil {
				return fail()
			}
			flagTarget.Name = flag
			stmt.TargetList = append(stmt.TargetList, &pgquery.Node{Node: &pgquery.Node_ResTarget{ResTarget: flagTarget}})
			plan.invalid = append(plan.invalid, postgresOutputInvalidColumn{flag: flag, column: operation.Column, reject: operation.InvalidValue == "reject"})
		default:
			return fail()
		}
		rewritten, parseErr := parsePostgresOutputExpression(replacement)
		if parseErr != nil {
			return fail()
		}
		target.Name, target.Val = operation.Column, rewritten.Val
	}
	plan.query, err = pgquery.Deparse(parsed)
	if err != nil {
		return fail()
	}
	return plan, nil
}

func collectPostgresRewriteParameters(message protoreflect.Message, count int, parameters map[int]bool) bool {
	if parameter, ok := message.Interface().(*pgquery.ParamRef); ok {
		index := int(parameter.GetNumber())
		if index < 1 || index > count {
			return false
		}
		parameters[index] = true
	}
	valid := true
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.Kind() != protoreflect.MessageKind {
			return true
		}
		if field.IsList() {
			list := value.List()
			for index := 0; index < list.Len(); index++ {
				if !collectPostgresRewriteParameters(list.Get(index).Message(), count, parameters) {
					valid = false
					return false
				}
			}
		} else {
			valid = collectPostgresRewriteParameters(value.Message(), count, parameters)
		}
		return valid
	})
	return valid
}

func postgresInputSortExpression(original *pgquery.Node) *pgquery.Node {
	// A bare column with the output alias would resolve to the transformed
	// projection. COALESCE with one argument is value/type/collation preserving
	// and forces PostgreSQL to resolve the column in the original input scope.
	return &pgquery.Node{Node: &pgquery.Node_CoalesceExpr{CoalesceExpr: &pgquery.CoalesceExpr{Args: []*pgquery.Node{proto.Clone(original).(*pgquery.Node)}}}}
}

func parsePostgresOutputExpression(expression string) (*pgquery.ResTarget, error) {
	parsed, err := pgquery.Parse("SELECT " + expression)
	if err != nil {
		return nil, err
	}
	return parsed.GetStmts()[0].GetStmt().GetSelectStmt().GetTargetList()[0].GetResTarget(), nil
}

// finish removes private validity columns and implements missing-field
// semantics. Neither invalid values nor private flags can reach an owner.
func (p *postgresOutputRewrite) finish(result *plugin.QueryResult) error {
	if p == nil || result == nil {
		return errPostgresOutputRewriteUnsupported
	}
	expected := append([]string(nil), p.columns...)
	for _, invalid := range p.invalid {
		expected = append(expected, invalid.flag)
	}
	if !slices.Equal(result.Columns, expected) || len(result.Fields) != len(expected) {
		return errPostgresOutputRewriteUnsupported
	}
	for index, field := range result.Fields {
		if field.Name != expected[index] || index >= len(p.columns) && field.Type != datatype.FieldTypeBool {
			return errPostgresOutputRewriteUnsupported
		}
	}
	for _, row := range result.Rows {
		for _, invalid := range p.invalid {
			value, ok := row[invalid.flag].(bool)
			if !ok || value && invalid.reject {
				return errPostgresOutputRewriteUnsupported
			}
		}
	}
	for _, row := range result.Rows {
		for _, invalid := range p.invalid {
			if row[invalid.flag] == true {
				delete(row, invalid.column)
			}
			delete(row, invalid.flag)
		}
		for _, column := range p.omitted {
			delete(row, column)
		}
	}
	result.Columns = slices.DeleteFunc(append([]string(nil), p.columns...), func(column string) bool { return slices.Contains(p.omitted, column) })
	result.Fields = slices.DeleteFunc(slices.Clone(result.Fields[:len(p.columns)]), func(field datatype.FieldInfo) bool { return slices.Contains(p.omitted, field.Name) })
	return nil
}
