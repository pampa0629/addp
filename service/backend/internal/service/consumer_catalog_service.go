package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/repository"
)

var ErrInvalidConsumerContract = errors.New("invalid service consumer contract")

const ConsumerQueryIntentHeader = "X-ADDP-Query-Intent"

type consumerQueryServiceRepository interface {
	ListConsumerServices(filter models.ConsumerServiceListFilter) ([]models.QueryService, int64, error)
	GetConsumerServiceByID(tenantID, serviceID uint) (*models.QueryService, error)
	MigrateInvalidQueryServiceConsumerContracts(validate func(*models.QueryService) error) (int64, error)
}

type ConsumerCatalogService struct {
	repo consumerQueryServiceRepository
}

func NewConsumerCatalogService(repo *repository.QueryServiceRepository) *ConsumerCatalogService {
	return &ConsumerCatalogService{repo: repo}
}

func (s *ConsumerCatalogService) MigrateInvalidQueryServiceContracts() (int64, error) {
	return s.repo.MigrateInvalidQueryServiceConsumerContracts(ValidateQueryConsumerContract)
}

func (s *ConsumerCatalogService) ListQueryServices(filter models.ConsumerServiceListFilter) ([]models.ConsumerServiceSummary, int64, error) {
	services, total, err := s.repo.ListConsumerServices(filter)
	if err != nil {
		return nil, 0, fmt.Errorf("list consumer services: %w", err)
	}
	result := make([]models.ConsumerServiceSummary, len(services))
	for index := range services {
		descriptor, err := BuildQueryConsumerDescriptor(&services[index])
		if err != nil {
			return nil, 0, err
		}
		result[index] = models.ConsumerServiceSummary{
			Ref: descriptor.Ref, Title: descriptor.Title, Description: descriptor.Description,
			AccessMode: descriptor.AccessMode, OutputKind: descriptor.OutputContract.Kind,
			ContractFingerprint: descriptor.ContractFingerprint,
		}
	}
	return result, total, nil
}

func (s *ConsumerCatalogService) GetQueryService(tenantID, serviceID uint) (*models.ConsumerDescriptor, error) {
	service, err := s.repo.GetConsumerServiceByID(tenantID, serviceID)
	if err != nil {
		return nil, err
	}
	return BuildQueryConsumerDescriptor(service)
}

func BuildQueryConsumerDescriptor(service *models.QueryService) (*models.ConsumerDescriptor, error) {
	if service == nil || service.ID == 0 {
		return nil, fmt.Errorf("%w: query service is not consumable", ErrInvalidConsumerContract)
	}
	if err := ValidateQueryConsumerContract(service); err != nil {
		return nil, err
	}
	table := service.GetTableInfo()
	stableKey := service.GetStableKey()

	outputKind := models.ConsumerOutputKindTabular
	spatialContract := consumerSpatialContract(service)
	if spatialContract != nil {
		outputKind = models.ConsumerOutputKindSpatialTabular
	}
	formats := consumerRESTFormats(service, spatialContract != nil)

	filterable := stringSet(service.GetFilterableFields())
	inputFields := make([]models.ConsumerQueryField, len(table.Fields))
	outputFields := make([]models.ConsumerOutputField, len(table.Fields))
	for index, field := range table.Fields {
		_, canFilter := filterable[field.Name]
		inputFields[index] = models.ConsumerQueryField{
			Name: field.Name, Type: field.Type, ElementType: field.ElementType, Nullable: field.Nullable,
			Selectable: true, Filterable: canFilter,
			Operators: consumerFieldOperators(field, canFilter, service.GetGeometryColumn()),
			Sortable:  !field.Nullable && isStableOrderFieldType(field.Type),
		}
		outputFields[index] = models.ConsumerOutputField{
			Name: field.Name, Type: field.Type, ElementType: field.ElementType,
			Nullable: field.Nullable, Comment: field.Comment,
		}
	}

	maxLimit := service.MaxFeatures
	if maxLimit <= 0 {
		maxLimit = 1000
	}
	if maxLimit > 10000 {
		maxLimit = 10000
	}
	defaultLimit := 50
	if maxLimit < defaultLimit {
		defaultLimit = maxLimit
	}
	defaultSelection := service.GetDefaultFields()
	if len(defaultSelection) == 0 {
		defaultSelection = table.FieldNames()
	}
	operation := models.ConsumerOperation{
		Key: "query", Method: "POST", Path: "/api/query/" + service.ServiceName + "/query",
		InputKind: "structured_query", OutputKind: outputKind,
	}
	input := models.StructuredQueryInputContract{
		Kind: "structured_query", NamedParameters: consumerNamedParameters(service.NamedParameters), Fields: inputFields, DefaultSelection: append([]string(nil), defaultSelection...),
		Filter:  models.ConsumerFilterContract{Combinators: []string{"and", "or", "not"}, MaxDepth: 16, MaxNodes: 256, MaxInValues: 1000},
		Order:   models.ConsumerOrderContract{Directions: []string{"asc", "desc"}, StableKey: append([]string(nil), stableKey...)},
		Page:    models.ConsumerPageContract{Kind: "cursor", DefaultLimit: defaultLimit, MaxLimit: maxLimit},
		Formats: formats,
		Intent: models.ConsumerQueryIntent{
			Header: ConsumerQueryIntentHeader, AllowedValues: []string{"query", "export"}, DefaultValue: "query",
		},
	}
	output := models.TabularOutputContract{Kind: outputKind, Fields: outputFields, Spatial: spatialContract}
	fingerprint, err := consumerContractFingerprint([]models.ConsumerOperation{operation}, input, output)
	if err != nil {
		return nil, err
	}
	accessMode := "private"
	if service.PublicAccess {
		accessMode = "public"
	}
	return &models.ConsumerDescriptor{
		SchemaVersion: models.ConsumerDescriptorSchemaVersion,
		Ref:           models.ConsumerServiceReference{ServiceType: models.ConsumerServiceTypeQuery, ServiceID: service.ID},
		Title:         service.Title, Description: service.Description, Status: "active", AccessMode: accessMode,
		ContractFingerprint: fingerprint, Operations: []models.ConsumerOperation{operation},
		InputContract: input, OutputContract: output,
	}, nil
}

// ValidateQueryConsumerContract is the single publication invariant shared by
// creation, updates, status restoration, data migration, and descriptor reads.
func ValidateQueryConsumerContract(service *models.QueryService) error {
	if service == nil || service.Status != "active" || !service.IsRESTAPIEnabled() {
		return fmt.Errorf("%w: query service is not consumable", ErrInvalidConsumerContract)
	}
	if _, err := validateQueryServiceNamedParameters(service.ConfigType, service.SqlQuery, service.NamedParameters); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidConsumerContract, err)
	}
	table := service.GetTableInfo()
	if table == nil || len(table.Fields) == 0 {
		return fmt.Errorf("%w: output fields are missing", ErrInvalidConsumerContract)
	}
	stableKey := service.GetStableKey()
	if _, err := validateStableKey(stableKey, table); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidConsumerContract, err)
	}

	spatialContract := consumerSpatialContract(service)
	formats := consumerRESTFormats(service, spatialContract != nil)
	if len(formats) == 0 {
		return fmt.Errorf("%w: REST response formats are missing", ErrInvalidConsumerContract)
	}
	for _, field := range table.Fields {
		if strings.TrimSpace(field.Name) == "" || !datatype.IsKnownFieldType(field.Type) {
			return fmt.Errorf("%w: output field is invalid", ErrInvalidConsumerContract)
		}
	}
	config := service.DataConfig
	if config == nil {
		config = models.JSONB{}
	}
	configCopy := make(models.JSONB, len(config))
	for key, value := range config {
		configCopy[key] = value
	}
	if err := validateQueryFieldPolicy(configCopy, table); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidConsumerContract, err)
	}
	return nil
}

func consumerNamedParameters(parameters []models.QueryServiceNamedParameter) []models.ConsumerNamedParameter {
	result := make([]models.ConsumerNamedParameter, 0, len(parameters))
	for _, parameter := range parameters {
		result = append(result, models.ConsumerNamedParameter{
			Name: parameter.Name, Type: parameter.Type, Required: parameter.Required,
			Description: parameter.Description, Default: parameter.Default,
		})
	}
	return result
}

func consumerContractFingerprint(
	operations []models.ConsumerOperation,
	input models.StructuredQueryInputContract,
	output models.TabularOutputContract,
) (string, error) {
	payload := struct {
		Operations []models.ConsumerOperation          `json:"operations"`
		Input      models.StructuredQueryInputContract `json:"input_contract"`
		Output     models.TabularOutputContract        `json:"output_contract"`
	}{Operations: operations, Input: input, Output: output}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("%w: encode fingerprint payload: %v", ErrInvalidConsumerContract, err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func consumerFieldOperators(field datatype.FieldInfo, filterable bool, geometryField string) []string {
	if !filterable {
		return []string{}
	}
	if datatype.IsSpatialFieldType(field.Type) {
		if field.Name == geometryField {
			return []string{"bbox_intersects", "is_null", "is_not_null"}
		}
		return []string{"is_null", "is_not_null"}
	}
	if !isStableOrderFieldType(field.Type) {
		return []string{"is_null", "is_not_null"}
	}
	operators := []string{"eq", "ne", "in", "is_null", "is_not_null"}
	if field.Type != datatype.FieldTypeBool {
		operators = append(operators, "lt", "lte", "gt", "gte")
	}
	return operators
}

func consumerRESTFormats(service *models.QueryService, spatial bool) []string {
	config := service.GetProtocolConfig("rest_api")
	var raw []string
	switch values := config["formats"].(type) {
	case []string:
		raw = values
	case []interface{}:
		for _, value := range values {
			if format, ok := value.(string); ok {
				raw = append(raw, format)
			}
		}
	}
	if _, declared := config["formats"]; !declared {
		raw = []string{"json", "csv"}
		if spatial {
			raw = append(raw, "geojson")
		}
	}
	allowed := map[string]bool{"json": true, "csv": true, "geojson": spatial}
	seen := map[string]struct{}{}
	for _, value := range raw {
		format := strings.ToLower(strings.TrimSpace(value))
		if !allowed[format] {
			continue
		}
		if _, exists := seen[format]; exists {
			continue
		}
		seen[format] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for _, format := range []string{"json", "csv", "geojson"} {
		if _, exists := seen[format]; exists {
			result = append(result, format)
		}
	}
	return result
}

func QueryServiceSupportsRESTFormat(service *models.QueryService, format string) bool {
	if service == nil {
		return false
	}
	for _, supported := range consumerRESTFormats(service, service.HasGeometry()) {
		if supported == format {
			return true
		}
	}
	return false
}

func QueryServiceVersion(service *models.QueryService) string {
	return serviceDependencyVersion(service)
}

func consumerSpatialContract(service *models.QueryService) *models.ConsumerSpatialContract {
	spatial := service.GetSpatialInfo()
	if spatial == nil || spatial.PrimaryGeometryName() == "" {
		return nil
	}
	fields := make([]models.ConsumerGeometryField, 0, len(spatial.GeometryColumns))
	for _, field := range spatial.GeometryColumns {
		fields = append(fields, models.ConsumerGeometryField{
			Name: field.Name, GeometryType: field.GeometryType, SRID: cloneInt(field.SRID),
			CRSRef: field.CRSRef, Dimension: cloneInt(field.Dimension),
		})
	}
	return &models.ConsumerSpatialContract{
		PrimaryGeometryField: spatial.PrimaryGeometryName(), SRID: cloneInt(spatial.SRID),
		CRSRef: spatial.CRSRef, GeometryFields: fields,
	}
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
