// Package cypherread parses the supported read-only openCypher 9 subset.
// Engine plugins must separately prove function and physical source locality.
package cypherread

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/antlr/antlr4/runtime/Go/antlr"
	cypher "github.com/cloudprivacylabs/opencypher/parser"
)

var ErrUnsupported = errors.New("unsupported local read-only openCypher syntax")

type cypherSyntaxErrors struct {
	*antlr.DefaultErrorListener
	failed bool
}

func (e *cypherSyntaxErrors) SyntaxError(antlr.Recognizer, interface{}, int, int, string, antlr.RecognitionException) {
	e.failed = true
}

type Proof struct {
	Functions []string
	Statement string
}

func ParseLocalRead(query string) (*Proof, error) {
	if len(query) == 0 || len(query) > 256<<10 {
		return nil, ErrUnsupported
	}
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
		return nil, ErrUnsupported
	}
	functions := map[string]struct{}{}
	stack := []antlr.Tree{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch typed := node.(type) {
		case *cypher.OC_UpdatingClauseContext, *cypher.OC_InQueryCallContext, *cypher.OC_StandaloneCallContext:
			return nil, ErrUnsupported
		case *cypher.OC_FunctionNameContext:
			// Escaped identifiers are decoded from AST identifier nodes, not
			// scanned from query text. The engine must separately verify
			// native function identity and its source-access behavior.
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
				return nil, ErrUnsupported
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
	proof := &Proof{Statement: string([]rune(query)[statement.GetStart().GetStart() : statement.GetStop().GetStop()+1])}
	for name := range functions {
		proof.Functions = append(proof.Functions, name)
	}
	sort.Strings(proof.Functions)
	return proof, nil
}

// BoundedStatement caps each UNION branch at the server, without interpreting
// text keywords. LIMIT supports an integer literal or one bound integer parameter.
func (p *Proof) BoundedStatement(maxRows int, parameters map[string]interface{}) (string, error) {
	if maxRows <= 0 || maxRows > 10000 {
		return "", ErrUnsupported
	}
	diagnostics := &cypherSyntaxErrors{DefaultErrorListener: antlr.NewDefaultErrorListener()}
	lexer := cypher.NewCypherLexer(antlr.NewInputStream(p.Statement))
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(diagnostics)
	parser := cypher.NewCypherParser(antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel))
	parser.RemoveErrorListeners()
	parser.AddErrorListener(diagnostics)
	root := parser.OC_Cypher()
	if diagnostics.failed {
		return "", ErrUnsupported
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	stack := []antlr.Tree{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if ret, ok := node.(*cypher.OC_ReturnContext); ok {
			body := ret.OC_ProjectionBody().(*cypher.OC_ProjectionBodyContext)
			capRows := maxRows
			if lim := body.OC_Limit(); lim != nil {
				expression := lim.(*cypher.OC_LimitContext).OC_Expression()
				value := expression.GetText()
				var n int64
				var err error
				if strings.HasPrefix(value, "$") {
					v, ok := parameters[strings.TrimPrefix(value, "$")]
					if !ok {
						return "", ErrUnsupported
					}
					switch v := v.(type) {
					case int:
						n = int64(v)
					case int64:
						n = v
					default:
						return "", ErrUnsupported
					}
				} else {
					n, err = strconv.ParseInt(value, 10, 64)
					if err != nil {
						return "", ErrUnsupported
					}
				}
				if n < 0 {
					return "", ErrUnsupported
				}
				if n < int64(capRows) {
					capRows = int(n)
				}
				edits = append(edits, edit{expression.GetStart().GetStart(), expression.GetStop().GetStop() + 1, strconv.Itoa(capRows)})
			} else {
				end := ret.GetStop().GetStop() + 1
				edits = append(edits, edit{end, end, " LIMIT " + strconv.Itoa(capRows)})
			}
		}
		stack = append(stack, node.GetChildren()...)
	}
	if len(edits) == 0 || len(edits) > 32 {
		return "", ErrUnsupported
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	runes := []rune(p.Statement)
	for _, e := range edits {
		runes = append(append(append([]rune{}, runes[:e.start]...), []rune(e.text)...), runes[e.end:]...)
	}
	return string(runes), nil
}
