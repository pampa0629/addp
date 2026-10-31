package skp

import (
	"context"
	"crypto/sha256"
	"fmt"
)

// Inventory the XSD 1.0 resource-bearing roles across the entire bounded tree.
// Other fields/subtrees remain opaque; this is not a schema-validity result.
func vffSKCSchemaResources(ctx context.Context, root *vffXMLNode, raw []byte, remaining *int) ([]vffSKCResourceDeclaration, error) {
	type entry struct{ node, parent *vffXMLNode }
	stack := []entry{{root, nil}}
	var result []vffSKCResourceDeclaration
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node := current.node
		kind := node.name.Local
		var attributes []string
		switch kind {
		case "include", "import", "redefine":
			if current.parent != root {
				return nil, fmt.Errorf("SKC schema dependency outside root children")
			}
			// The existing dependency reader binds only these root declarations.
		case "documentation", "appinfo":
			if current.parent == nil || current.parent.name.Local != "annotation" {
				return nil, fmt.Errorf("SKC annotation resource outside annotation")
			}
			attributes = []string{"source"}
		case "notation":
			if current.parent != root {
				return nil, fmt.Errorf("SKC notation outside schema root")
			}
			attributes = []string{"system", "public"}
		}
		for _, attribute := range attributes {
			value, exists := node.attrs[attribute]
			if !exists {
				continue
			}
			if *remaining <= 0 {
				return nil, fmt.Errorf("SKC cumulative resource declaration budget exceeded")
			}
			*remaining -= 1
			result = append(result, vffSKCResourceDeclaration{kind: kind, attribute: attribute, value: value, begin: node.begin, end: node.end, sha256: sha256.Sum256(raw[node.begin:node.end])})
		}
		for i := len(node.children) - 1; i >= 0; i-- {
			stack = append(stack, entry{node.children[i], node})
		}
	}
	return result, nil
}
