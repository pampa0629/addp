package postgresql

import (
	"errors"
	"strconv"
	"strings"
	"unicode"

	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	pgquery "github.com/pganalyze/pg_query_go/v6"
	"github.com/pganalyze/pg_query_go/v6/parser"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func postgresSyntaxError(err error, source, bound string) error {
	var parseError *parser.Error
	if !errors.As(err, &parseError) {
		return err
	}
	offset, ok := commonquery.ByteOffsetAtCharacter(bound, parseError.Cursorpos)
	if !ok {
		return err
	}
	context := commonquery.NewErrorContext(bound, "", "", offset)
	return plugin.WithQueryErrorContext(err, context.RemapSource(source, bound))
}

func postgresFailureObject(message protoreflect.Message) (*commonquery.ErrorObject, int) {
	var kind string
	var parts []string
	offset := -1
	switch node := message.Interface().(type) {
	case *pgquery.RangeVar:
		kind, offset = "table", int(node.Location)
		parts = []string{node.Schemaname, node.Relname}
	case *pgquery.ColumnRef:
		kind, offset = "column", int(node.Location)
		for _, part := range node.Fields {
			if part.GetString_() == nil {
				return nil, -1
			}
			parts = append(parts, part.GetString_().Sval)
		}
	case *pgquery.FuncCall:
		kind, offset = "function", int(node.Location)
		for _, part := range node.Funcname {
			parts = append(parts, part.GetString_().GetSval())
		}
	case *pgquery.A_Expr:
		kind, offset = "operator", int(node.Location)
		for _, part := range node.Name {
			parts = append(parts, part.GetString_().GetSval())
		}
	default:
		return nil, -1
	}
	names := []string{}
	for _, part := range parts {
		if part == "" {
			continue
		}
		if kind == "operator" {
			names = append(names, part)
		} else {
			names = append(names, commonquery.ForDialect(commonquery.DialectPostgreSQL).QuoteIdentifier(part))
		}
	}
	if len(names) == 0 {
		return nil, -1
	}
	return &commonquery.ErrorObject{Kind: kind, Name: strings.Join(names, ".")}, offset
}

func walkPostgresFailureObjects(message protoreflect.Message, visit func(*commonquery.ErrorObject, int) bool) bool {
	if object, offset := postgresFailureObject(message); object != nil && visit(object, offset) {
		return true
	}
	found := false
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.Kind() != protoreflect.MessageKind || field.IsMap() {
			return true
		}
		if field.IsList() {
			for i := 0; i < value.List().Len(); i++ {
				if walkPostgresFailureObjects(value.List().Get(i).Message(), visit) {
					found = true
					return false
				}
			}
		} else {
			found = walkPostgresFailureObjects(value.Message(), visit)
		}
		return !found
	})
	return found
}

func postgresObjectContext(source string, matches func(*commonquery.ErrorObject, int) bool) *commonquery.ErrorContext {
	parsed, err := pgquery.Parse(source)
	if err != nil {
		return nil
	}
	var context *commonquery.ErrorContext
	walkPostgresFailureObjects(parsed.ProtoReflect(), func(object *commonquery.ErrorObject, offset int) bool {
		if !matches(object, offset) {
			return false
		}
		context = commonquery.NewErrorContext(source, object.Kind, object.Name, offset)
		return true
	})
	return context
}

func postgresNamedObjectContext(source, kind, schema, name string) *commonquery.ErrorContext {
	parts := []string{}
	for _, part := range []string{schema, name} {
		if part != "" {
			parts = append(parts, commonquery.ForDialect(commonquery.DialectPostgreSQL).QuoteIdentifier(part))
		}
	}
	wanted := strings.Join(parts, ".")
	return postgresObjectContext(source, func(object *commonquery.ErrorObject, _ int) bool { return object.Kind == kind && object.Name == wanted })
}

// Read only typed SQLSTATE and Position. Message/Detail/Hint and database-reported
// object fields can contain business values or names outside the submitted SQL.
func postgresNativeQueryError(err error, source string, opts plugin.QueryOptions) error {
	actualStatement, executed := plugin.ExecutedSQLForError(err)
	if !executed {
		return err
	}
	state, position := "", 0
	var pgxError *pgconn.PgError
	var pqError *pq.Error
	if errors.As(err, &pgxError) {
		state, position = pgxError.Code, int(pgxError.Position)
	} else if errors.As(err, &pqError) {
		state = string(pqError.Code)
		position, _ = strconv.Atoi(pqError.Position)
	}
	allowed := map[string]bool{"42601": true, "42P01": true, "42703": true, "42702": true, "42883": true, "42804": true, "22P02": true, "22003": true, "22007": true, "22008": true, "22012": true}
	if !allowed[state] || position < 1 {
		return err
	}
	bound, _, bindErr := plugin.BindSQLRuntimeParameters(commonquery.DialectPostgreSQL, source, opts)
	if bindErr != nil {
		return err
	}
	statement := bound
	start, end := 0, len(bound)
	if opts.Limit > 0 {
		statement, start, end = commonquery.ForDialect(commonquery.DialectPostgreSQL).PaginateQuerySQLSourceRange(bound, opts.Limit, 0)
	}
	if actualStatement != statement {
		return err
	}
	offset, ok := commonquery.ByteOffsetAtCharacter(statement, position)
	if !ok || offset < start || offset > end || (opts.Limit > 0 && offset == end) {
		return err
	}
	offset -= start
	if opts.Limit > 0 {
		offset += len(bound) - len(strings.TrimLeftFunc(bound, unicode.IsSpace))
	}
	kinds := map[string]string{"42P01": "table", "42703": "column", "42702": "column", "42883": "function|operator"}
	context := postgresObjectContext(bound, func(object *commonquery.ErrorObject, at int) bool {
		return at == offset && (kinds[state] == object.Kind || (kinds[state] == "function|operator" && (object.Kind == "function" || object.Kind == "operator")))
	})
	if context == nil {
		context = commonquery.NewErrorContext(bound, "", "", offset)
	}
	return plugin.WithQueryErrorContext(err, context.RemapSource(source, bound))
}
