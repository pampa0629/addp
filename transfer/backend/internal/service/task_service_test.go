package service

import (
	"testing"

	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/csv"
	_ "github.com/addp/common/format/plugins/pdf"
)

func TestValidateNewTaskConfigAcceptsRawCopy(t *testing.T) {
	err := validateNewTaskConfig(map[string]interface{}{
		"mode": "batch",
		"source": map[string]interface{}{
			"engine":         map[string]interface{}{"scope": "system", "id": 1},
			"resource":       map[string]interface{}{"kind": "object", "path": map[string]interface{}{"bucket": "docs", "path": "a.pdf"}},
			"data_type":      "document",
			"representation": "encoded",
			"format":         string(format.FormatPDF),
		},
		"target": map[string]interface{}{
			"engine":         map[string]interface{}{"scope": "system", "id": 2},
			"resource":       map[string]interface{}{"kind": "file", "path": map[string]interface{}{"path": "backup/a.pdf"}},
			"representation": "encoded",
			"policy":         map[string]interface{}{"write_mode": "overwrite"},
		},
	}, 1000)
	if err != nil {
		t.Fatalf("validateNewTaskConfig() error = %v", err)
	}
}

func TestValidateNewTaskConfigStillAcceptsTableTransfer(t *testing.T) {
	err := validateNewTaskConfig(map[string]interface{}{
		"mode":       "batch",
		"batch_size": 100,
		"source": map[string]interface{}{
			"engine":         map[string]interface{}{"scope": "system", "id": 1},
			"resource":       map[string]interface{}{"kind": "native_table", "path": map[string]interface{}{"schema": "public", "table": "roads"}},
			"data_type":      "table",
			"representation": "native",
		},
		"target": map[string]interface{}{
			"engine":         map[string]interface{}{"scope": "system", "id": 2},
			"resource":       map[string]interface{}{"kind": "file", "path": map[string]interface{}{"path": "exports/roads.csv"}},
			"data_type":      "table",
			"representation": "encoded",
			"format":         string(format.FormatCSV),
			"policy":         map[string]interface{}{"write_mode": "overwrite"},
		},
	}, 1000)
	if err != nil {
		t.Fatalf("validateNewTaskConfig() error = %v", err)
	}
}
