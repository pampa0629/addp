package plugin

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/addp/common/datatype"
)

var ErrQueryOutputTransformUnsupported = errors.New("query output transform unsupported")

// QueryOutputTransform describes a physical value operation, without policy or
// authorization facts. Columns refer to the original logical result.
type QueryOutputTransform struct {
	Column       string
	Kind         string // keep_prefix_suffix, constant_text, omit
	Prefix       int
	Suffix       int
	MaskRune     string
	Replacement  string
	InvalidValue string // omit or reject
}

type OutputTransformPreparedQuery interface {
	PreparedQuery
	BindOutputTransforms(context.Context, []QueryOutputTransform) error
}

// SQLQueryOutputCompiler belongs to the actual dialect Provider. It must be
// deterministic, preserve input semantics, and bind generated values as args.
type SQLQueryOutputCompiler interface {
	CompileQueryOutput(string, []any, []QueryOutputTransform) (*SQLQueryOutput, error)
}

type SQLQueryOutputCompilerProvider interface {
	SQLQueryOutputCompiler() SQLQueryOutputCompiler
}

// SQLQueryOutput includes the private result protocol of one physical query.
// Only a Provider compiler constructs it; owners cannot install physical SQL.
type SQLQueryOutput struct {
	Query    string
	Args     []any
	Finalize func(*QueryResult) error
}

type preparedSQLQueryWithOutput struct {
	*preparedSQLQuery
}

func (p *preparedSQLQueryWithOutput) BindOutputTransforms(ctx context.Context, operations []QueryOutputTransform) error {
	// Resolve immutable logical facts before taking the mutation lock. Execute
	// racing this resolution wins consumption and causes binding to fail.
	lineage, factErr := p.OutputLineage(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.consumed {
		return ErrPreparedQueryConsumed
	}
	if p.outputAttempted {
		p.outputErr = fmt.Errorf("%w: output already bound", ErrQueryOutputTransformUnsupported)
		return p.outputErr
	}
	p.outputAttempted = true
	p.outputErr = ErrQueryOutputTransformUnsupported
	if err := ctx.Err(); err != nil {
		p.outputErr = err
		return err
	}
	if factErr != nil {
		p.outputErr = factErr
		return factErr
	}
	if !p.request.Options.ReadOnly || p.request.analytical != nil || p.outputCompiler == nil || !directOutputTransforms(lineage, operations) {
		return p.outputErr
	}
	compiled, err := p.outputCompiler.CompileQueryOutput(p.request.Query, cloneQueryValues(p.request.Options.Args), slices.Clone(operations))
	if err != nil {
		p.outputErr = err
		return err
	}
	if compiled == nil || compiled.Query == "" || compiled.Finalize == nil {
		return p.outputErr
	}
	p.request.Query = compiled.Query
	p.request.Options.Args = cloneQueryValues(compiled.Args)
	p.outputFinalize = compiled.Finalize
	p.outputErr = nil
	return nil
}

func directOutputTransforms(lineage *QueryOutputLineage, operations []QueryOutputTransform) bool {
	if lineage == nil || len(operations) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, operation := range operations {
		if operation.Column == "" || seen[operation.Column] {
			return false
		}
		seen[operation.Column] = true
		matches := 0
		for _, source := range lineage.Sources {
			if source.OpaqueOutput {
				return false
			}
			for _, field := range source.Fields {
				match := source.IdentityOutput && field.Name == operation.Column
				for _, binding := range source.Bindings {
					if len(binding.OutputPath) != 1 || binding.OutputPath[0] != operation.Column {
						continue
					}
					if binding.Transformation != QueryOutputTransformationDirect || len(binding.SourcePath) != 1 {
						return false
					}
					if binding.SourcePath[0] == field.Name {
						if match {
							return false
						}
						match = true
					}
				}
				if match {
					if operation.Kind != "omit" && field.Type != datatype.FieldTypeString {
						return false
					}
					matches++
				}
			}
		}
		if matches != 1 {
			return false
		}
	}
	return true
}

// SQLPreparedReadSession carries the consumed physical request together with
// its mandatory batch finalizer. It is only a Provider execution bridge.
type SQLPreparedReadSession struct {
	ConnectionInfo ConnectionInfo
	Request        QueryRequest
	finalize       func(*QueryResult) error
}

func ConsumeSQLPreparedReadSession(prepared PreparedQuery, provider SQLQueryRuntimeProvider) (*SQLPreparedReadSession, error) {
	plan, conn, request, err := consumeSQLPreparedQuery(prepared, provider, true)
	if err != nil {
		return nil, err
	}
	return &SQLPreparedReadSession{ConnectionInfo: conn, Request: request, finalize: plan.outputFinalize}, nil
}

// FinalizeBatch does not change offset, spatial information or hints. A failed
// batch must be discarded and the underlying read session closed by Provider.
func (p *SQLPreparedReadSession) FinalizeBatch(batch *BatchData) error {
	if p == nil || batch == nil {
		return ErrQueryOutputTransformUnsupported
	}
	if p.finalize == nil {
		return nil
	}
	columns := make([]string, len(batch.Fields))
	for index, field := range batch.Fields {
		columns[index] = field.Name
	}
	result := &QueryResult{Rows: batch.Rows, Columns: columns, Fields: batch.Fields}
	if err := p.finalize(result); err != nil {
		return err
	}
	batch.Rows, batch.Fields = result.Rows, result.Fields
	return nil
}
