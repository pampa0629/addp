package repository

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestPrivateFilePublicationOriginalOperation(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		confirmed bool
	}{{"confirmed", true}, {"unknown", false}} {
		t.Run(scenario.name, func(t *testing.T) {
			db, repo, _, _ := filePendingFixture(t)
			runPrivatePublicationCase(t, db, repo, 102, scenario.confirmed)
		})
	}
}

func runPrivatePublicationCase(t *testing.T, db *gorm.DB, repo *FileExecutionRepository, authorizationID int64, confirmed bool) {
	t.Helper()
	ctx := t.Context()
	item, sources := newFilePendingValues()
	target := &execution.DevelopFileTargetIntent{NodeID: "save", Target: plugin.FileItemPath(11, "outputs/result.gpkg"), Mode: "fail"}
	facts, err := repo.CreatePending(ctx, item, sources, target)
	if err != nil {
		t.Fatal(err)
	}
	issued := fileIssuedFixture(t, facts)
	issued.Authorization.ID = strconv.FormatInt(authorizationID, 10)
	if err := repo.BindAuthorization(ctx, 7, item.ExecutionID, issued); err != nil {
		t.Fatal(err)
	}
	facts, err = repo.Read(ctx, 7, item.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	approval := fileAdmissionFixture(facts)
	approval.AuthorizationID = issued.Authorization.ID
	approval.ApprovalID = uuid.NewString()
	if err := repo.ApplyAdmission(ctx, 7, item.ExecutionID, approval); err != nil {
		t.Fatal(err)
	}
	running, lease, err := NewExecutionRepository(db).ClaimNext("workflow", ctx, "publication-fixture", time.Now(), time.Minute, nil)
	if err != nil || lease == nil || running.ExecutionID != item.ExecutionID {
		t.Fatal(running, lease, err)
	}
	id, op, directory := uuid.NewString(), uuid.NewString(), t.TempDir()
	output := plugin.WorkflowPrivateFile{NodeID: target.NodeID, Size: 8192, SHA256: strings.Repeat("a", 64)}
	if err := repo.RegisterPrivatePublication(ctx, *lease, id, op, output); err == nil {
		t.Fatal("operation registered before Runtime")
	}
	if err := repo.RecordPrivateInputDirectory(ctx, *lease, "publication-fixture", directory); err != nil {
		t.Fatal(err)
	}
	manifest := plugin.WorkflowPrivateFileManifest{SchemaVersion: plugin.WorkflowPrivateFilesAPIV1,
		TenantID: 7, ExecutionID: item.ExecutionID, Attempt: lease.Attempt, AuthorizationID: authorizationID, ScopeDigest: issued.ScopeDigest,
		Deadline: running.StartedAt.Add(time.Minute), WorkflowJSON: `{"tasks":[]}`, Outputs: []string{target.NodeID}}
	var reads []PrivateFileSourceRead
	for _, source := range facts.Intent.Sources {
		set, _ := plugin.NewQueryReadSet(source.Target)
		reads = append(reads, PrivateFileSourceRead{NodeID: source.NodeID, ReadSet: set})
		manifest.Inputs = append(manifest.Inputs, plugin.WorkflowPrivateFile{NodeID: source.NodeID, Size: 123, SHA256: strings.Repeat("a", 64)})
	}
	if err := repo.RecordPrivateRuntime(ctx, *lease, PrivateFileRuntimeRecord{RuntimeExecutionID: id,
		Manifest: manifest, OwnerInstanceID: "publication-fixture", InputDirectory: directory, SourceReads: reads}); err != nil {
		t.Fatal(err)
	}
	otherLease := *lease
	otherLease.Token = uuid.NewString()
	if err := repo.RegisterPrivatePublication(ctx, otherLease, id, op, output); err == nil {
		t.Fatal("wrong lease registered operation")
	}
	if err := repo.RegisterPrivatePublication(ctx, *lease, uuid.NewString(), op, output); err == nil {
		t.Fatal("wrong Runtime registered operation")
	}
	otherOutput := output
	otherOutput.NodeID = "different"
	if err := repo.RegisterPrivatePublication(ctx, *lease, id, op, otherOutput); err == nil {
		t.Fatal("wrong Runtime output registered operation")
	}
	if err := repo.RegisterPrivatePublication(ctx, *lease, id, op, output); err != nil {
		t.Fatal(err)
	}
	if err := repo.RegisterPrivatePublication(ctx, *lease, id, uuid.NewString(), output); err == nil {
		t.Fatal("operation replaced")
	}
	facts, err = repo.Read(ctx, 7, item.ExecutionID)
	expected := plugin.NewFilePublicationRequest{OperationID: op, Target: target.Target, SizeBytes: output.Size, SHA256: output.SHA256}
	if err != nil || facts.Publication == nil || facts.Publication.ApprovalID != approval.ApprovalID ||
		!reflect.DeepEqual(facts.Publication.Request, expected) || facts.Publication.PreparationDigest != "" {
		t.Fatal(facts, err)
	}
	rawProviderState := []byte("sensitive Provider handle and staging path")
	result := plugin.PreparedNewFile{NewFilePublicationRequest: expected, ProviderState: rawProviderState}
	if err := repo.RecordPrivatePublicationPreparation(ctx, otherLease, result, true); err == nil {
		t.Fatal("wrong lease recorded preparation")
	}
	wrong := result
	wrong.Target = plugin.FileItemPath(11, "outputs/other.gpkg")
	if err := repo.RecordPrivatePublicationPreparation(ctx, *lease, wrong, true); err == nil {
		t.Fatal("retargeted receipt accepted")
	}
	if err := repo.RecordPrivatePublicationPreparation(ctx, *lease, result, confirmed); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordPrivatePublicationPreparation(ctx, *lease, result, true); err == nil {
		t.Fatal("preparation overwritten or promoted")
	}
	var row models.FileExecution
	if err := db.Where("execution_id = ?", item.ExecutionID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	record, err := privateFilePublication(&row)
	if err != nil || !record.PreparationObserved || !bytes.Equal(record.ProviderState, rawProviderState) {
		t.Fatal("original private receipt not retained", err)
	}
	facts, err = repo.Read(ctx, 7, item.ExecutionID)
	digest := ""
	if confirmed {
		digest = providerPreparationDigest(rawProviderState)
	}
	if err != nil || facts.Publication == nil || facts.Publication.PreparationDigest != digest {
		t.Fatal(facts, err)
	}
	encoded, err := json.Marshal(facts)
	if err != nil || bytes.Contains(encoded, []byte("provider_state")) || bytes.Contains(encoded, rawProviderState) {
		t.Fatal("private Provider receipt entered projection", err)
	}
	now, err := execution.DatabaseNow(db)
	if err != nil {
		t.Fatal(err)
	}
	release := client.DevelopFilePublicationObservation{
		AuthorizationID: issued.Authorization.ID, ExecutionID: lease.ExecutionID, Attempt: lease.Attempt,
		ScopeDigest: issued.ScopeDigest, ApprovalID: approval.ApprovalID, OperationID: op,
		Stage: client.DevelopFilePublicationRelease, PreparationDigest: providerPreparationDigest(rawProviderState),
		ObservedAt: now, ExpiresAt: now.Add(10 * time.Second), BusinessDeadline: manifest.Deadline,
	}
	if _, err := repo.ReadPrivatePublication(ctx, 8, item.ExecutionID); err == nil {
		t.Fatal("wrong tenant could read publication")
	}
	before, err := repo.ReadPrivatePublication(ctx, 7, item.ExecutionID)
	if err != nil || before.DispatchState != plugin.NewFileNotPublished || before.PreparationDigest != digest {
		t.Fatal("new operation has an unexpected dispatch state", err)
	}
	if !confirmed {
		if prepared, err := repo.ClaimPrivatePublicationDispatch(ctx, *lease, release); prepared != nil || err == nil {
			t.Fatal("uncertain preparation obtained a dispatch claim")
		}
		return
	}
	for _, tc := range []struct {
		name   string
		change func(*client.DevelopFilePublicationObservation)
	}{
		{"wrong authorization", func(r *client.DevelopFilePublicationObservation) { r.AuthorizationID = "1" }},
		{"wrong execution", func(r *client.DevelopFilePublicationObservation) { r.ExecutionID = uuid.NewString() }},
		{"wrong attempt", func(r *client.DevelopFilePublicationObservation) { r.Attempt++ }},
		{"wrong scope", func(r *client.DevelopFilePublicationObservation) { r.ScopeDigest = strings.Repeat("b", 64) }},
		{"wrong approval", func(r *client.DevelopFilePublicationObservation) { r.ApprovalID = uuid.NewString() }},
		{"wrong operation", func(r *client.DevelopFilePublicationObservation) { r.OperationID = uuid.NewString() }},
		{"wrong preparation", func(r *client.DevelopFilePublicationObservation) { r.PreparationDigest = strings.Repeat("b", 64) }},
		{"prepare stage", func(r *client.DevelopFilePublicationObservation) { r.Stage = client.DevelopFilePublicationPrepare }},
		{"connection present", func(r *client.DevelopFilePublicationObservation) { r.Engine = &commonmodels.Engine{} }},
		{"expired window", func(r *client.DevelopFilePublicationObservation) { r.ExpiresAt = now }},
		{"expired deadline", func(r *client.DevelopFilePublicationObservation) { r.BusinessDeadline = now }},
	} {
		invalid := release
		tc.change(&invalid)
		if prepared, err := repo.ClaimPrivatePublicationDispatch(ctx, *lease, invalid); prepared != nil || err == nil {
			t.Fatal(tc.name, "obtained a dispatch claim")
		}
	}
	if prepared, err := repo.ClaimPrivatePublicationDispatch(ctx, otherLease, release); prepared != nil || err == nil {
		t.Fatal("wrong lease obtained a dispatch claim")
	}
	prepared, err := repo.ClaimPrivatePublicationDispatch(ctx, *lease, release)
	if err != nil || prepared == nil || !reflect.DeepEqual(*prepared, result) {
		t.Fatal("original live release did not yield its confirmed preparation", err)
	}
	if prepared, err := repo.ClaimPrivatePublicationDispatch(ctx, *lease, release); prepared != nil || err == nil {
		t.Fatal("replay obtained a second dispatch claim")
	}
	recovered, err := repo.ReadPrivatePublication(ctx, 7, item.ExecutionID)
	if err != nil || recovered.DispatchState != plugin.NewFileUnknown || recovered.Issued.Authorization.ID != issued.Authorization.ID ||
		recovered.ApprovalID != approval.ApprovalID || recovered.PreparationDigest != digest || !reflect.DeepEqual(recovered.Prepared, result) {
		t.Fatal("original dispatch is not recoverable as unknown", err)
	}
	after, err := repo.Read(ctx, 7, item.ExecutionID)
	if err != nil || !reflect.DeepEqual(after, facts) {
		t.Fatal("dispatch changed the trusted Owner projection", err)
	}

	if err := repo.MarkPrivatePublicationCleaned(ctx, *lease, *prepared); err == nil {
		t.Fatal("unknown publication was marked clean")
	}
	if err := repo.CompletePrivatePublication(ctx, running, *lease, nil); err == nil {
		t.Fatal("unknown publication completed as success")
	}

	outcome := plugin.NewFilePublicationOutcome{State: plugin.NewFilePublished, CleanupPending: true,
		Evidence: &plugin.ContentWriteEvidence{Target: expected.Target, Disposition: "created", SizeBytes: expected.SizeBytes, Committed: true}}
	wrongEvidence := outcome
	wrongEvidence.Evidence = &plugin.ContentWriteEvidence{Target: wrong.Target, Disposition: "created", SizeBytes: expected.SizeBytes, Committed: true}
	if err := repo.RecordPrivatePublicationOutcome(ctx, *lease, *prepared, wrongEvidence); err == nil {
		t.Fatal("outcome for another target was accepted")
	}
	if err := repo.RecordPrivatePublicationOutcome(ctx, otherLease, *prepared, outcome); err == nil {
		t.Fatal("wrong lease recorded outcome")
	}
	if err := repo.RecordPrivatePublicationOutcome(ctx, *lease, *prepared, outcome); err != nil {
		t.Fatal(err)
	}
	observed, err := repo.ReadPrivatePublication(ctx, 7, item.ExecutionID)
	if err != nil || observed.OutcomeObservedAt == nil || observed.OutcomeObservedAt.IsZero() || observed.RecoveryObservedAt != nil {
		t.Fatal("first outcome lost its persisted observation time", observed, err)
	}
	firstObserved := *observed.OutcomeObservedAt

	if err := repo.RecordPrivatePublicationOutcome(ctx, *lease, *prepared, outcome); err == nil {
		t.Fatal("original outcome was replaced")
	}
	if err := repo.MarkPrivatePublicationCleaned(ctx, *lease, wrong); err == nil {
		t.Fatal("foreign preparation marked clean")
	}
	if err := repo.MarkPrivatePublicationCleaned(ctx, *lease, *prepared); err != nil {
		t.Fatal(err)
	}
	final, err := repo.ReadPrivatePublication(ctx, 7, item.ExecutionID)
	outcome.CleanupPending = false
	if err != nil || final.OutcomeObservedAt == nil || !final.OutcomeObservedAt.Equal(firstObserved) || final.DispatchState != plugin.NewFilePublished || !reflect.DeepEqual(final.Outcome, &outcome) {
		t.Fatal("outcome and cleanup state did not survive the original record", err)
	}
	after, err = repo.Read(ctx, 7, item.ExecutionID)
	if err != nil || !reflect.DeepEqual(after, facts) {
		t.Fatal("private outcome changed the trusted Owner projection", err)
	}

	// Original leased evidence can complete the execution atomically.
	if err := repo.CompletePrivatePublication(ctx, running, otherLease, nil); err == nil {
		t.Fatal("wrong lease completed published operation")
	}
	if err := repo.CompletePrivatePublication(ctx, running, *lease, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.CompletePrivatePublication(ctx, running, *lease, nil); err == nil {
		t.Fatal("terminal publication completed twice")
	}
	var terminal execution.TaskExecution
	if err := db.First(&terminal, "execution_id=?", item.ExecutionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordPrivatePublicationRecovery(ctx, 8, item.ExecutionID, *prepared, outcome); err == nil {
		t.Fatal("wrong tenant recovered operation")
	}
	if err := repo.RecordPrivatePublicationRecovery(ctx, 7, item.ExecutionID, wrong, outcome); err == nil {
		t.Fatal("wrong preparation recovered operation")
	}
	if err := repo.RecordPrivatePublicationRecovery(ctx, 7, item.ExecutionID, *prepared, outcome); err != nil {
		t.Fatal(err)
	}
	recoveredTime, err := repo.ReadPrivatePublication(ctx, 7, item.ExecutionID)
	if err != nil || recoveredTime.RecoveryObservedAt == nil || recoveredTime.RecoveryObservedAt.IsZero() || recoveredTime.OutcomeObservedAt == nil || !recoveredTime.OutcomeObservedAt.Equal(firstObserved) {
		t.Fatal("recovery rewrote original observation", recoveredTime, err)
	}
	firstRecovery := *recoveredTime.RecoveryObservedAt
	if err := repo.RecordPrivatePublicationRecovery(ctx, 7, item.ExecutionID, *prepared, outcome); err != nil {
		t.Fatal(err)
	}
	repeated, err := repo.ReadPrivatePublication(ctx, 7, item.ExecutionID)
	if err != nil || repeated.RecoveryObservedAt == nil || !repeated.RecoveryObservedAt.Equal(firstRecovery) {
		t.Fatal("repeated reconciliation renewed observation time", repeated, err)
	}

	regressed := outcome
	regressed.CleanupPending = true
	if err := repo.RecordPrivatePublicationRecovery(ctx, 7, item.ExecutionID, *prepared, regressed); err == nil {
		t.Fatal("recovery regressed cleanup")
	}
	var stillTerminal execution.TaskExecution
	if err := db.First(&stillTerminal, "execution_id=?", item.ExecutionID).Error; err != nil || !reflect.DeepEqual(terminal, stillTerminal) {
		t.Fatal("late recovery changed original terminal", err)
	}
	after, err = repo.Read(ctx, 7, item.ExecutionID)
	if err != nil || !reflect.DeepEqual(after, facts) {
		t.Fatal("late recovery changed authorization projection", err)
	}

	if err := db.Where("execution_id=?", item.ExecutionID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	legacy, err := privateFilePublication(&row)
	if err != nil {
		t.Fatal(err)
	}
	legacy.OutcomeObservedAt = nil
	legacy.RecoveryObservedAt = nil
	rawLegacy, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&row).Update("publication_json", rawLegacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordPrivatePublicationRecovery(ctx, 7, item.ExecutionID, *prepared, outcome); err != nil {
		t.Fatal(err)
	}
	unchanged, err := repo.ReadPrivatePublication(ctx, 7, item.ExecutionID)
	if err != nil || unchanged.OutcomeObservedAt != nil || unchanged.RecoveryObservedAt != nil {
		t.Fatal("historical observation time was invented during replay", unchanged, err)
	}

}
