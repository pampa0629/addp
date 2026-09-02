package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/security/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestReconcileStructuredOwnerProjectionsUpgradesLegacyEnrollmentOnce(t *testing.T) {
	db := openSecurityTestDB(t)
	definitions := newTestDefinitionService(db)
	classification, err := definitions.CreateClassification(models.DefinitionRequest{Code: "personal", Name: "个人信息"}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	grade, err := definitions.CreateGrade(models.DefinitionRequest{Code: "l3", Name: "三级", RiskOrder: 3}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	dataType, err := definitions.CreateType(models.SensitiveDataTypeRequest{Code: "phone", Name: "手机号", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID, ProtectionThreshold: 0.9}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := definitions.CreateBaseline(models.ProtectionBaselineRequest{SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID, Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1, KeepPrefix: 3, KeepSuffix: 4, InvalidValueEffect: dataprotection.EffectSuppress}, 7, 11); err != nil {
		t.Fatal(err)
	}

	created, err := NewEnrollmentService(db).Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(11, resourcetree.TypeCollection, "Outdoor.Persons"))
	if err != nil {
		t.Fatal(err)
	}
	fields := []datatype.FieldInfo{
		{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "userInfo.phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	snapshot, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	component := dataprotection.Component{Key: "userInfo.phone", Path: []dataprotection.PathSegment{{Name: "userInfo", Container: "object"}, {Name: "phone", Container: "scalar"}}, ValueType: string(datatype.FieldTypeString)}
	component.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(fields, component)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	finding := models.SensitiveFinding{
		ID: uuid.NewString(), TenantID: 7, EnrollmentID: created.ID, ComponentKey: component.Key,
		SensitiveDataTypeID: dataType.ID, DetectorCode: "phone", DetectorVersion: models.FindingDetectorPhoneMetadataV1,
		Confidence: 1, Evidence: commonmodels.JSONMap{"matched_field_name": "phone"}, Component: component,
		SourceSnapshotHash: snapshot, ObservedAt: now, CreatedAt: now,
	}
	if err := db.Create(&finding).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ProtectionEnrollment{}).Where("tenant_id = ? AND id = ?", 7, created.ID).Updates(map[string]interface{}{
		"state": models.EnrollmentStateEnrolling, "latest_source_snapshot_hash": snapshot, "last_discovered_at": now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	var enrollment models.ProtectionEnrollment
	if err := db.Where("tenant_id = ? AND id = ?", 7, created.ID).First(&enrollment).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return compileProtectionProjections(tx, enrollment, snapshot, now, []string{"manager"})
	}); err != nil {
		t.Fatal(err)
	}

	reconciled, err := ReconcileStructuredOwnerProjections(context.Background(), db, now.Add(time.Second))
	if err != nil || reconciled != 3 {
		t.Fatalf("reconciled = %d, err=%v", reconciled, err)
	}
	changes, err := NewEnrollmentService(db).ListChanges(context.Background(), 7, "develop", "", 20)
	if err != nil || len(changes.Changes) != 2 {
		t.Fatalf("develop changes = %#v, err=%v", changes, err)
	}
	requireDevelopQueryProjection(t, changes.Changes[1].Projection, dataprotection.EffectMask)
	serviceChanges, err := NewEnrollmentService(db).ListChanges(context.Background(), 7, "service", "", 20)
	if err != nil || len(serviceChanges.Changes) != 2 {
		t.Fatalf("service changes = %#v, err=%v", serviceChanges, err)
	}
	requireServiceExecuteProjection(t, serviceChanges.Changes[1].Projection, dataprotection.EffectMask)
	transferChanges, err := NewEnrollmentService(db).ListChanges(context.Background(), 7, "transfer", "", 20)
	if err != nil || len(transferChanges.Changes) != 2 {
		t.Fatalf("transfer changes = %#v, err=%v", transferChanges, err)
	}
	requireTransferExportProjection(t, transferChanges.Changes[1].Projection, dataprotection.EffectMask)
	second, err := ReconcileStructuredOwnerProjections(context.Background(), db, now.Add(2*time.Second))
	if err != nil || second != 0 {
		t.Fatalf("second reconciliation = %d, err=%v", second, err)
	}
}
