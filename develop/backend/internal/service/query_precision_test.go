package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
)

type preciseResultReadGate struct{ allowDevelopProtectionGate }

func (*preciseResultReadGate) ReadQueryResult(_ context.Context, _ int64, snapshot *dataprotection.AuthorizedQueryResult, _ dataprotection.SubjectReference, _ *dataprotection.QueryRoleSelection) (*dataprotection.AuthorizedQueryReadResult, error) {
	until := time.Now().Add(time.Minute)
	return &dataprotection.AuthorizedQueryReadResult{QueryResult: snapshot.Result, PlaintextExpiresAt: &until}, nil
}

func TestCurrentQueryResultRetainsSchemaThroughCacheAndExactWireProjection(t *testing.T) {
	cache := newQueryResultCache()
	executor := &DevExecutor{queryResults: cache, sqlEngine: &SQLEngineService{protectionGate: &preciseResultReadGate{}}}
	raw := authorizedResultFixture()
	raw.Result.Columns = []string{"id", "small", "amount"}
	raw.Result.Fields = []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "small", Type: datatype.FieldTypeInt}, {Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 38, Scale: 18}}
	raw.Result.Rows = []map[string]interface{}{{"id": int64(9007199254740993), "small": int64(2), "amount": "12345678901234567890.123456789012345678"}}
	if err := cache.put("current", 1, "generation", raw, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cache.remove("current", "generation") })
	ctx := WithExecutionAuthContext(t.Context(), authtest.NewTenantUserAuthContext("7", "9", []string{"develop.task.read", "develop.data_read.execute"}))
	response, err := executor.queryResultResponse(ctx, professionalExecutionResponse(resultFixture()), 7, "")
	if err != nil {
		t.Fatal(err)
	}
	result := response.Metadata["result"].(map[string]interface{})
	rows := result["summary"].(map[string]interface{})["preview_rows"].([]map[string]interface{})
	if rows[0]["id"] != "9007199254740993" || rows[0]["amount"] != raw.Result.Rows[0]["amount"] {
		t.Fatal("reauthorized result lost exact values")
	}
	fields := result["fields"].([]datatype.FieldInfo)
	if fields[0].Type != datatype.FieldTypeBigInt || fields[2].Precision != 38 || fields[2].Scale != 18 {
		t.Fatal("cache lost real output schema")
	}
	if raw.Result.Rows[0]["id"] != int64(9007199254740993) {
		t.Fatal("wire projection changed Provider values")
	}
}

func TestQueryObservationRejectsRoundedValuesAndKeepsProtectedText(t *testing.T) {
	executor := &DevExecutor{}
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}
	if _, _, err := executor.queryResult(1, []string{"id"}, fields, []map[string]interface{}{{"id": float64(9007199254740993)}}, 1, SQLExecutionEffectRead, "table", nil); err == nil {
		t.Fatal("rounded bigint accepted")
	}
	result, _, err := executor.queryResult(1, []string{"id"}, fields, []map[string]interface{}{{"id": "***"}}, 1, SQLExecutionEffectRead, "table", nil)
	if err != nil || result["summary"].(map[string]interface{})["preview_rows"].([]map[string]interface{})[0]["id"] != "***" {
		t.Fatal("masked text changed")
	}
}
