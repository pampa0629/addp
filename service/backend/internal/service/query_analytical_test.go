package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/service/internal/models"
)

func TestAnalyticalResultRequestsUseFrozenPackage(t *testing.T) {
	for _, engineType := range []string{"postgresql", "mysql", "metric_test_extension"} {
		t.Run(engineType, func(t *testing.T) {
			frozen, engine := metricServiceFixture(t, engineType)
			snapshot := &models.QueryServiceDependencySnapshot{CapturedAt: time.Now(), MetricSource: &frozen}
			snapshot.DependencyHash = queryServiceDependencyHash(snapshot)
			service := &models.QueryService{ID: 71, TenantID: 7, ConfigType: "analytical", MaxFeatures: 5, Status: "active", Protocols: models.JSONB{"rest_api": map[string]interface{}{"enabled": true, "formats": []interface{}{"json", "csv"}}}, DataConfig: models.JSONB{models.QueryServiceSourceSnapshotKey: queryServiceSnapshotPayload(snapshot)}}
			if err := validateAnalyticalPublication(service); err != nil {
				t.Fatal(err)
			}
			descriptor, err := BuildQueryConsumerDescriptor(service, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range descriptor.InputContract.Fields {
				if !field.Filterable || !containsConsumerValue(field.Operators, "eq") || containsConsumerValue(field.Operators, "contains") {
					t.Fatalf("published result filter contract is missing or exceeds neutral operators: %+v", field)
				}
			}
			if filterFieldAllowed(service, queryProtocolREST, "unpublished_source_field", "eq") {
				t.Fatal("source field exposed as result filter")
			}
			service.DataConfig["filterable_fields"] = []interface{}{"value"}
			if validateAnalyticalPublication(service) == nil {
				t.Fatal("duplicate filter field contract accepted")
			}
			delete(service.DataConfig, "filterable_fields")
			codec := newQueryTokenCodec([]byte(strings.Repeat("k", 32)))
			request := &models.QueryExecutionRequest{Parameters: map[string]interface{}{"subject_id": "A' secret value", "start_date": "2026-01-01", "end_date": "2027-01-01", "grain": "total"}, Select: []string{"value"}, Page: models.QueryPageRequest{Limit: 1}, Filter: &models.QueryFilter{And: []models.QueryFilter{{Field: "value", Op: "gte", Value: json.Number("1")}, {Field: "bucket", Op: "eq", Value: "2026-01-01"}}}}
			prepared, query, err := compileAnalyticalQuery(service, request, queryProtocolREST, engine.AsEngine(), codec)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(query.Query, "secret value") || prepared.Limit != 1 || len(prepared.HiddenFields) != 2 {
				t.Fatalf("unbound request or missing cursor fields: %#v", prepared)
			}
			cursor, err := codec.encodeCursor(queryCursorPayload{ServiceID: 71, ServiceVersion: prepared.ServiceVersion, QueryHash: prepared.QueryHash, OrderBy: prepared.OrderBy, Values: []interface{}{"A' secret value", "2026-01-01"}})
			if err != nil {
				t.Fatal(err)
			}
			request.Page.Cursor = cursor
			if _, _, err = compileAnalyticalQuery(service, request, queryProtocolREST, engine.AsEngine(), codec); err != nil {
				t.Fatal(err)
			}
			request.Parameters["subject_id"] = "B"
			if _, _, err = compileAnalyticalQuery(service, request, queryProtocolREST, engine.AsEngine(), codec); err == nil {
				t.Fatal("cursor reused for changed parameters")
			}
			service.SqlQuery = "SELECT 1"
			if err = validateAnalyticalPublication(service); err == nil {
				t.Fatal("mixed SQL and plan accepted")
			}
		})
	}
}
func TestAnalyticalLiteralsPreserveExactValues(t *testing.T) {
	for _, text := range []string{"0.123456789012345678", "99999999999999999999.999999999999999999"} {
		literal, err := analyticalLiteral(datatype.FieldTypeDecimal, json.Number(text))
		if err != nil || literal.Text != text {
			t.Fatalf("decimal changed: %v %v", literal, err)
		}
	}
	for _, value := range []interface{}{1.1, nil, true} {
		if _, err := analyticalLiteral(datatype.FieldTypeDecimal, value); err == nil {
			t.Fatalf("inexact value accepted: %v", value)
		}
	}
}
