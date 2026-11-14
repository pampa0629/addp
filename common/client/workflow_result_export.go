package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
)

// WorkflowResultExportSource binds a complete private table to its original
// professional result. It carries references only; rows never enter execution config.
type WorkflowResultExportSource struct {
	ExecutionID string                             `json:"execution_id"`
	Attempt     int                                `json:"attempt"`
	ExpiresAt   time.Time                          `json:"expires_at"`
	SHA256      string                             `json:"sha256"`
	Columns     []string                           `json:"columns"`
	RowCount    int                                `json:"row_count"`
	Targets     []dataprotection.ResourceReference `json:"targets"`
}

func (r *WorkflowResultExportSource) Validate() error {
	if r == nil || !validExportExecutionID(r.ExecutionID) || r.Attempt <= 0 || r.ExpiresAt.IsZero() || r.RowCount < 0 || r.RowCount > 1000 || len(r.Columns) == 0 || len(r.Targets) == 0 {
		return errors.New("invalid workflow result export source")
	}
	digest, err := hex.DecodeString(r.SHA256)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != r.SHA256 {
		return errors.New("invalid workflow result export digest")
	}
	seen := map[string]bool{}
	for _, c := range r.Columns {
		if c == "" || seen[c] {
			return errors.New("invalid workflow result columns")
		}
		seen[c] = true
	}
	for _, target := range r.Targets {
		if target.Validate() != nil || target.ComponentKey != "" {
			return errors.New("invalid workflow result source target")
		}
	}
	return nil
}

func WorkflowResultExportRows(table *plugin.WorkflowTableResult) ([]byte, error) {
	if table == nil || table.SchemaVersion != "addp.workflow.table-result/v1" || len(table.Rows) != table.RowCount || table.RowCount < 0 || table.RowCount > 1000 || len(table.Columns) == 0 {
		return nil, errors.New("invalid complete workflow table")
	}
	columns := map[string]bool{}
	for _, c := range table.Columns {
		if c == "" || columns[c] {
			return nil, errors.New("invalid complete workflow columns")
		}
		columns[c] = true
	}
	for _, row := range table.Rows {
		if len(row) != len(columns) {
			return nil, errors.New("invalid complete workflow row")
		}
		for c, v := range row {
			if !columns[c] {
				return nil, errors.New("invalid complete workflow field")
			}
			switch v.(type) {
			case nil, string, bool, json.Number, float64, float32, int, int64, int32, uint, uint64, uint32:
			default:
				return nil, errors.New("workflow table export requires scalar values")
			}
		}
	}
	data, err := json.Marshal(table.Rows)
	if err != nil || len(data) > 64<<10 {
		return nil, errors.New("workflow table export exceeds bound")
	}
	return data, nil
}

func WorkflowResultRowsDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
