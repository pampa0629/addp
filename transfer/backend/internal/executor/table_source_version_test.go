package executor

import (
	"context"
	"errors"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/production"
	"testing"
)

func TestTrackedProductionRequiresSourceVersionBeforeAnyIO(t *testing.T) {
	e := &TableTransferExecutor{}
	_, err := e.Execute(t.Context(), TableTransferPlan{Source: TableSourcePlan{Path: plugin.TabularItemPath(2, "schema", "public", "source")}, TargetPreparedCallback: func(context.Context, plugin.TableTargetPreparation) error {
		t.Fatal("target mutation entered")
		return nil
	}})
	if !errors.Is(err, production.ErrEvidence) {
		t.Fatal("missing source version reached Provider resolution", err)
	}
}
