package service

import (
	"fmt"
	"strings"
	"testing"
)

func mermaidMarkdown(scope string, domainCode string, code string) string {
	metadata := fmt.Sprintf(`%%%% addp:document {"format":"addp.model.er/v2","scope":%q`, scope)
	if domainCode != "" {
		metadata += fmt.Sprintf(`,"domain_code":%q`, domainCode)
	}
	metadata += "}\n"
	return "# ADDP Entity Relationship Diagram\n\n```mermaid\nerDiagram\n" + metadata + code + "\n```\n"
}

func TestParseMermaidERRejectsUnsupportedOrIncompleteInput(t *testing.T) {
	tests := []string{
		mermaidMarkdown("all", "", "customer {\n uuid id FK\n}"),
		mermaidMarkdown("all", "", "customer {\n uuid id\n}\ncustomer ||--o{ order : places"),
		mermaidMarkdown("all", "", "customer {\n uuid id\n}\nmissing ||--o{ customer : places"),
		"erDiagram\ncustomer {\n bigint id PK\n}",
		"```mermaid\nerDiagram\ncustomer {\n bigint id PK\n}\n```",
	}
	for _, input := range tests {
		if _, err := ParseMermaidER(input); err == nil {
			t.Fatalf("expected parser to reject input: %s", input)
		}
	}
}

func TestParseMermaidERPreservesAttributeTypeAndRelation(t *testing.T) {
	parsed, err := ParseMermaidER(mermaidMarkdown("all", "", `customer {
  bigint id PK
  string display_name
}
order {
  bigint id PK
}
customer ||--o{ order : places`))
	if err != nil {
		t.Fatalf("parse mermaid: %v", err)
	}
	if len(parsed.Entities) != 2 || parsed.Entities[0].Attributes[0].Type != "bigint" {
		t.Fatalf("parser lost entity or attribute type: %+v", parsed)
	}
	if len(parsed.Relations) != 1 || ConvertRelationType(parsed.Relations[0].Symbol) != "one_to_many" {
		t.Fatalf("parser lost relation: %+v", parsed.Relations)
	}
}

func TestParseMermaidERRestoresADDPDisplayMetadata(t *testing.T) {
	parsed, err := ParseMermaidER(mermaidMarkdown("domain", "sales", `%% addp:entity {"code":"customer","name":"Customer Display","domain_code":"sales","description":""}
customer {
  %% addp:attribute {"entity":"customer","column":"display_name","name":"Display Name","nullable":false}
  string display_name
}`))
	if err != nil {
		t.Fatalf("parse mermaid metadata: %v", err)
	}
	entity := parsed.Entities[0]
	if entity.DisplayName != "Customer Display" {
		t.Fatalf("unexpected entity display name: %q", entity.DisplayName)
	}
	attribute := entity.Attributes[0]
	if attribute.DisplayName != "Display Name" || attribute.Nullable {
		t.Fatalf("unexpected attribute metadata: %+v", attribute)
	}
}

func TestParseMermaidERRejectsValuesBeyondDatabaseLengths(t *testing.T) {
	longCode := strings.Repeat("a", 101)
	longName := strings.Repeat("界", 201)
	tests := []string{
		mermaidMarkdown("all", "", fmt.Sprintf("%s {\n bigint id PK\n}", longCode)),
		mermaidMarkdown("all", "", fmt.Sprintf("%%%% addp:entity {\"code\":\"customer\",\"name\":\"%s\"}\ncustomer {\n bigint id PK\n}", longName)),
		mermaidMarkdown("all", "", fmt.Sprintf("customer {\n bigint id PK\n}\norder {\n bigint order_id PK\n}\ncustomer ||--o{ order : %s", longName)),
	}
	for _, input := range tests {
		if _, err := ParseMermaidER(input); err == nil {
			t.Fatal("expected parser to reject overlong value")
		}
	}
}

func TestParseMermaidERRequiresDocumentScopeToMatchEntities(t *testing.T) {
	input := mermaidMarkdown("domain", "sales", `%% addp:entity {"code":"customer","name":"Customer","domain_code":"marketing","description":""}
customer {
  bigint id PK
}`)
	if _, err := ParseMermaidER(input); err == nil {
		t.Fatal("expected domain-scoped document to reject an entity from another domain")
	}
}

func TestParseMermaidERRejectsV1NumericReferenceMetadata(t *testing.T) {
	input := "# ADDP Entity Relationship Diagram\n\n```mermaid\nerDiagram\n" +
		"  %% addp:document {\"format\":\"addp.model.er/v1\",\"scope\":\"domain\",\"domain_id\":7}\n" +
		"  customer {\n    bigint id PK\n  }\n```\n"
	if _, err := ParseMermaidER(input); err == nil {
		t.Fatal("expected v1 numeric reference metadata to be rejected")
	}
}

func TestParseMermaidERRejectsDuplicateRelationIdentity(t *testing.T) {
	input := mermaidMarkdown("all", "", `customer {
  bigint id PK
}
order {
  bigint id PK
}
customer ||--o{ order : places
customer ||--o{ order : places`)
	if _, err := ParseMermaidER(input); err == nil {
		t.Fatal("expected duplicate relation identity to be rejected")
	}
}
