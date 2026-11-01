package skp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
)

// Only a local edge's exact bytes and declared namespace relationship. This
// is not a resolver, symbol graph, complete schema, or composition receipt.
type vffSKCSchemaLink struct {
	source, target                   vffSKCMemberFact
	dependency, begin, end           int
	declarationSHA256                [32]byte
	sourceNamespace, targetNamespace string
	namespaceChecked                 bool
	issues                           []string
}

func bindVFFSKCSchemaLinks(ctx context.Context, item vffClassificationPackage, schemas []vffSKCSchemaFact) ([]vffSKCSchemaLink, error) {
	byMember := map[string]*vffSKCSchemaFact{}
	for i := range schemas {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		schema := &schemas[i]
		raw, exists := item.files[schema.member]
		if !exists || byMember[schema.member] != nil || sha256.Sum256(raw) != schema.sha256 {
			return nil, fmt.Errorf("SKC schema link requires unique current schema bytes")
		}
		byMember[schema.member] = schema
	}
	var links []vffSKCSchemaLink
	for _, schema := range schemas {
		for i, dependency := range schema.dependencies {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			raw := item.files[schema.member]
			if dependency.begin < 0 || dependency.end <= dependency.begin || dependency.end > len(raw) {
				return nil, fmt.Errorf("invalid SKC dependency declaration range")
			}
			link := vffSKCSchemaLink{source: schema.vffSKCMemberFact, dependency: i, begin: dependency.begin, end: dependency.end, declarationSHA256: sha256.Sum256(raw[dependency.begin:dependency.end]), sourceNamespace: schema.targetNamespace}
			if vffSKCUnprovedNamespace(schema.targetNamespace, schema.namespaceDeclared) {
				link.issues = append(link.issues, "unsupported source namespace declaration")
			}
			if dependency.member == "" {
				link.issues = append(link.issues, "dependency has no exact local member binding")
				links = append(links, link)
				continue
			}
			target := byMember[dependency.member]
			if target == nil || target.sha256 != dependency.sha256 {
				return nil, fmt.Errorf("SKC dependency candidate differs from current schema")
			}
			link.target, link.targetNamespace = target.vffSKCMemberFact, target.targetNamespace
			if vffSKCUnprovedNamespace(target.targetNamespace, target.namespaceDeclared) {
				link.issues = append(link.issues, "unsupported target namespace declaration")
			}
			switch dependency.kind {
			case "import":
				if vffSKCUnprovedNamespace(dependency.namespace, dependency.namespaceDeclared) {
					link.issues = append(link.issues, "unsupported import namespace declaration")
				}
				if dependency.namespace == schema.targetNamespace {
					link.issues = append(link.issues, "import cannot bind the source namespace")
				}
				if dependency.namespace != target.targetNamespace || dependency.namespaceDeclared != target.namespaceDeclared {
					link.issues = append(link.issues, "import namespace does not match target declaration")
				}
			case "include", "redefine":
				if !target.namespaceDeclared && schema.namespaceDeclared {
					link.issues = append(link.issues, "chameleon namespace adoption not proved")
				} else if schema.targetNamespace != target.targetNamespace || schema.namespaceDeclared != target.namespaceDeclared {
					link.issues = append(link.issues, "included namespace differs from source declaration")
				}
				if dependency.kind == "redefine" {
					link.issues = append(link.issues, "redefinition semantics not proved")
				}
			default:
				return nil, fmt.Errorf("unsupported SKC dependency link kind")
			}
			link.namespaceChecked = len(link.issues) == 0
			links = append(links, link)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return links, nil
}

func vffSKCUnprovedNamespace(value string, declared bool) bool {
	return declared && value == "" || strings.Trim(value, " \t\r\n") != value
}
