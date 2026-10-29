package models

import "github.com/addp/common/engine/plugin"

type FrozenBuildMaterial struct {
	Material BuildMaterial `json:"material"`
	SHA256   string        `json:"sha256"`
	Size     int64         `json:"size"`
}

// BuildInput is immutable execution input, not an independently editable
// ontology revision. Material identities are hashes of the actual bytes.
type BuildInput struct {
	Schema          string                   `json:"schema"`
	Task            BuildTask                `json:"task"`
	TenantID        uint                     `json:"tenant_id"`
	GraphID         uint                     `json:"graph_id"`
	EngineID        uint                     `json:"engine_id"`
	EngineVersion   int64                    `json:"engine_version"`
	CatalogPath     plugin.EngineCatalogPath `json:"catalog_path"`
	IndexManagement string                   `json:"index_management"`
	Ontology        Ontology                 `json:"ontology"`
	Materials       []FrozenBuildMaterial    `json:"materials"`
}

const BuildInputSchema = "addp.graph-build-input/v1"
