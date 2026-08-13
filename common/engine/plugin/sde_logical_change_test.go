package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/common/datatype"
)

type sdeLogicalChangeProviderFixture struct{}

func (sdeLogicalChangeProviderFixture) OpenSDELogicalChangeSource(context.Context, ConnectionInfo, CatalogPath, SDELogicalChangeOpenOptions) (SDELogicalChangeSource, error) {
	return nil, ErrSDELogicalPositionExpired
}

func TestSDELogicalChangeProviderContractIsWorkspaceScoped(t *testing.T) {
	var provider SDELogicalChangeSourceProvider = sdeLogicalChangeProviderFixture{}
	_, err := provider.OpenSDELogicalChangeSource(context.Background(), nil, CatalogPath{}, SDELogicalChangeOpenOptions{
		BootstrapMode: SDELogicalBootstrapInitial,
	})
	if !errors.Is(err, ErrSDELogicalPositionExpired) {
		t.Fatalf("OpenSDELogicalChangeSource() error = %v", err)
	}
	if _, isEnginePlugin := provider.(EnginePlugin); isEnginePlugin {
		t.Fatal("SDE logical change provider must remain workspace-scoped")
	}
}

func TestSDELogicalChangeContractConstants(t *testing.T) {
	if SDEVersioningModelTraditional != "traditional" || SDEVersioningModelBranch != "branch" {
		t.Fatal("unexpected SDE versioning model constants")
	}
	if SDELogicalPositionType != "arcgis_sde_logical_position" || SDELogicalPositionVersionV1 != "v1" {
		t.Fatal("unexpected SDE logical position identity")
	}
}

func TestValidateSDELogicalSourceDescriptor(t *testing.T) {
	descriptor := SDELogicalSourceDescriptor{
		RepositoryOwner: "SDE", RegistrationID: "42",
		VersioningModel: SDEVersioningModelTraditional, VersionName: "SDE.DEFAULT",
		Fields: []datatype.FieldInfo{{Name: "FEATURE_ID", Type: datatype.FieldTypeBigInt}},
		Keys:   []string{"FEATURE_ID"},
	}
	if err := ValidateSDELogicalSourceDescriptor(descriptor); err != nil {
		t.Fatalf("ValidateSDELogicalSourceDescriptor() error = %v", err)
	}
	descriptor.VersioningModel = SDEVersioningModelBranch
	if err := ValidateSDELogicalSourceDescriptor(descriptor); err == nil {
		t.Fatal("branch versioning must remain unsupported")
	}
}

func TestValidateSDELogicalChange(t *testing.T) {
	position := ChangeStreamPosition{
		Type: SDELogicalPositionType, Version: SDELogicalPositionVersionV1,
		Partition: "SDE.DEFAULT/42", Values: map[string]string{"checkpoint_token": "opaque-7"},
	}
	upsert := SDELogicalChange{
		Operation: TableChangeOperationUpsert, Position: position,
		Key: map[string]interface{}{"FEATURE_ID": int64(1)}, Row: map[string]interface{}{"FEATURE_ID": int64(1)},
	}
	if err := ValidateSDELogicalChange(upsert, []string{"FEATURE_ID"}); err != nil {
		t.Fatalf("ValidateSDELogicalChange(upsert) error = %v", err)
	}
	deleted := upsert
	deleted.Operation = TableChangeOperationDelete
	if err := ValidateSDELogicalChange(deleted, []string{"FEATURE_ID"}); err == nil {
		t.Fatal("delete carrying a row must be rejected")
	}
	deleted.Row = nil
	if err := ValidateSDELogicalChange(deleted, []string{"FEATURE_ID"}); err != nil {
		t.Fatalf("ValidateSDELogicalChange(delete) error = %v", err)
	}
	batch := &SDELogicalChangeBatch{Changes: []SDELogicalChange{deleted}, EndPosition: position}
	if err := ValidateSDELogicalChangeBatch(batch, []string{"FEATURE_ID"}); err != nil {
		t.Fatalf("ValidateSDELogicalChangeBatch() error = %v", err)
	}
	batch.Changes[0].Position.Partition = "SDE.OTHER/42"
	if err := ValidateSDELogicalChangeBatch(batch, []string{"FEATURE_ID"}); err == nil {
		t.Fatal("cross-partition SDE batch must be rejected")
	}
}
