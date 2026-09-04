package service

import (
	"context"
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/transfer/internal/executor"
)

func TestPrepareBoundedTableSourceProtectionUsesPreparedQueryAcrossEngines(t *testing.T) {
	tests := []struct {
		name         string
		kind         executor.TableEndpointKind
		wantPrepare  int
		wantResource int
	}{
		{name: "native", kind: executor.TableEndpointNative, wantPrepare: 1},
		{name: "query", kind: executor.TableEndpointQuery, wantPrepare: 1},
		{name: "encoded", kind: executor.TableEndpointEncoded, wantResource: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gate := &recordingSourceProtectionGate{}
			if _, err := prepareBoundedTableSourceProtection(t.Context(), gate, 7, map[string]interface{}{"source": map[string]interface{}{"locator": "fixture"}}, test.kind); err != nil {
				t.Fatal(err)
			}
			if gate.prepareCalls != test.wantPrepare || gate.resourceCalls != test.wantResource {
				t.Fatalf("calls = prepare:%d resource:%d", gate.prepareCalls, gate.resourceCalls)
			}
		})
	}
}

type recordingSourceProtectionGate struct {
	prepareCalls  int
	resourceCalls int
}

func (g *recordingSourceProtectionGate) RequireSourceConfig(context.Context, uint, map[string]interface{}) error {
	g.resourceCalls++
	return nil
}

func (g *recordingSourceProtectionGate) PrepareBoundedTableProtection(context.Context, uint, map[string]interface{}) (executor.TableSourceProtector, error) {
	g.prepareCalls++
	return nil, nil
}

func (g *recordingSourceProtectionGate) PrepareBoundedEncodedRecordProtection(context.Context, uint, map[string]interface{}, []datatype.FieldInfo) (engineplugin.EncodedRecordTransform, error) {
	return nil, nil
}
