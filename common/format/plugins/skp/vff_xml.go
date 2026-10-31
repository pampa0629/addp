package skp

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

type vffXMLLimits struct{ bytes, nodes, depth, attributes, text int }
type vffXMLBudget struct {
	limits                         vffXMLLimits
	bytes, nodes, attributes, text int
}
type vffXMLNode struct {
	name       xml.Name
	attrs      map[string]string
	children   []*vffXMLNode
	text       string
	begin, end int
}

// Qualified attributes are opt-in and retain their expanded names. Material
// and style callers pass nil and keep their existing strict attribute rules.
func readVFFXML(ctx context.Context, raw []byte, budget *vffXMLBudget, namespaces map[string]bool, qualified map[xml.Name]bool) (*vffXMLNode, error) {
	if len(raw) > budget.limits.bytes-budget.bytes {
		return nil, fmt.Errorf("VFF cumulative XML byte budget exceeded")
	}
	budget.bytes += len(raw)
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var stack []*vffXMLNode
	var root *vffXMLNode
	declared := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		position := decoder.InputOffset()
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid VFF XML: %w", err)
		}
		switch t := token.(type) {
		case xml.StartElement:
			budget.nodes++
			budget.attributes += len(t.Attr)
			if budget.nodes > budget.limits.nodes || budget.attributes > budget.limits.attributes || len(stack) >= budget.limits.depth {
				return nil, fmt.Errorf("VFF XML structure budget exceeded")
			}
			if !namespaces[t.Name.Space] {
				return nil, fmt.Errorf("unsupported VFF XML namespace")
			}
			node := &vffXMLNode{name: t.Name, attrs: map[string]string{}, begin: int(position)}
			seen := map[xml.Name]bool{}
			for _, attr := range t.Attr {
				if seen[attr.Name] {
					return nil, fmt.Errorf("duplicate VFF XML attribute")
				}
				seen[attr.Name] = true
				budget.text += len(attr.Value)
				if attr.Name.Space == "xmlns" || attr.Name.Space == "" && attr.Name.Local == "xmlns" {
					if err := vffXMLNamespaceDeclaration(attr); err != nil {
						return nil, err
					}
					continue
				}
				key := attr.Name.Local
				if attr.Name.Space != "" && !qualified[attr.Name] {
					return nil, fmt.Errorf("unsupported VFF namespaced attribute")
				}
				if attr.Name.Space != "" {
					key = vffXMLAttributeKey(attr.Name.Space, attr.Name.Local)
				}
				node.attrs[key] = attr.Value
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, fmt.Errorf("multiple VFF XML roots")
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unmatched VFF XML end")
			}
			stack[len(stack)-1].end = int(decoder.InputOffset())
			stack = stack[:len(stack)-1]
		case xml.CharData:
			budget.text += len(t)
			if len(stack) == 0 {
				if strings.TrimSpace(string(t)) != "" {
					return nil, fmt.Errorf("unclaimed VFF XML text")
				}
			} else {
				stack[len(stack)-1].text += string(t)
			}
		case xml.Directive:
			return nil, fmt.Errorf("VFF XML directives and DTD are unsupported")
		case xml.ProcInst:
			if t.Target != "xml" || position != 0 || root != nil || declared {
				return nil, fmt.Errorf("unsupported VFF XML processing instruction")
			}
			declared = true
		case xml.Comment:
			// Comments have no resource semantics; source bytes remain budgeted.
		}
		if budget.text > budget.limits.text {
			return nil, fmt.Errorf("VFF cumulative XML text budget exceeded")
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, fmt.Errorf("incomplete VFF XML document")
	}
	return root, nil
}

func vffXMLAttributeKey(namespace, local string) string { return "{" + namespace + "}" + local }

// encoding/xml does not itself enforce all Namespaces in XML constraints.
// Reject invalid reserved bindings even when their prefixes are unused.
func vffXMLNamespaceDeclaration(attr xml.Attr) error {
	const xmlNS = "http://www.w3.org/XML/1998/namespace"
	const xmlnsNS = "http://www.w3.org/2000/xmlns/"
	prefix := ""
	if attr.Name.Space == "xmlns" {
		prefix = attr.Name.Local
	}
	if prefix == "xml" {
		if attr.Value != xmlNS {
			return fmt.Errorf("invalid VFF reserved XML namespace binding")
		}
	} else if attr.Value == xmlNS || attr.Value == xmlnsNS || prefix == "xmlns" || strings.Contains(prefix, ":") || prefix != "" && attr.Value == "" {
		return fmt.Errorf("invalid VFF namespace declaration")
	}
	return nil
}
