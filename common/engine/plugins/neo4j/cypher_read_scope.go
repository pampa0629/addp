package neo4j

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/addp/common/engine/plugin"
	"github.com/antlr/antlr4/runtime/Go/antlr"
	cypher "github.com/cloudprivacylabs/opencypher/parser"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// The parser is the ANTLR-generated openCypher 9 grammar, pinned by go.mod.
// Unsupported extensions are rejected, never passed through as local reads.
var errLocalReadCypher = plugin.NewQueryError(plugin.QueryErrorCodeLocalReadUnsupported, errors.New("Cypher query requires a physical local database, built-in functions and supported read-only openCypher syntax; procedures, remote sources and unsupported extensions are refused"))

type cypherSyntaxErrors struct {
	*antlr.DefaultErrorListener
	failed bool
}

func (e *cypherSyntaxErrors) SyntaxError(antlr.Recognizer, interface{}, int, int, string, antlr.RecognitionException) {
	e.failed = true
}

type cypherReadProof struct {
	functions []string
	statement string
}

func parseLocalReadCypher(query string) (*cypherReadProof, error) {
	diagnostics := &cypherSyntaxErrors{DefaultErrorListener: antlr.NewDefaultErrorListener()}
	lexer := cypher.NewCypherLexer(antlr.NewInputStream(query))
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(diagnostics)
	tokens := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := cypher.NewCypherParser(tokens)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(diagnostics)
	root := parser.OC_Cypher()
	if diagnostics.failed {
		return nil, errLocalReadCypher
	}
	functions := map[string]struct{}{}
	stack := []antlr.Tree{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch typed := node.(type) {
		case *cypher.OC_UpdatingClauseContext, *cypher.OC_InQueryCallContext, *cypher.OC_StandaloneCallContext:
			return nil, errLocalReadCypher
		case *cypher.OC_FunctionNameContext:
			// Escaped identifiers are decoded from AST identifier nodes, not
			// scanned from query text. SHOW FUNCTIONS is the authority on
			// built-in functions; custom functions cannot prove local access.
			var names []string
			invalidQualification := false
			var collect func(antlr.Tree)
			collect = func(n antlr.Tree) {
				if identifier, ok := n.(*cypher.OC_SymbolicNameContext); ok {
					name := identifier.GetText()
					if strings.HasPrefix(name, "`") {
						name = strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(name, "`"), "`"), "``", "`")
					}
					// SHOW FUNCTIONS renders qualified names with dots. A dot
					// inside one escaped segment would make two different
					// native names indistinguishable in that metadata.
					if strings.Contains(name, ".") {
						invalidQualification = true
					}
					names = append(names, name)
					return
				}
				for _, child := range n.GetChildren() {
					collect(child)
				}
			}
			collect(typed)
			if invalidQualification {
				return nil, errLocalReadCypher
			}
			name := strings.Join(names, ".")
			if typed.EXISTS() != nil {
				name = "exists"
			}
			functions[name] = struct{}{}
		}
		stack = append(stack, node.GetChildren()...)
	}
	statement := root.(*cypher.OC_CypherContext).OC_Statement()
	proof := &cypherReadProof{statement: string([]rune(query)[statement.GetStart().GetStart() : statement.GetStop().GetStop()+1])}
	for name := range functions {
		proof.functions = append(proof.functions, name)
	}
	sort.Strings(proof.functions)
	return proof, nil
}

// Metadata is read from the same fixed connection/database as execution. A
// physical standard database is required: aliases and composite databases do
// not prove access is confined to this registered engine instance.
func (p *Neo4jPlugin) verifyLocalReadCypher(ctx context.Context, conn plugin.ConnectionInfo, proof *cypherReadProof) error {
	driver, err := p.createDriver(ctx, conn)
	if err != nil {
		return err
	}
	defer driver.Close(ctx)
	databases, err := neo4jdriver.ExecuteQuery(ctx, driver,
		"SHOW DATABASES YIELD name, type WHERE name = $database RETURN type",
		map[string]interface{}{"database": getDatabase(conn)}, neo4jdriver.EagerResultTransformer,
		neo4jdriver.ExecuteQueryWithDatabase("system"), neo4jdriver.ExecuteQueryWithReadersRouting())
	if err != nil {
		return fmt.Errorf("verify local Cypher database: %w", err)
	}
	if len(databases.Records) != 1 {
		return errLocalReadCypher
	}
	databaseType, _ := databases.Records[0].Get("type")
	if databaseType != "standard" {
		return errLocalReadCypher
	}
	if len(proof.functions) == 0 {
		return nil
	}
	functions, err := neo4jdriver.ExecuteQuery(ctx, driver,
		"SHOW FUNCTIONS YIELD name, isBuiltIn WHERE isBuiltIn RETURN name", nil,
		neo4jdriver.EagerResultTransformer, neo4jdriver.ExecuteQueryWithDatabase(getDatabase(conn)),
		neo4jdriver.ExecuteQueryWithReadersRouting())
	if err != nil {
		return fmt.Errorf("verify built-in Cypher functions: %w", err)
	}
	builtins := map[string]struct{}{}
	for _, record := range functions.Records {
		name, ok := record.Get("name")
		if text, valid := name.(string); ok && valid {
			builtins[text] = struct{}{}
		}
	}
	for _, name := range proof.functions {
		if _, ok := builtins[name]; !ok {
			return errLocalReadCypher
		}
	}
	return nil
}

func (p *Neo4jPlugin) localReadResolver(conn plugin.ConnectionInfo, proof *cypherReadProof, engineID uint) func(context.Context) (*plugin.QueryEngineReadScope, error) {
	var once sync.Once
	var err error
	return func(ctx context.Context) (*plugin.QueryEngineReadScope, error) {
		once.Do(func() { err = p.verifyLocalReadCypher(ctx, conn, proof) })
		if err != nil {
			return nil, err
		}
		return plugin.NewQueryEngineReadScope(engineID)
	}
}
