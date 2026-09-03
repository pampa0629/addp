package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/service/internal/models"
)

func TestBuildQueryConsumerDescriptorProjectsOnlyPublicContract(t *testing.T) {
	service := consumerDescriptorTestService()
	service.NamedParameters = []models.QueryServiceNamedParameter{{
		Name: "threshold", Type: datatype.FieldTypeDouble, Required: false, Default: 0.5, Description: "Minimum score",
	}}
	service.SqlQuery = "SELECT * FROM secret_source WHERE value >= :threshold"
	descriptor, err := BuildQueryConsumerDescriptor(service)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.SchemaVersion != models.ConsumerDescriptorSchemaVersion ||
		descriptor.Ref.ServiceType != models.ConsumerServiceTypeQuery || descriptor.Ref.ServiceID != service.ID ||
		descriptor.OutputContract.Kind != models.ConsumerOutputKindSpatialTabular {
		t.Fatalf("unexpected descriptor identity: %#v", descriptor)
	}
	if descriptor.InputContract.Page.MaxLimit != 500 || descriptor.InputContract.Page.DefaultLimit != 50 {
		t.Fatalf("unexpected page contract: %#v", descriptor.InputContract.Page)
	}
	if len(descriptor.InputContract.DefaultSelection) != 3 || descriptor.InputContract.Filter.MaxInValues != 1000 {
		t.Fatalf("unexpected structured query defaults: %#v", descriptor.InputContract)
	}
	if len(descriptor.InputContract.NamedParameters) != 1 || descriptor.InputContract.NamedParameters[0].Name != "threshold" || descriptor.InputContract.NamedParameters[0].Default != 0.5 {
		t.Fatalf("unexpected named parameters: %#v", descriptor.InputContract.NamedParameters)
	}
	if got := descriptor.InputContract.Fields[1].Operators; !containsConsumerValue(got, "gte") {
		t.Fatalf("numeric filter operators = %#v", got)
	}
	if got := descriptor.InputContract.Fields[2].Operators; !containsConsumerValue(got, "bbox_intersects") {
		t.Fatalf("spatial filter operators = %#v", got)
	}
	encoded, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	payload := string(encoded)
	for _, forbidden := range []string{
		"sql_query", "engine_id", "runtime_engine_id", "schema_name", "table_name",
		"secret-source-table", "SELECT * FROM secret_source", "dependency-secret",
	} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("consumer descriptor leaked %q: %s", forbidden, payload)
		}
	}
}

func TestValidateQueryConsumerContractRejectsIncompleteActiveService(t *testing.T) {
	service := consumerDescriptorTestService()
	delete(service.DataConfig, models.QueryServiceSourceSnapshotKey)

	err := ValidateQueryConsumerContract(service)
	if !errors.Is(err, ErrInvalidConsumerContract) || !strings.Contains(err.Error(), "output fields are missing") {
		t.Fatalf("ValidateQueryConsumerContract() error = %v", err)
	}
}

func TestConsumerContractFingerprintChangesOnlyWithPublicContract(t *testing.T) {
	service := consumerDescriptorTestService()
	initial, err := BuildQueryConsumerDescriptor(service)
	if err != nil {
		t.Fatal(err)
	}

	service.Title = "renamed"
	service.Description = "new description"
	service.PublicAccess = !service.PublicAccess
	snapshot := service.SourceSnapshot()
	snapshot.DependencyHash = "content-refresh-only"
	snapshot.CapturedAt = snapshot.CapturedAt.Add(time.Hour)
	service.DataConfig[models.QueryServiceSourceSnapshotKey] = queryServiceSnapshotPayload(snapshot)
	refreshed, err := BuildQueryConsumerDescriptor(service)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.ContractFingerprint != initial.ContractFingerprint {
		t.Fatalf("metadata/content refresh changed contract fingerprint: %s != %s", refreshed.ContractFingerprint, initial.ContractFingerprint)
	}

	snapshot.Table.Fields[1].Type = datatype.FieldTypeDouble
	service.DataConfig[models.QueryServiceSourceSnapshotKey] = queryServiceSnapshotPayload(snapshot)
	changed, err := BuildQueryConsumerDescriptor(service)
	if err != nil {
		t.Fatal(err)
	}
	if changed.ContractFingerprint == initial.ContractFingerprint {
		t.Fatal("public field type change did not change contract fingerprint")
	}
}

func TestQueryShapeFingerprintExcludesFilterLiterals(t *testing.T) {
	request := &models.QueryExecutionRequest{
		Parameters: map[string]interface{}{"threshold": 10},
		Select:     []string{"category", "value"},
		Filter:     &models.QueryFilter{Field: "category", Op: "eq", Value: "secret-a"},
		OrderBy:    []models.QueryOrder{{Field: "id", Direction: "asc"}},
		Page:       models.QueryPageRequest{Limit: 100, Cursor: "secret-cursor-a"},
		Format:     "json",
	}
	first, err := QueryShapeFingerprint(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Filter.Value = "secret-b"
	request.Parameters["threshold"] = 20
	request.Page.Cursor = "secret-cursor-b"
	second, err := QueryShapeFingerprint(request)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("literal-only change altered query shape: %s != %s", first, second)
	}
	request.Parameters["other"] = true
	parameterShape, err := QueryShapeFingerprint(request)
	if err != nil {
		t.Fatal(err)
	}
	if parameterShape == first {
		t.Fatal("named parameter key change did not alter query shape")
	}
	delete(request.Parameters, "other")
	request.Filter.Op = "ne"
	third, err := QueryShapeFingerprint(request)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("operator change did not alter query shape")
	}
}

func consumerDescriptorTestService() *models.QueryService {
	srid := 4326
	dimension := 2
	snapshot := &models.QueryServiceDependencySnapshot{
		CapturedAt:     time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC),
		DependencyHash: "dependency-secret",
		Table: &datatype.TableInfo{Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt, Nullable: false},
			{Name: "value", Type: datatype.FieldTypeInt, Nullable: true},
			{Name: "location", Type: datatype.FieldTypeGeometry, Nullable: true},
		}},
		Spatial: &datatype.SpatialInfo{
			SRID: &srid, CRSRef: "EPSG:4326", PrimaryGeometryColumn: "location",
			GeometryColumns: []datatype.GeometryColumnInfo{{Name: "location", GeometryType: "Point", SRID: &srid, Dimension: &dimension}},
		},
	}
	return &models.QueryService{
		ID: 42, TenantID: 7, ServiceName: "consumer-test", Title: "Consumer test",
		Description: "descriptor", ConfigType: "sql", SqlQuery: "SELECT * FROM secret_source",
		SchemaName: "private_schema", TargetTable: "secret-source-table",
		DataConfig: models.JSONB{
			models.QueryServiceSourceSnapshotKey: queryServiceSnapshotPayload(snapshot),
			"stable_key":                         []interface{}{"id"},
			"filterable_fields":                  []interface{}{"value", "location"},
		},
		Protocols: models.JSONB{"rest_api": map[string]interface{}{
			"enabled": true, "formats": []interface{}{"json", "csv", "geojson"},
		}},
		PublicAccess: false, MaxFeatures: 500, Status: "active",
	}
}

func containsConsumerValue(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
