package service

import (
	"encoding/csv"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/service/internal/models"
)

func TestQueryCSVPreservesMaskedValuesAndEmptyCells(t *testing.T) {
	result := &models.QueryExecutionResult{
		Fields: []string{"phone", "note", "count", "enabled"},
		Data: []map[string]interface{}{
			{"phone": "***********", "note": "含逗号,及\n换行", "count": 0, "enabled": false},
			{"phone": nil, "note": "<nil>", "count": 2, "enabled": true},
			{"note": "缺失字段"},
		},
	}
	encoded, err := (&QueryExecutorService{}).FormatAsCSV(result)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(encoded))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"phone", "note", "count", "enabled"},
		{"***********", "含逗号,及\n换行", "0", "false"},
		{"", "<nil>", "2", "true"},
		{"", "缺失字段", "", ""},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("CSV changed protected, empty or literal values: got %#v; want %#v", rows, want)
	}
}
