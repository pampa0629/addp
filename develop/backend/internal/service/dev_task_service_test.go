package service

import (
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
)

func TestValidateDevTaskContentAcceptsCanonicalQuery(t *testing.T) {
	err := validateDevTaskContent(commonExecution.TaskTypeQuery, map[string]interface{}{
		"query":      "SELECT 1",
		"query_type": "sql",
	})
	if err != nil {
		t.Fatalf("expected canonical query content to pass, got %v", err)
	}
}

func TestValidateDevTaskContentRejectsLegacyQuerySQL(t *testing.T) {
	err := validateDevTaskContent(commonExecution.TaskTypeQuery, map[string]interface{}{
		"sql":        "SELECT 1",
		"query_type": "sql",
	})
	if err == nil {
		t.Fatal("expected legacy content.sql to be rejected")
	}
	if !strings.Contains(err.Error(), "content.query") {
		t.Fatalf("expected content.query error, got %v", err)
	}
}

func TestValidateDevTaskContentAcceptsCanonicalWorkflow(t *testing.T) {
	err := validateDevTaskContent(commonExecution.TaskTypeWorkflow, map[string]interface{}{
		"workflow_definition": map[string]interface{}{
			"nodes": []interface{}{},
			"edges": []interface{}{},
		},
		"inputs": map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("expected canonical workflow content to pass, got %v", err)
	}
}

func TestValidateDevTaskContentRejectsLegacyWorkflowDef(t *testing.T) {
	err := validateDevTaskContent(commonExecution.TaskTypeWorkflow, map[string]interface{}{
		"workflow_def": map[string]interface{}{
			"nodes": []interface{}{},
			"edges": []interface{}{},
		},
	})
	if err == nil {
		t.Fatal("expected legacy content.workflow_def to be rejected")
	}
	if !strings.Contains(err.Error(), "content.workflow_definition") {
		t.Fatalf("expected content.workflow_definition error, got %v", err)
	}
}
