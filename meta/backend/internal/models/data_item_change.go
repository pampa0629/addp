package models

import "time"

const DataItemChangesSchemaVersion = "meta.data_item_changes/v1"

type DataItemChange struct {
	ChangeID       string    `json:"change_id"`
	Operation      string    `json:"operation"`
	SourceIdentity string    `json:"source_identity"`
	SourceVersion  string    `json:"source_version"`
	ObservedAt     time.Time `json:"observed_at"`
	Snapshot       JSONMap   `json:"snapshot"`
}

type DataItemChangesResponse struct {
	SchemaVersion string           `json:"schema_version"`
	Changes       []DataItemChange `json:"changes"`
	NextCursor    string           `json:"next_cursor"`
	HasMore       bool             `json:"has_more"`
}
