package service

import "testing"

func TestParseMermaidERRejectsUnsupportedOrIncompleteInput(t *testing.T) {
	tests := []string{
		"erDiagram\ncustomer {\n uuid id FK\n}",
		"erDiagram\ncustomer {\n uuid id\n}\ncustomer ||--o{ order : places",
		"erDiagram\ncustomer {\n uuid id\n}\nmissing ||--o{ customer : places",
	}
	for _, input := range tests {
		if _, err := ParseMermaidER(input); err == nil {
			t.Fatalf("expected parser to reject input: %s", input)
		}
	}
}

func TestParseMermaidERPreservesAttributeTypeAndRelation(t *testing.T) {
	parsed, err := ParseMermaidER(`erDiagram
customer {
  bigint id PK
  string display_name
}
order {
  bigint id PK
}
customer ||--o{ order : places`)
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
	parsed, err := ParseMermaidER(`erDiagram
%% addp:entity {"code":"customer","name":"Customer Display"}
customer {
  %% addp:attribute {"entity":"customer","column":"display_name","name":"Display Name","nullable":false}
  string display_name
}`)
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
