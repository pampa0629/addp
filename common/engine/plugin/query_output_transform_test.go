package plugin

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
)

type outputSQLProvider struct {
	fakeSQLRuntimeProvider
	compileError     error
	missingFinalizer bool
	compiledSQL      string
	compiledArgs     []any
	finalized        int
}

func (p *outputSQLProvider) CompileQueryOutput(query string, args []any, operations []QueryOutputTransform) (*SQLQueryOutput, error) {
	p.compiledSQL, p.compiledArgs = query, args
	if p.compileError != nil {
		return nil, p.compileError
	}
	compiled := &SQLQueryOutput{Query: "SELECT 'masked' AS phone", Args: []any{operations[0].Replacement}}
	if !p.missingFinalizer {
		compiled.Finalize = func(result *QueryResult) error {
			p.finalized++
			if result == nil {
				return ErrQueryOutputTransformUnsupported
			}
			for _, row := range result.Rows {
				delete(row, "private_flag")
			}
			return nil
		}
	}
	return compiled, nil
}

func prepareOutputSQL(t *testing.T, provider SQLQueryRuntimeProvider, source QueryOutputSource) (OutputTransformPreparedQuery, *[]string) {
	t.Helper()
	var observed []string
	prepared, err := PrepareSQLRuntimeQuery(provider, ConnectionInfo{"host": "db"}, QueryRequest{EngineID: 9, Language: "sql", Query: "SELECT phone FROM public.people WHERE id=:id", Options: QueryOptions{ReadOnly: true, Parameters: map[string]any{"id": 1}}},
		func(_ context.Context, _ ConnectionInfo, request QueryRequest) (*QueryReadSet, error) {
			observed = append(observed, request.Query)
			return NewQueryReadSet(source.Path)
		},
		func(_ context.Context, _ ConnectionInfo, request QueryRequest, _ *QueryReadSet) (*QueryOutputLineage, error) {
			observed = append(observed, request.Query)
			return &QueryOutputLineage{Sources: []QueryOutputSource{source}}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	return prepared.(OutputTransformPreparedQuery), &observed
}

func outputSource() QueryOutputSource {
	return QueryOutputSource{Path: TabularItemPath(9, EngineCatalogTermSchema, "public", "people"), IdentityOutput: true, Fields: []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString}}}
}

func TestSQLPreparedOutputBindingExecutesOnceWithoutReplacingLogicalFacts(t *testing.T) {
	p := &outputSQLProvider{}
	prepared, observed := prepareOutputSQL(t, p, outputSource())
	operations := []QueryOutputTransform{{Column: "phone", Kind: "constant_text", Replacement: "masked"}}
	if err := prepared.BindOutputTransforms(t.Context(), operations); err != nil {
		t.Fatal(err)
	}
	operations[0].Replacement = "caller mutation"
	if p.compiledSQL != "SELECT phone FROM public.people WHERE id=$1" || !reflect.DeepEqual(p.compiledArgs, []any{1}) {
		t.Fatal("compiler did not receive original bound request")
	}
	if _, err := prepared.ReadSet(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.OutputLineage(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*observed, []string{p.compiledSQL, p.compiledSQL}) {
		t.Fatal("logical facts resolved from rewritten SQL or re-resolved")
	}
	if _, err := prepared.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	if p.lastSQL != "SELECT 'masked' AS phone" || !reflect.DeepEqual(p.lastOptions.Args, []any{"masked"}) || p.finalized != 1 {
		t.Fatal("execution missed compiled request or mandatory finalization")
	}
	if _, err := prepared.Execute(t.Context()); !errors.Is(err, ErrPreparedQueryConsumed) {
		t.Fatal(err)
	}
	if err := prepared.BindOutputTransforms(t.Context(), operations); !errors.Is(err, ErrPreparedQueryConsumed) {
		t.Fatal(err)
	}
}

func TestSQLPreparedOutputBindingFailureCannotExecuteOriginalQuery(t *testing.T) {
	for _, mode := range []string{"no compiler", "compiler error", "missing finalizer", "opaque", "derived", "numeric", "missing column", "empty operations", "duplicate binding"} {
		t.Run(mode, func(t *testing.T) {
			p := &outputSQLProvider{}
			var provider SQLQueryRuntimeProvider = p
			source := outputSource()
			operations := []QueryOutputTransform{{Column: "phone", Kind: "constant_text"}}
			switch mode {
			case "no compiler":
				provider = &p.fakeSQLRuntimeProvider
			case "compiler error":
				p.compileError = errors.New("unsupported syntax")
			case "missing finalizer":
				p.missingFinalizer = true
			case "opaque":
				source.IdentityOutput = false
				source.OpaqueOutput = true
			case "derived":
				source.IdentityOutput = false
				source.Bindings = []QueryOutputBinding{{SourcePath: []string{"phone"}, OutputPath: []string{"phone"}, Transformation: QueryOutputTransformationDerived}}
			case "numeric":
				source.Fields[0].Type = datatype.FieldTypeBigInt
			case "missing column":
				operations[0].Column = "unknown"
			case "empty operations":
				operations = nil
			}
			prepared, _ := prepareOutputSQL(t, provider, source)
			if mode == "duplicate binding" {
				if err := prepared.BindOutputTransforms(t.Context(), operations); err != nil {
					t.Fatal(err)
				}
			}
			if err := prepared.BindOutputTransforms(t.Context(), operations); err == nil {
				t.Fatal("binding unexpectedly succeeded")
			}
			if _, _, err := ConsumeSQLPreparedQuery(prepared, provider); err == nil {
				t.Fatal("unsafe atomic consumption succeeded")
			}
			if _, err := ConsumeSQLPreparedReadSession(prepared, provider); err == nil {
				t.Fatal("failed binding opened read session")
			}
			if result, err := prepared.Execute(t.Context()); err == nil || result != nil {
				t.Fatal("failed binding returned original result")
			}
			if p.lastSQL != "" {
				t.Fatal("failed binding executed original SQL")
			}
		})
	}
}

func TestSQLPreparedOutputReadSessionConsumesSamePlanAndFinalizesBatches(t *testing.T) {
	p := &outputSQLProvider{}
	prepared, _ := prepareOutputSQL(t, p, outputSource())
	if err := prepared.BindOutputTransforms(t.Context(), []QueryOutputTransform{{Column: "phone", Kind: "omit"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ConsumeSQLPreparedQuery(prepared, p); !errors.Is(err, ErrQueryOutputTransformUnsupported) {
		t.Fatal("unaware atomic consumer accepted transformed outputs", err)
	}
	if _, err := ConsumeSQLPreparedReadSession(prepared, &fakeSQLRuntimeProvider{engineType: "mysql"}); err == nil {
		t.Fatal("different provider accepted request")
	}
	session, err := ConsumeSQLPreparedReadSession(prepared, p)
	if err != nil {
		t.Fatal(err)
	}
	if session.Request.Query != "SELECT 'masked' AS phone" || session.ConnectionInfo["host"] != "db" {
		t.Fatal("read session missed compiled request")
	}
	for index := range 2 {
		batch := &BatchData{Rows: []map[string]any{{"phone": "masked", "private_flag": false}}, Fields: []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString}}, Offset: int64(index), Hints: map[string]any{"key": "value"}}
		if err := session.FinalizeBatch(batch); err != nil {
			t.Fatal(err)
		}
		if _, exists := batch.Rows[0]["private_flag"]; exists || batch.Offset != int64(index) || batch.Hints["key"] != "value" {
			t.Fatal("batch finalization missed private fields or changed coordinates")
		}
	}
	if p.finalized != 2 {
		t.Fatal("not all batches finalized")
	}
	if _, err := ConsumeSQLPreparedReadSession(prepared, p); !errors.Is(err, ErrPreparedQueryConsumed) {
		t.Fatal(err)
	}
	if _, err := prepared.Execute(t.Context()); !errors.Is(err, ErrPreparedQueryConsumed) {
		t.Fatal(err)
	}
}
