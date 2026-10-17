package neo4j

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/cypherread"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// The parser is the ANTLR-generated openCypher 9 grammar, pinned by go.mod.
// Unsupported extensions are rejected, never passed through as local reads.
var errLocalReadCypher = plugin.NewQueryError(plugin.QueryErrorCodeLocalReadUnsupported, errors.New("Cypher query requires a physical local database, built-in functions and supported read-only openCypher syntax; procedures, remote sources and unsupported extensions are refused"))

type cypherReadProof = cypherread.Proof

func parseLocalReadCypher(query string) (*cypherReadProof, error) {
	proof, err := cypherread.ParseLocalRead(query)
	if err != nil {
		return nil, errLocalReadCypher
	}
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
	if len(proof.Functions) == 0 {
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
	for _, name := range proof.Functions {
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
