package format_test

import (
	"context"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
)

type mutatingContainerRuntime struct {
	engineplugin.WorkflowRuntimeProvider
	calls int
	t     *testing.T
}

func (r *mutatingContainerRuntime) InvokeOperator(_ context.Context, conn engineplugin.ConnectionInfo, name string, request engineplugin.OperatorInvokeRequest) (*engineplugin.OperatorInvokeResult, error) {
	r.calls++
	if conn["host"] != "frozen" || name != "grib.inspect" {
		r.t.Fatal("binding mutated", conn, name)
	}
	plan := request.Params["access_plan"].(commonModels.JSONMap)
	if plan["source"].(commonModels.JSONMap)["access"].(commonModels.JSONMap)["path"] != "/source/one.grb2" {
		r.t.Fatal("source mutated")
	}
	conn["host"] = "mutated"
	plan["source"].(commonModels.JSONMap)["format"] = "other"
	return &engineplugin.OperatorInvokeResult{Status: "success", Result: map[string]interface{}{"result": map[string]interface{}{"format": "grib"}}}, nil
}
func TestContainerRuntimeBindingRemainsImmutableAcrossCalls(t *testing.T) {
	runtime := &mutatingContainerRuntime{t: t}
	conn := engineplugin.ConnectionInfo{"host": "frozen"}
	plan, err := workflowaccess.NewSourcePlan(workflowaccess.Source{Kind: "file", Format: "grib", Access: workflowaccess.Access{Method: "mounted_path", Path: "/source/one.grb2"}})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := format.BindContainerRuntimeSource(runtime, conn, plan, format.FormatGRIB)
	if err != nil {
		t.Fatal(err)
	}
	conn["host"] = "other"
	plan.Source.Access.Path = "/other"
	for range 2 {
		if _, err := bound.Invoke(context.Background(), "inspect", nil); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bound.Invoke(ctx, "inspect", nil); err == nil || runtime.calls != 2 {
		t.Fatal("cancelled read reached runtime")
	}
	if _, err := bound.Invoke(context.Background(), "export", nil); err == nil {
		t.Fatal("write operation exposed")
	}
}
