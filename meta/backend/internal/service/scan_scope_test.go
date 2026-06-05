package service

import (
	"testing"

	"github.com/addp/meta/internal/scanflow"
)

func TestResolveScanScopeDelegatesToResolver(t *testing.T) {
	t.Parallel()

	scanSvc := &ScanService{}
	scope, err := scanSvc.ResolveScanScope(3, scanflow.Options{
		EngineID:  7,
		ScanDepth: "DEEP",
		Source:    " transfer ",
	})
	if err != nil {
		t.Fatalf("ResolveScanScope() error = %v", err)
	}

	if scope.EngineID != 7 || scope.Mode != scanflow.ModeEngine || scope.ScanDepth != "deep" || scope.Source != "transfer" {
		t.Fatalf("scope scalar fields = %#v", scope)
	}
}
