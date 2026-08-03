package service

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/service/internal/models"
)

func TestCompileQueryPlanBindsFilterAndBuildsCompositeKeyset(t *testing.T) {
	t.Parallel()

	queryService := testPublishedQueryService()
	codec := newQueryTokenCodec([]byte("0123456789abcdef0123456789abcdef"))
	request := &models.QueryExecutionRequest{
		Select:  []string{"name"},
		Filter:  &models.QueryFilter{Field: "name", Op: "eq", Value: "x' OR 1=1 --"},
		OrderBy: []models.QueryOrder{{Field: "score", Direction: "desc"}},
		Page:    models.QueryPageRequest{Limit: 2},
	}
	plan, err := compileQueryPlan(queryService, request, queryProtocolREST, "postgresql", "SELECT id, name, score FROM public.items LIMIT 100", codec)
	if err != nil {
		t.Fatalf("compileQueryPlan() error = %v", err)
	}
	if strings.Contains(plan.SQL, "OR 1=1") || !strings.Contains(plan.SQL, `"name" = ?`) {
		t.Fatalf("filter was not parameterized: %s", plan.SQL)
	}
	if !strings.Contains(plan.SQL, `ORDER BY addp_source."score" DESC, addp_source."id" ASC LIMIT 3`) {
		t.Fatalf("unexpected order or limit: %s", plan.SQL)
	}
	if !reflect.DeepEqual(plan.Args, []interface{}{"x' OR 1=1 --"}) {
		t.Fatalf("args = %#v", plan.Args)
	}
	if !reflect.DeepEqual(plan.HiddenFields, []string{"score", "id"}) {
		t.Fatalf("hidden fields = %#v", plan.HiddenFields)
	}

	cursor, err := codec.encodeCursor(queryCursorPayload{
		ServiceID: queryService.ID, ServiceVersion: plan.ServiceVersion,
		QueryHash: plan.QueryHash, OrderBy: plan.OrderBy, Values: []interface{}{9.5, int64(7)},
	})
	if err != nil {
		t.Fatalf("encodeCursor() error = %v", err)
	}
	request.Page.Cursor = cursor
	nextPlan, err := compileQueryPlan(queryService, request, queryProtocolREST, "postgresql", "SELECT id, name, score FROM public.items LIMIT 100", codec)
	if err != nil {
		t.Fatalf("compileQueryPlan(cursor) error = %v", err)
	}
	if !strings.Contains(nextPlan.SQL, `(addp_source."score" < ?) OR (addp_source."score" = ? AND addp_source."id" > ?)`) {
		t.Fatalf("unexpected keyset predicate: %s", nextPlan.SQL)
	}
	if !reflect.DeepEqual(nextPlan.Args, []interface{}{"x' OR 1=1 --", 9.5, 9.5, int64(7)}) {
		t.Fatalf("cursor args = %#v", nextPlan.Args)
	}

	request.Select = []string{"id"}
	if _, err := compileQueryPlan(queryService, request, queryProtocolREST, "postgresql", "SELECT id, name, score FROM public.items", codec); !errors.Is(err, ErrInvalidQueryCursor) {
		t.Fatalf("query-shape mismatch error = %v, want ErrInvalidQueryCursor", err)
	}
}

func TestQueryTokenRejectsTamperingAndBindsCompositeFeatureID(t *testing.T) {
	t.Parallel()

	queryService := testPublishedQueryService()
	queryService.DataConfig["stable_key"] = []interface{}{"id", "name"}
	codec := newQueryTokenCodec([]byte("0123456789abcdef0123456789abcdef"))
	token, err := codec.encodeCursor(queryCursorPayload{ServiceID: queryService.ID})
	if err != nil {
		t.Fatalf("encodeCursor() error = %v", err)
	}
	tampered := token[:len(token)-1] + "A"
	if _, err := codec.decodeCursor(tampered); !errors.Is(err, ErrInvalidQueryCursor) {
		t.Fatalf("tampered cursor error = %v", err)
	}

	executor := &QueryExecutorService{tokenCodec: codec}
	featureID, err := executor.encodeFeatureID(queryService, map[string]interface{}{"id": int64(3), "name": "alpha"})
	if err != nil {
		t.Fatalf("encodeFeatureID() error = %v", err)
	}
	filter, err := executor.DecodeFeatureID(queryService, featureID)
	if err != nil {
		t.Fatalf("DecodeFeatureID() error = %v", err)
	}
	if len(filter.And) != 2 || filter.And[0].Field != "id" || filter.And[1].Field != "name" {
		t.Fatalf("feature filter = %#v", filter)
	}
}

func TestFinalizeResultUsesLimitPlusOneAndRemovesHiddenFields(t *testing.T) {
	t.Parallel()

	queryService := testPublishedQueryService()
	executor := &QueryExecutorService{tokenCodec: newQueryTokenCodec([]byte("0123456789abcdef0123456789abcdef"))}
	plan := &compiledQueryPlan{
		Limit: 2, SelectedFields: []string{"name"}, HiddenFields: []string{"id"},
		OrderBy:   []models.QueryOrder{{Field: "id", Direction: "asc"}},
		QueryHash: "hash", ServiceVersion: "revision-1",
	}
	result, err := executor.finalizeResult(queryService, plan, []map[string]interface{}{
		{"id": int64(1), "name": "one"},
		{"id": int64(2), "name": "two"},
		{"id": int64(3), "name": "three"},
	})
	if err != nil {
		t.Fatalf("finalizeResult() error = %v", err)
	}
	if !result.Page.HasMore || result.Page.NextCursor == "" || len(result.Data) != 2 {
		t.Fatalf("page result = %#v", result.Page)
	}
	if _, exists := result.Data[0]["id"]; exists {
		t.Fatalf("hidden stable key leaked: %#v", result.Data[0])
	}
	if len(result.FeatureIDs) != 2 || result.FeatureIDs[0] == "" {
		t.Fatalf("feature IDs = %#v", result.FeatureIDs)
	}
}

func TestPublishedSQLStableKeyBecomesNonNullOutputContract(t *testing.T) {
	t.Parallel()

	snapshot := &models.QueryServiceDependencySnapshot{Table: &datatype.TableInfo{Fields: []datatype.FieldInfo{
		{Name: "business_key", Type: datatype.FieldTypeString, Nullable: true},
	}}}
	stableKey, err := publishedStableKey("sql", models.JSONB{"stable_key": []interface{}{"business_key"}}, snapshot)
	if err != nil {
		t.Fatalf("publishedStableKey() error = %v", err)
	}
	if !reflect.DeepEqual(stableKey, []string{"business_key"}) || snapshot.Table.Fields[0].Nullable {
		t.Fatalf("stable key = %#v, field = %#v", stableKey, snapshot.Table.Fields[0])
	}
}

func TestFederatedQueryOptionsLoadsSpatialOnlyForPublishedGeometry(t *testing.T) {
	t.Parallel()

	queryService := testPublishedQueryService()
	plan := &compiledQueryPlan{Limit: 9, Args: []interface{}{int64(4)}}
	options := federatedQueryOptions(queryService, plan)
	if options.Spatial || options.Limit != 10 || !reflect.DeepEqual(options.Args, plan.Args) {
		t.Fatalf("non-spatial options = %#v", options)
	}

	snapshot := queryService.SourceSnapshot()
	snapshot.Spatial = &datatype.SpatialInfo{
		GeometryColumns:       []datatype.GeometryColumnInfo{{Name: "shape"}},
		PrimaryGeometryColumn: "shape",
	}
	queryService.DataConfig[models.QueryServiceSourceSnapshotKey] = commonJSON.MapFromStruct(snapshot)
	options = federatedQueryOptions(queryService, plan)
	if !options.Spatial {
		t.Fatalf("spatial options = %#v", options)
	}
}

func testPublishedQueryService() *models.QueryService {
	snapshot := &models.QueryServiceDependencySnapshot{
		DependencyHash: "revision-1",
		Table: &datatype.TableInfo{Kind: "query", Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt},
			{Name: "name", Type: datatype.FieldTypeString},
			{Name: "score", Type: datatype.FieldTypeDouble},
		}},
	}
	return &models.QueryService{
		ID: 17, MaxFeatures: 100,
		DataConfig: models.JSONB{
			models.QueryServiceSourceSnapshotKey: commonJSON.MapFromStruct(snapshot),
			"stable_key":                         []interface{}{"id"}, "default_fields": []interface{}{"id", "name"},
			"filterable_fields": []interface{}{"name", "score"},
		},
	}
}
