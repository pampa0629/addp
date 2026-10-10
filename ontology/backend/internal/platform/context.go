// Package platform owns code-published platform semantics, not Tenant revisions
// or the Tool Manifest's authorization and execution contracts.
package platform

import _ "embed"

//go:embed transfer.json
var transferDefinition []byte

//go:embed transfer.review.json
var transferReview []byte

type Concept struct {
	ID   string            `json:"id"`
	Name map[string]string `json:"name"`
}

type Relation struct {
	From string `json:"from"`
	Kind string `json:"kind"`
	To   string `json:"to"`
}

type Requirement struct {
	ID    string   `json:"id"`
	Tools []string `json:"tools"`
}

type Operation struct {
	Owner           string   `json:"owner"`
	Skill           string   `json:"skill"`
	Tool            string   `json:"tool"`
	InputsRequired  []string `json:"inputs_required"`
	Effects         []string `json:"effects"`
	ExcludedEffects []string `json:"excluded_effects"`
}

type Context struct {
	SchemaVersion string        `json:"schema_version"`
	KnowledgeKind string        `json:"knowledge_kind"`
	Capability    string        `json:"capability"`
	Revision      uint64        `json:"revision"`
	Digest        string        `json:"digest"`
	Availability  string        `json:"availability"`
	Concepts      []Concept     `json:"concepts"`
	Relations     []Relation    `json:"relations"`
	Operation     Operation     `json:"operation"`
	Requirements  []Requirement `json:"requirements"`
}

const CatalogMaxItems = 32
const CatalogMaxBytes = 128 * 1024

type Catalog struct {
	SchemaVersion string    `json:"schema_version"`
	Capabilities  []Context `json:"capabilities"`
}

// CompileTransferRelease is the deployment-only source entry. Runtime semantic
// reads restore the authoritative active PG revision, never this embedded file.
func CompileTransferRelease() (*Snapshot, error) { return Compile(transferDefinition, transferReview) }
