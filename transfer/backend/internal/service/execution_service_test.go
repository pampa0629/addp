package service

import (
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
)

func TestFinishErrorDetailsPreservesLogsOnSuccess(t *testing.T) {
	details, changed := finishErrorDetails(commonModels.JSONMap{
		"logs":    "batch=1\n",
		"message": "old error",
	}, models.ExecutionStatusSuccess, "")

	if !changed {
		t.Fatal("finishErrorDetails changed = false, want true")
	}
	if got := details["logs"]; got != "batch=1\n" {
		t.Fatalf("logs = %#v, want preserved logs", got)
	}
	if _, ok := details["message"]; ok {
		t.Fatalf("message still exists in details: %#v", details)
	}
}

func TestFinishErrorDetailsPreservesLogsOnFailure(t *testing.T) {
	details, changed := finishErrorDetails(commonModels.JSONMap{
		"logs": "batch=1\n",
	}, models.ExecutionStatusFailed, "failed to write target")

	if !changed {
		t.Fatal("finishErrorDetails changed = false, want true")
	}
	if got := details["logs"]; got != "batch=1\n" {
		t.Fatalf("logs = %#v, want preserved logs", got)
	}
	if got := details["message"]; got != "failed to write target" {
		t.Fatalf("message = %#v, want failure message", got)
	}
}
