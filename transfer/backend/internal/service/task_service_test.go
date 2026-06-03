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
			"locator":        "addp://engine/1/path/docs/a.pdf?type=object",
			"data_type":      "document",
			"representation": "encoded",
			"format":         string(format.FormatPDF),
		},
		"target": map[string]interface{}{
			"locator":        "addp://engine/2/path/backup/a.pdf?type=file",
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
			"locator":        "addp://engine/1/path/public/roads?type=table",
			"data_type":      "table",
			"representation": "native",
		},
		"target": map[string]interface{}{
			"locator":        "addp://engine/2/path/exports/roads.csv?type=file",
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
