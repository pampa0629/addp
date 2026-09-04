package service

import (
	"bytes"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
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
	plan, err := compileQueryPlan(queryService, request, queryProtocolREST, "postgresql", "SELECT id, name, score FROM public.items LIMIT 100", nil, nil, codec)
	if err != nil {
		t.Fatalf("compileQueryPlan() error = %v", err)
	}
	if strings.Contains(plan.SQL, "OR 1=1") || !strings.Contains(plan.SQL, `"name" = $1`) {
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
	nextPlan, err := compileQueryPlan(queryService, request, queryProtocolREST, "postgresql", "SELECT id, name, score FROM public.items LIMIT 100", nil, nil, codec)
	if err != nil {
		t.Fatalf("compileQueryPlan(cursor) error = %v", err)
	}
	if !strings.Contains(nextPlan.SQL, `(addp_source."score" < $2) OR (addp_source."score" = $3 AND addp_source."id" > $4)`) {
		t.Fatalf("unexpected keyset predicate: %s", nextPlan.SQL)
	}
	if !reflect.DeepEqual(nextPlan.Args, []interface{}{"x' OR 1=1 --", 9.5, 9.5, int64(7)}) {
		t.Fatalf("cursor args = %#v", nextPlan.Args)
	}

	request.Select = []string{"id"}
	if _, err := compileQueryPlan(queryService, request, queryProtocolREST, "postgresql", "SELECT id, name, score FROM public.items", nil, nil, codec); !errors.Is(err, ErrInvalidQueryCursor) {
		t.Fatalf("query-shape mismatch error = %v, want ErrInvalidQueryCursor", err)
	}
}

func TestCompileQueryPlanContinuesExistingPostgreSQLArgs(t *testing.T) {
	t.Parallel()

	queryService := testPublishedQueryService()
	plan, err := compileQueryPlan(
		queryService,
		&models.QueryExecutionRequest{
			Select: []string{"id", "name"},
			Filter: &models.QueryFilter{Field: "name", Op: "eq", Value: "active"},
			Page:   models.QueryPageRequest{Limit: 2},
		},
		queryProtocolREST,
		"postgresql",
		"SELECT id, name, score FROM public.items WHERE score > $1",
		[]interface{}{10},
		nil,
		newQueryTokenCodec([]byte("0123456789abcdef0123456789abcdef")),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.SQL, `addp_source."name" = $2`) {
		t.Fatalf("query did not continue existing positional arguments: %s", plan.SQL)
	}
	if !reflect.DeepEqual(plan.Args, []interface{}{10, "active"}) {
		t.Fatalf("args = %#v", plan.Args)
	}
}

func TestCompileQueryPlanUsesOracleSubqueryAndFetchSyntax(t *testing.T) {
	t.Parallel()

	queryService := testPublishedQueryService()
	plan, err := compileQueryPlan(
		queryService,
		&models.QueryExecutionRequest{Select: []string{"id", "name"}, Page: models.QueryPageRequest{Limit: 2}},
		queryProtocolREST,
		"oracle",
		`SELECT "ID" AS "id", "NAME" AS "name", "SCORE" AS "score" FROM "BUSINESS"."ITEMS"`,
		nil,
		nil,
		newQueryTokenCodec([]byte("0123456789abcdef0123456789abcdef")),
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan.SQL, ") AS addp_source") {
		t.Fatalf("Oracle query contains unsupported AS table alias: %s", plan.SQL)
	}
	if !strings.Contains(plan.SQL, `) addp_source ORDER BY addp_source."id" ASC FETCH FIRST 3 ROWS ONLY`) {
		t.Fatalf("unexpected Oracle query plan: %s", plan.SQL)
	}
}

func TestCompileQueryPlanUsesOracleParameterSyntax(t *testing.T) {
	t.Parallel()

	queryService := testPublishedQueryService()
	plan, err := compileQueryPlan(
		queryService,
		&models.QueryExecutionRequest{
			Select: []string{"id", "name"},
			Filter: &models.QueryFilter{Field: "name", Op: "eq", Value: "active"},
			Page:   models.QueryPageRequest{Limit: 2},
		},
		queryProtocolREST,
		"oracle",
		`SELECT "ID" AS "id", "NAME" AS "name", "SCORE" AS "score" FROM "BUSINESS"."ITEMS"`,
		nil,
		nil,
		newQueryTokenCodec([]byte("0123456789abcdef0123456789abcdef")),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.SQL, `addp_source."name" = :1`) {
		t.Fatalf("Oracle query did not use positional parameter syntax: %s", plan.SQL)
	}
}

func TestCompileQueryPlanUsesPostgreSQLPlaceholdersForSpatialFilter(t *testing.T) {
	t.Parallel()

	queryService := testPublishedQueryService()
	srid := 4490
	snapshot := queryService.SourceSnapshot()
	snapshot.Table.Fields = append(snapshot.Table.Fields, datatype.FieldInfo{Name: "shape", Type: datatype.FieldTypeGeometry})
	snapshot.Spatial = &datatype.SpatialInfo{
		GeometryColumns:       []datatype.GeometryColumnInfo{{Name: "shape", SRID: &srid}},
		PrimaryGeometryColumn: "shape",
	}
	queryService.DataConfig[models.QueryServiceSourceSnapshotKey] = commonJSON.MapFromStruct(snapshot)
	plan, err := compileQueryPlan(
		queryService,
		&models.QueryExecutionRequest{
			Select: []string{"id", "shape"},
			Filter: &models.QueryFilter{Field: "shape", Op: "bbox_intersects", Value: []interface{}{112.5, 27.5, 114.5, 29.5}},
			Page:   models.QueryPageRequest{Limit: 2},
		},
		queryProtocolOGC,
		"postgresql",
		"SELECT id, shape FROM public.items",
		nil,
		nil,
		newQueryTokenCodec([]byte("0123456789abcdef0123456789abcdef")),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := `ST_Transform(ST_MakeEnvelope($1, $2, $3, $4, 4326), $5)`
	if !strings.Contains(plan.SQL, want) {
		t.Fatalf("spatial query did not use PostgreSQL placeholders: %s", plan.SQL)
	}
	if !reflect.DeepEqual(plan.Args, []interface{}{112.5, 27.5, 114.5, 29.5, 4490}) {
		t.Fatalf("args = %#v", plan.Args)
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
	tamperedBytes, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	tamperedBytes[len(tamperedBytes)-1] ^= 0x01
	tampered := base64.RawURLEncoding.EncodeToString(tamperedBytes)
	if _, err := codec.decodeCursor(tampered); !errors.Is(err, ErrInvalidQueryCursor) {
		t.Fatalf("tampered cursor error = %v", err)
	}
	decodedToken, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(decodedToken, []byte(`"service_id"`)) {
		t.Fatalf("cursor exposed plaintext payload: %q", decodedToken)
	}

	executor := &QueryExecutorService{tokenCodec: codec}
	featureID, err := executor.encodeFeatureID(queryService, map[string]interface{}{"id": int64(3), "name": "alpha"})
	if err != nil {
		t.Fatalf("encodeFeatureID() error = %v", err)
	}
	decodedFeatureID, err := base64.RawURLEncoding.DecodeString(featureID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(decodedFeatureID, []byte("alpha")) {
		t.Fatalf("feature ID exposed a source value: %q", decodedFeatureID)
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
	result, err := executor.finalizeResult(queryService, plan, &plugin.QueryResult{Columns: []string{"name", "id"}, Rows: []map[string]interface{}{
		{"id": int64(1), "name": "one"},
		{"id": int64(2), "name": "two"},
		{"id": int64(3), "name": "three"},
	}}, nil)
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

func TestFinalizeResultUsesProtectedColumnsAndRows(t *testing.T) {
	t.Parallel()

	queryService := testPublishedQueryService()
	executor := &QueryExecutorService{tokenCodec: newQueryTokenCodec([]byte("0123456789abcdef0123456789abcdef"))}
	plan := &compiledQueryPlan{
		Limit: 1, SelectedFields: []string{"id", "name"},
		OrderBy:   []models.QueryOrder{{Field: "id", Direction: "asc"}},
		QueryHash: "hash", ServiceVersion: "revision-1",
	}
	protect := func(result *plugin.QueryResult) error {
		delete(result.Rows[0], "name")
		result.Columns = []string{"id"}
		return nil
	}
	result, err := executor.finalizeResult(queryService, plan, &plugin.QueryResult{
		Columns: []string{"id", "name"},
		Rows:    []map[string]interface{}{{"id": int64(1), "name": "sensitive"}},
	}, protect)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := result.Data[0]["name"]; exists {
		t.Fatalf("suppressed value remained in response: %#v", result.Data[0])
	}
	if !reflect.DeepEqual(result.Fields, []string{"id"}) {
		t.Fatalf("protected fields = %#v", result.Fields)
	}
}

func TestNormalizePublishedResultRowsCoercesMySQLBooleanScalars(t *testing.T) {
	t.Parallel()

	table := &datatype.TableInfo{Fields: []datatype.FieldInfo{
		{Name: "active", Type: datatype.FieldTypeBool},
		{Name: "name", Type: datatype.FieldTypeString},
	}}
	rows := []map[string]interface{}{
		{"active": int8(1), "name": "one"},
		{"active": []byte("0"), "name": "two"},
		{"active": nil, "name": "three"},
	}
	if err := normalizePublishedResultRows(rows, table); err != nil {
		t.Fatalf("normalizePublishedResultRows() error = %v", err)
	}
	if rows[0]["active"] != true || rows[1]["active"] != false || rows[2]["active"] != nil {
		t.Fatalf("normalized rows = %#v", rows)
	}
	if err := normalizePublishedResultRows(
		[]map[string]interface{}{{"active": int8(2)}}, table,
	); err == nil {
		t.Fatal("normalizePublishedResultRows() accepted non-boolean TINYINT")
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
