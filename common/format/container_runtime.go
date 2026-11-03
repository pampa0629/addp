package format

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
)

// ContainerRuntimeSource is the existing source-only container binding, with
// detached request values. Format plugins retain their own result validation.
type ContainerRuntimeSource struct {
	runtime engineplugin.WorkflowRuntimeProvider
	format  FormatType
	frozen  []byte
}

func BindContainerRuntimeSource(runtime engineplugin.WorkflowRuntimeProvider, conn engineplugin.ConnectionInfo, plan workflowaccess.SourcePlan, kind FormatType) (*ContainerRuntimeSource, error) {
	if runtime == nil || len(conn) == 0 || plan.Validate() != nil || plan.Source.Kind != workflowaccess.KindFile || plan.Source.Format != string(kind) {
		return nil, fmt.Errorf("%s requires a runtime and an exact source-only file plan", kind)
	}
	raw, err := json.Marshal(struct {
		Conn engineplugin.ConnectionInfo
		Plan workflowaccess.SourcePlan
	}{conn, plan})
	if err != nil {
		return nil, err
	}
	return &ContainerRuntimeSource{runtime: runtime, format: kind, frozen: raw}, nil
}

func (b *ContainerRuntimeSource) Invoke(ctx context.Context, operation string, params map[string]interface{}) (map[string]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b == nil || (operation != "inspect" && operation != "read_slice") {
		return nil, fmt.Errorf("invalid container read operation")
	}
	var input struct {
		Conn engineplugin.ConnectionInfo
		Plan workflowaccess.SourcePlan
	}
	if err := json.Unmarshal(b.frozen, &input); err != nil {
		return nil, err
	}
	request := make(map[string]interface{}, len(params)+1)
	for k, v := range params {
		request[k] = v
	}
	request["access_plan"] = input.Plan.JSONMap()
	result, err := b.runtime.InvokeOperator(ctx, input.Conn, string(b.format)+"."+operation, engineplugin.OperatorInvokeRequest{Params: request})
	if err != nil {
		return nil, err
	}
	if result == nil || result.Status != "success" {
		return nil, fmt.Errorf("%s runtime read did not succeed", b.format)
	}
	payload, ok := result.Result["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s runtime omitted result", b.format)
	}
	switch payload["error_code"] {
	case strings.ToUpper(string(b.format)) + "_SLICE_INVALID":
		return nil, ErrGridSliceSelection
	case strings.ToUpper(string(b.format)) + "_SOURCE_CHANGED":
		return nil, ErrContainerSourceChanged
	}
	if payload["format"] != string(b.format) {
		return nil, fmt.Errorf("container runtime format mismatch")
	}
	return payload, nil
}

func ValidContainerSourceVersion(value interface{}) bool {
	s, ok := value.(string)
	if !ok || len(s) != 71 || !strings.HasPrefix(s, "sha256:") {
		return false
	}
	for _, r := range s[7:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
