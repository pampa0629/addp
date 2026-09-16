package service

import (
	"fmt"
	"testing"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

func TestPostgresOutdoorConceptModelRealizesDirectlyAsDimensionsAndFact(t *testing.T) {
	tx, tenantID := beginModelAggregatePostgresTransaction(t)
	userID := tenantID + 1
	entityRepo := repository.NewEntityRepository(tx)
	relationRepo := repository.NewEntityRelationRepository(tx)
	tableRepo := repository.NewLogicalTableRepository(tx)
	mappingRepo := repository.NewConceptMappingRepository(tx)

	entities := map[string]*models.Entity{
		"person":        {TenantID: tenantID, Name: "户外参与者", Code: fmt.Sprintf("outdoor_person_%d", tenantID), Status: "approved", Version: 1, CreatedBy: userID},
		"activity":      {TenantID: tenantID, Name: "户外活动", Code: fmt.Sprintf("outdoor_activity_%d", tenantID), Status: "approved", Version: 1, CreatedBy: userID},
		"participation": {TenantID: tenantID, Name: "活动参与", Code: fmt.Sprintf("outdoor_participation_%d", tenantID), Status: "approved", Version: 1, CreatedBy: userID},
	}
	attributes := make(map[string]*models.EntityAttribute)
	for code, entity := range entities {
		if err := entityRepo.Create(entity); err != nil {
			t.Fatalf("create Outdoor entity %s: %v", code, err)
		}
		attribute := &models.EntityAttribute{
			EntityID: entity.ID, Name: "业务主键", ColumnName: code + "_id", DataType: "bigint", IsPK: true,
		}
		if err := entityRepo.CreateAttribute(attribute); err != nil {
			t.Fatalf("create Outdoor entity attribute %s: %v", code, err)
		}
		attributes[code] = attribute
	}

	personRelation := &models.EntityRelation{
		TenantID: tenantID, SourceEntity: entities["person"].ID, TargetEntity: entities["participation"].ID,
		RelationType: "one_to_many", Name: "参与人", Version: 1,
	}
	activityRelation := &models.EntityRelation{
		TenantID: tenantID, SourceEntity: entities["activity"].ID, TargetEntity: entities["participation"].ID,
		RelationType: "one_to_many", Name: "参与活动", Version: 1,
	}
	for _, relation := range []*models.EntityRelation{personRelation, activityRelation} {
		if err := relationRepo.Create(relation); err != nil {
			t.Fatalf("create Outdoor entity relation: %v", err)
		}
	}

	layer := &models.DWLayer{TenantID: tenantID, LayerCode: "dwd", LayerName: "DWD", Version: 1}
	if err := repository.NewDWLayerRepository(tx).Create(layer); err != nil {
		t.Fatalf("create Outdoor layer: %v", err)
	}
	tables := map[string]*models.LogicalTable{
		"person": {
			TenantID: tenantID, Name: "参与者维度", Code: fmt.Sprintf("dim_outdoor_person_%d", tenantID),
			TableType: "dimension", Layer: "dwd", Status: "draft", SCDType: 1, Materialization: models.JSONB{}, Version: 1, CreatedBy: userID,
		},
		"activity": {
			TenantID: tenantID, Name: "活动维度", Code: fmt.Sprintf("dim_outdoor_activity_%d", tenantID),
			TableType: "dimension", Layer: "dwd", Status: "draft", SCDType: 1, Materialization: models.JSONB{}, Version: 1, CreatedBy: userID,
		},
		"participation": {
			TenantID: tenantID, Name: "活动参与事实", Code: fmt.Sprintf("dwd_outdoor_participation_%d", tenantID),
			TableType: "fact", Layer: "dwd", Status: "draft", GrainDescription: "每行代表一位参与者参加一次户外活动",
			Materialization: models.JSONB{}, Version: 1, CreatedBy: userID,
		},
	}
	fields := make(map[string]*models.LogicalField)
	for code, table := range tables {
		if err := tableRepo.Create(table); err != nil {
			t.Fatalf("create Outdoor logical table %s: %v", code, err)
		}
		field := &models.LogicalField{
			TableID: table.ID, Name: "业务主键", ColumnName: code + "_id", DataType: "bigint", IsPK: true, FieldRole: "regular",
		}
		if err := tableRepo.CreateField(field); err != nil {
			t.Fatalf("create Outdoor logical field %s: %v", code, err)
		}
		fields[code] = field
	}
	personFK := &models.LogicalField{TableID: tables["participation"].ID, Name: "参与者", ColumnName: "person_id", DataType: "bigint", FieldRole: "dimension_fk"}
	activityFK := &models.LogicalField{TableID: tables["participation"].ID, Name: "活动", ColumnName: "activity_id", DataType: "bigint", FieldRole: "dimension_fk"}
	for _, field := range []*models.LogicalField{personFK, activityFK} {
		if err := tableRepo.CreateField(field); err != nil {
			t.Fatalf("create Outdoor fact foreign key: %v", err)
		}
	}
	personTableRelation := &models.TableRelation{
		TenantID: tenantID, SourceTable: tables["participation"].ID, SourceField: personFK.ID,
		TargetTable: tables["person"].ID, TargetField: fields["person"].ID, RelationType: "fk",
	}
	activityTableRelation := &models.TableRelation{
		TenantID: tenantID, SourceTable: tables["participation"].ID, SourceField: activityFK.ID,
		TargetTable: tables["activity"].ID, TargetField: fields["activity"].ID, RelationType: "fk",
	}
	for _, relation := range []*models.TableRelation{personTableRelation, activityTableRelation} {
		if err := repository.NewTableRelationRepository(tx).Create(relation); err != nil {
			t.Fatalf("create Outdoor table relation: %v", err)
		}
	}

	mappingSvc := NewConceptMappingService(mappingRepo, tableRepo)
	for _, code := range []string{"person", "activity"} {
		response, err := mappingSvc.Replace(tables[code].ID, tenantID, &models.ReplaceConceptMappingsRequest{
			Version:       1,
			TableMappings: []models.TableEntityMappingInput{{EntityID: entities[code].ID, MappingRole: "represents"}},
			FieldMappings: []models.FieldAttributeMappingInput{{FieldID: fields[code].ID, EntityAttributeID: attributes[code].ID, MappingRole: "direct"}},
		})
		if err != nil {
			t.Fatalf("map Outdoor %s dimension: %v", code, err)
		}
		if response.Version != 2 || len(response.TableMappings) != 1 || !response.TableMappings[0].InSync {
			t.Fatalf("Outdoor %s dimension mappings = %#v", code, response)
		}
		tables[code].Version = response.Version
	}

	factResponse, err := mappingSvc.Replace(tables["participation"].ID, tenantID, &models.ReplaceConceptMappingsRequest{
		Version:       1,
		TableMappings: []models.TableEntityMappingInput{{EntityID: entities["participation"].ID, MappingRole: "represents"}},
		FieldMappings: []models.FieldAttributeMappingInput{{FieldID: fields["participation"].ID, EntityAttributeID: attributes["participation"].ID, MappingRole: "direct"}},
		RelationMappings: []models.RelationConceptMappingInput{
			{TableRelationID: personTableRelation.ID, EntityRelationID: personRelation.ID, Orientation: "inverse"},
			{TableRelationID: activityTableRelation.ID, EntityRelationID: activityRelation.ID, Orientation: "inverse"},
		},
	})
	if err != nil {
		t.Fatalf("map Outdoor participation fact: %v", err)
	}
	if factResponse.Version != 2 || len(factResponse.RelationMappings) != 2 {
		t.Fatalf("Outdoor participation mappings = %#v", factResponse)
	}

	logicalTableSvc := NewLogicalTableService(tableRepo, repository.NewDWLayerRepository(tx))
	logicalTableSvc.SetConceptMappingRepository(mappingRepo)
	for _, code := range []string{"person", "activity"} {
		if _, err := logicalTableSvc.ApproveLogicalTable(tables[code].ID, tenantID, userID, tables[code].Version); err != nil {
			t.Fatalf("approve Outdoor %s dimension: %v", code, err)
		}
	}
	if err := tx.Model(&models.Entity{}).Where("id = ?", entities["participation"].ID).
		Update("version", entities["participation"].Version+1).Error; err != nil {
		t.Fatalf("simulate conceptual-model revision: %v", err)
	}
	entities["participation"].Version++
	_, err = logicalTableSvc.ApproveLogicalTable(tables["participation"].ID, tenantID, userID, factResponse.Version)
	requireDomainErrorCode(t, err, "concept_mapping_drift")

	factResponse, err = mappingSvc.Replace(tables["participation"].ID, tenantID, &models.ReplaceConceptMappingsRequest{
		Version:       factResponse.Version,
		TableMappings: []models.TableEntityMappingInput{{EntityID: entities["participation"].ID, MappingRole: "represents"}},
		FieldMappings: []models.FieldAttributeMappingInput{{FieldID: fields["participation"].ID, EntityAttributeID: attributes["participation"].ID, MappingRole: "direct"}},
		RelationMappings: []models.RelationConceptMappingInput{
			{TableRelationID: personTableRelation.ID, EntityRelationID: personRelation.ID, Orientation: "inverse"},
			{TableRelationID: activityTableRelation.ID, EntityRelationID: activityRelation.ID, Orientation: "inverse"},
		},
	})
	if err != nil {
		t.Fatalf("refresh Outdoor participation mappings after drift: %v", err)
	}
	approvedFact, err := logicalTableSvc.ApproveLogicalTable(tables["participation"].ID, tenantID, userID, factResponse.Version)
	if err != nil {
		t.Fatalf("approve Outdoor participation fact: %v", err)
	}
	if approvedFact.Status != "approved" {
		t.Fatalf("Outdoor participation fact status = %q", approvedFact.Status)
	}

	entitySvc := NewEntityService(entityRepo, relationRepo)
	entitySvc.SetConceptMappingRepository(mappingRepo)
	_, err = entitySvc.ReopenEntity(entities["participation"].ID, tenantID, userID, entities["participation"].Version)
	requireDomainErrorCode(t, err, "concept_mapping_in_use")
}
