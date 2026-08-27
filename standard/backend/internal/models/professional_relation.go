package models

const ProfessionalRelationsSchemaVersion = "addp.professional_relations/v1"

type ProfessionalResourceKey struct {
	OwnerModule  string `json:"owner_module"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

type ProfessionalRelationNode struct {
	ProfessionalResourceKey
	Name   string `json:"name,omitempty"`
	Code   string `json:"code,omitempty"`
	Status string `json:"status,omitempty"`
	Kind   string `json:"kind,omitempty"`
}

type ProfessionalRelationEdge struct {
	ID           string                  `json:"id"`
	RelationKind string                  `json:"relation_kind"`
	Source       ProfessionalResourceKey `json:"source"`
	Target       ProfessionalResourceKey `json:"target"`
	Coefficient  *float64                `json:"coefficient,omitempty"`
	Note         string                  `json:"note,omitempty"`
}

type ProfessionalRelationsResponse struct {
	SchemaVersion string                     `json:"schema_version"`
	Subject       ProfessionalResourceKey    `json:"subject"`
	Nodes         []ProfessionalRelationNode `json:"nodes"`
	Edges         []ProfessionalRelationEdge `json:"edges"`
	Truncated     bool                       `json:"truncated"`
}
