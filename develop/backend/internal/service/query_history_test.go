package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/protection"
)

type historyReadGate struct {
	allowDevelopProtectionGate
	reads   int
	expired bool
}

func (g *historyReadGate) ReadQueryHistory(_ context.Context, tenantID int64, history *dataprotection.HistoricalQueryResult, subject dataprotection.SubjectReference) (*dataprotection.HistoricalQueryReadResult, error) {
	g.reads++
	if tenantID != 7 || subject.Type != "user" || subject.ID != "9" {
		return nil, protection.ErrResultRequired
	}
	phone := "13812345678"
	var expires *time.Time
	if g.expired {
		phone = "138****5678"
	} else {
		until := time.Now().Add(time.Hour)
		expires = &until
	}
	return &dataprotection.HistoricalQueryReadResult{QueryResult: plugin.QueryResult{Columns: []string{"phone"}, Rows: []map[string]interface{}{{"phone": phone}}}, PlaintextExpiresAt: expires}, nil
}

func TestProfessionalHistoryAlwaysRechecksUserAndNeverReturnsPrivateSnapshot(t *testing.T) {
	gate := &historyReadGate{}
	executor := &DevExecutor{sqlEngine: &SQLEngineService{protectionGate: gate}}
	actor := int64(9)
	item := &commonExecution.TaskExecution{ExecutionID: "history", TenantID: 7, Module: "develop", TaskType: "query", Status: "success", ActorPrincipalID: &actor, Metadata: commonModels.JSONMap{
		"result":                           map[string]interface{}{"result_kind": "table", "columns": []string{"phone"}, "summary": map[string]interface{}{"preview_rows": []map[string]interface{}{{"phone": "138****5678"}}}},
		protection.QueryHistoryMetadataKey: dataprotection.HistoricalQueryResult{SchemaVersion: dataprotection.HistoricalQueryResultSchemaV1, TenantID: 7, Subject: dataprotection.SubjectReference{Type: "user", ID: "9"}, Result: plugin.QueryResult{Rows: []map[string]interface{}{{"phone": "13812345678"}}}},
	}}
	ctx := WithExecutionAuthContext(t.Context(), authtest.NewTenantUserAuthContext("7", "9", []string{"develop.task.read", "develop.data_read.execute"}))
	for _, expired := range []bool{false, true} {
		gate.expired = expired
		response, err := executor.protectedExecutionResponse(ctx, item)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(response)
		if strings.Contains(string(encoded), protection.QueryHistoryMetadataKey) {
			t.Fatal("private snapshot exposed")
		}
		if strings.Contains(string(encoded), "13812345678") == expired {
			t.Fatalf("current grant not reflected: expired=%v", expired)
		}
		if strings.Contains(string(encoded), "plaintext_expires_at") == expired {
			t.Fatal("current plaintext deadline not reflected")
		}
	}
	if gate.reads != 2 {
		t.Fatal("history was served without fresh protection")
	}
	if strings.Contains(string(mustJSON(t, commonExecution.Observe(item))), "13812345678") {
		t.Fatal("generic execution observation exposed plaintext history")
	}
	if _, err := executor.protectedExecutionResponse(t.Context(), item); err == nil {
		t.Fatal("history accepted an unverified user")
	}
	if strings.Contains(string(mustJSON(t, item.Metadata["result"])), "13812345678") {
		t.Fatal("public persisted result mutated")
	}
}

func mustJSON(t *testing.T, value interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
