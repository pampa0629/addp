package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/production"
	"github.com/addp/common/schema"
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
	facts, err := repo.CreatePending(ctx, item, sources, []execution.DevelopFileTargetIntent{*target})
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
	waiting := fileAdmissionFixture(facts)
	waiting.AuthorizationID, waiting.State = issued.Authorization.ID, "awaiting_approval"
	if err := repo.ApplyAdmission(ctx, 7, item.ExecutionID, waiting); err != nil {
		t.Fatal(err)
	}
	if _, lease, err := NewExecutionRepository(db).ClaimNext("workflow", ctx, "publication-fixture", time.Now(), time.Minute, nil); err != nil || lease != nil {
		t.Fatal("unapproved save was claimable", err)
	}
	// A ready receipt without the original single-use approval cannot activate
	// the queue. This case also runs through the standard PostgreSQL T2 gate.
	missingApproval := fileAdmissionFixture(facts)
	missingApproval.AuthorizationID = issued.Authorization.ID
	if err := repo.ApplyAdmission(ctx, 7, item.ExecutionID, missingApproval); err == nil {
		t.Fatal("missing save approval accepted")
	}
	approval := fileAdmissionFixture(facts)
	approval.AuthorizationID = issued.Authorization.ID
	approval.ApprovalIDs = []string{uuid.NewString()}
	if err := repo.ApplyAdmission(ctx, 7, item.ExecutionID, approval); err != nil {
		t.Fatal(err)
	}
	running, lease, err := NewExecutionRepository(db).ClaimNext("workflow", ctx, "publication-fixture", time.Now(), time.Minute, nil)
	if err != nil || lease == nil || running.ExecutionID != item.ExecutionID {
		t.Fatal(running, lease, err)
	}
	id, op, directory := uuid.NewString(), uuid.NewString(), t.TempDir()
	output := plugin.WorkflowPrivateFile{NodeID: target.NodeID, Format: "geopackage", Size: 8192, SHA256: strings.Repeat("a", 64)}
	if err := repo.RegisterPrivatePublication(ctx, *lease, id, op, output); err == nil {
		t.Fatal("operation registered before Runtime")
	}
	if err := repo.RecordPrivateInputDirectory(ctx, *lease, "publication-fixture", directory); err != nil {
		t.Fatal(err)
	}
	manifest := plugin.WorkflowPrivateFileManifest{SchemaVersion: plugin.WorkflowPrivateFilesAPIV2,
		TenantID: 7, ExecutionID: item.ExecutionID, Attempt: lease.Attempt, AuthorizationID: authorizationID, ScopeDigest: issued.ScopeDigest,
		Deadline: running.StartedAt.Add(time.Minute), WorkflowJSON: `{"tasks":[]}`, Outputs: []string{target.NodeID}}
	var reads []PrivateFileSourceRead
	for _, source := range facts.Intent.Sources {
		set, _ := plugin.NewQueryReadSet(source.Target)
		reads = append(reads, PrivateFileSourceRead{EngineVersion: 2, NodeID: source.NodeID, ReadSet: set})
		manifest.Inputs = append(manifest.Inputs, plugin.WorkflowPrivateFile{NodeID: source.NodeID, Format: "geopackage", Size: 123, SHA256: strings.Repeat("a", 64)})
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
	if err != nil || len(facts.Publications) == 0 || facts.Publications[0].ApprovalID != approval.ApprovalIDs[0] ||
		!reflect.DeepEqual(facts.Publications[0].Request, expected) || facts.Publications[0].PreparationDigest != "" {
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
	records, err := privateFilePublications(&row)
	if err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
	record := &records[0]
	if err != nil || !record.PreparationObserved || !bytes.Equal(record.ProviderState, rawProviderState) {
		t.Fatal("original private receipt not retained", err)
	}
	facts, err = repo.Read(ctx, 7, item.ExecutionID)
	digest := ""
	if confirmed {
		digest = providerPreparationDigest(rawProviderState)
	}
	if err != nil || len(facts.Publications) == 0 || facts.Publications[0].PreparationDigest != digest {
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
		ScopeDigest: issued.ScopeDigest, ApprovalID: approval.ApprovalIDs[0], OperationID: op,
		Stage: client.DevelopFilePublicationRelease, PreparationDigest: providerPreparationDigest(rawProviderState),
		ObservedAt: now, ExpiresAt: now.Add(10 * time.Second), BusinessDeadline: manifest.Deadline,
	}
	if _, err := repo.ReadPrivatePublication(ctx, 8, item.ExecutionID, op); err == nil {
		t.Fatal("wrong tenant could read publication")
	}
	before, err := repo.ReadPrivatePublication(ctx, 7, item.ExecutionID, op)
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
	recovered, err := repo.ReadPrivatePublication(ctx, 7, item.ExecutionID, op)
	if err != nil || recovered.DispatchState != plugin.NewFileUnknown || recovered.Issued.Authorization.ID != issued.Authorization.ID ||
		recovered.ApprovalID != approval.ApprovalIDs[0] || recovered.PreparationDigest != digest || !reflect.DeepEqual(recovered.Prepared, result) {
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
	if err := repo.RecordPrivatePublicationOutcome(ctx, *lease, *prepared, wrongEvidence, false); err == nil {
		t.Fatal("outcome for another target was accepted")
	}
	if err := repo.RecordPrivatePublicationOutcome(ctx, otherLease, *prepared, outcome, false); err == nil {
		t.Fatal("wrong lease recorded outcome")
	}
	if err := repo.RecordPrivatePublicationOutcome(ctx, *lease, *prepared, outcome, false); err != nil {
		t.Fatal(err)
	}
	observed, err := repo.ReadPrivatePublication(ctx, 7, item.ExecutionID, op)
	if err != nil || observed.OutcomeObservedAt == nil || observed.OutcomeObservedAt.IsZero() || observed.RecoveryObservedAt != nil {
		t.Fatal("first outcome lost its persisted observation time", observed, err)
	}
	firstObserved := *observed.OutcomeObservedAt
	claim, err := repo.ClaimProductionAuthorization(ctx, time.Now().Add(time.Second))
	if err != nil || claim == nil || claim.Reference.ResultID != op || claim.State.Attempts != 1 {
		t.Fatal("published result did not atomically create delivery", claim, err)
	}
	newer, err := repo.ClaimProductionAuthorization(ctx, time.Unix(claim.State.NextAttemptUnix, 0))
	if err != nil || newer == nil || newer.State.Attempts != 2 {
		t.Fatal("continuation did not use bounded retry", newer, err)
	}
	lateState := claim.State
	lateState.Status, lateState.GrantRequestID = "issued", uuid.NewString()
	if err := repo.RecordProductionAuthorization(ctx, *claim, lateState); err != nil {
		t.Fatal(err)
	}
	terminalDelivery := newer.State
	terminalDelivery.Status, terminalDelivery.Code = "denied", "source_access_denied"
	if err := repo.RecordProductionAuthorization(ctx, *newer, terminalDelivery); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordProductionAuthorization(ctx, *claim, lateState); err != nil {
		t.Fatal(err)
	}
	if next, err := repo.ClaimProductionAuthorization(ctx, time.Now().Add(time.Hour)); err != nil || next != nil {
		t.Fatal("stale delivery revived a terminal result", next, err)
	}
	production, err := repo.ReadPrivateProduction(ctx, 7, item.ExecutionID, op)
	if err != nil || production.OperationID != op || production.Attempt != 1 || !production.CommittedAt.Equal(firstObserved) || len(production.SourceReads) != len(facts.Intent.Sources) {
		t.Fatal("committed production lost original operation/read evidence", production, err)
	}
	for _, lookup := range []struct {
		tenant uint
		result string
	}{{8, op}, {7, uuid.NewString()}} {
		if _, err := repo.ReadPrivateProduction(ctx, lookup.tenant, item.ExecutionID, lookup.result); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatal("foreign tenant/result acquired receipt", err)
		}
	}

	if err := repo.RecordPrivatePublicationOutcome(ctx, *lease, *prepared, outcome, false); err == nil {
		t.Fatal("original outcome was replaced")
	}
	if err := repo.MarkPrivatePublicationCleaned(ctx, *lease, wrong); err == nil {
		t.Fatal("foreign preparation marked clean")
	}
	if err := repo.MarkPrivatePublicationCleaned(ctx, *lease, *prepared); err != nil {
		t.Fatal(err)
	}
	final, err := repo.ReadPrivatePublication(ctx, 7, item.ExecutionID, op)
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
	for _, status := range []string{"failed", "cancelled", "timeout", "success"} {
		if err := db.Model(&execution.TaskExecution{}).Where("execution_id=?", item.ExecutionID).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		production, err := repo.ReadPrivateProduction(ctx, 7, item.ExecutionID, op)
		if err != nil || !production.CommittedAt.Equal(firstObserved) {
			t.Fatal("parent terminal status erased committed production", status, err)
		}
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
	recoveredTime, err := repo.ReadPrivatePublication(ctx, 7, item.ExecutionID, op)
	if err != nil || recoveredTime.RecoveryObservedAt == nil || recoveredTime.RecoveryObservedAt.IsZero() || recoveredTime.OutcomeObservedAt == nil || !recoveredTime.OutcomeObservedAt.Equal(firstObserved) {
		t.Fatal("recovery rewrote original observation", recoveredTime, err)
	}
	firstRecovery := *recoveredTime.RecoveryObservedAt
	if err := repo.RecordPrivatePublicationRecovery(ctx, 7, item.ExecutionID, *prepared, outcome); err != nil {
		t.Fatal(err)
	}
	repeated, err := repo.ReadPrivatePublication(ctx, 7, item.ExecutionID, op)
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
	archived, err := privateFilePublications(&row)
	legacy := &archived[0]
	if err != nil {
		t.Fatal(err)
	}
	// Resolve-only recovery may supply the sole committed result after the
	// original dispatch acknowledgement was lost; retain that first time.
	legacy.Outcome = nil
	legacy.OutcomeObservedAt = nil
	recoveryOnly, err := json.Marshal(archived)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&row).Update("publication_json", recoveryOnly).Error; err != nil {
		t.Fatal(err)
	}
	late, err := repo.ReadPrivateProduction(ctx, 7, item.ExecutionID, op)
	if err != nil || !late.CommittedAt.Equal(firstRecovery) {
		t.Fatal("resolve-only committed result was lost", late, err)
	}
	legacy.RecoveryObservedAt = nil
	legacy.InitialAuthorization = nil
	rawLegacy, err := json.Marshal(archived)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&row).Update("publication_json", rawLegacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordPrivatePublicationRecovery(ctx, 7, item.ExecutionID, *prepared, outcome); err != nil {
		t.Fatal(err)
	}
	unchanged, err := repo.ReadPrivatePublication(ctx, 7, item.ExecutionID, op)
	if err != nil || unchanged.OutcomeObservedAt != nil || unchanged.RecoveryObservedAt != nil {
		t.Fatal("historical observation time was invented during replay", unchanged, err)
	}
	if _, err := repo.ReadPrivateProduction(ctx, 7, item.ExecutionID, op); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("historical missing observation became a new production", err)
	}

}

func TestMySQLFileProductionDelivery(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_DEVELOP_FILE_DELIVERY") != "1" {
		t.Skip("requires the isolated Develop MySQL gate")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{}, "develop", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	runFileProductionDelivery(t, db)
	runOriginalFileCutover(t, db)
}

// Two pending results share one row. After the earliest is claimed and
// finalized, the next claim selects the other without reviving the first.
func runFileProductionDelivery(t *testing.T, db *gorm.DB) {
	now := time.Now().UTC().Truncate(time.Second)
	var records privatePublications
	var approvals []string
	for _, name := range []string{"main.gpkg", "members.gpkg"} {
		target := plugin.FileItemPath(5, name)
		approval := uuid.NewString()
		prepared := []byte(name)
		request := plugin.NewFilePublicationRequest{OperationID: uuid.NewString(), Target: target, SizeBytes: 100, SHA256: strings.Repeat("d", 64)}
		outcome := plugin.NewFilePublicationOutcome{State: plugin.NewFilePublished, Evidence: &plugin.ContentWriteEvidence{Target: target, Disposition: "created", SizeBytes: 100, Committed: true}}
		record := privatePublicationRecord{
			Facts:               client.DevelopFilePublicationFacts{Request: request, ApprovalID: approval, PreparationDigest: providerPreparationDigest(prepared)},
			PreparationObserved: true, ProviderState: prepared, DispatchReleasedAt: &now, Outcome: &outcome, OutcomeObservedAt: &now,
			InitialAuthorization: &production.DeliveryState{SchemaVersion: publicationAuthorizationSchema, Status: "pending", NextAttemptUnix: now.Unix()},
		}
		records = append(records, record)
		approvals = append(approvals, approval)
	}
	publications, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := json.Marshal(client.DevelopFileAdmissionObservation{ApprovalIDs: approvals})
	if err != nil {
		t.Fatal(err)
	}
	row := models.FileExecution{ExecutionID: uuid.NewString(), TenantID: 7, ActorPrincipalID: 31, ActorMembershipID: 41, AuthorizationVersion: 1, AdmissionState: "ready", ConfigJSON: json.RawMessage("{}"), IntentJSON: json.RawMessage("{}"), AdmissionJSON: admission, PublicationJSON: publications, Version: 1}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Delete(&row).Error; err != nil {
			t.Error(err)
		}
	}()
	repo := NewFileExecutionRepository(db)
	for _, record := range records {
		claim, err := repo.ClaimProductionAuthorization(t.Context(), now)
		if err != nil || claim == nil || claim.Reference.ExecutionID != row.ExecutionID || claim.Reference.ResultID != record.Facts.Request.OperationID {
			t.Fatal("wrong per-file claim", claim, err)
		}
		state := claim.State
		state.Status, state.GrantRequestID = "issued", uuid.NewString()
		if err := repo.RecordProductionAuthorization(t.Context(), *claim, state); err != nil {
			t.Fatal(err)
		}
	}
	if claim, err := repo.ClaimProductionAuthorization(t.Context(), now.Add(time.Hour)); err != nil || claim != nil {
		t.Fatal("completed file requeued", claim, err)
	}
}

func runOriginalFileCutover(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := t.Context()
	oldRow := func(journal string) models.FileExecution {
		row := models.FileExecution{ExecutionID: uuid.NewString(), TenantID: 7, ActorPrincipalID: 31, ActorMembershipID: 41, AuthorizationVersion: 1, AdmissionState: "ready", ConfigJSON: json.RawMessage("{}"), IntentJSON: json.RawMessage(`{"schema_version":"develop.file-intent/v1"}`), PublicationJSON: json.RawMessage(journal), Version: 1}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Delete(&row).Error; err != nil {
				t.Error(err)
			}
		})
		return row
	}
	unknown := oldRow(`{"dispatch_released_at":"2020-01-01T00:00:00Z"}`)
	pending := oldRow(`{"outcome":{"State":"published"},"initial_authorization":{"status":"pending"}}`)
	check := func() error { return schema.CheckDevelopFileCutover(ctx, db.Statement.ConnPool, db.Dialector.Name()) }
	if err := check(); err == nil || !strings.Contains(err.Error(), unknown.ExecutionID) || !strings.Contains(err.Error(), pending.ExecutionID) {
		t.Fatal("original unresolved or ungranted execution missing", err)
	}
	// A completed observation from the old owner remains read-only forever.
	settled := `{"dispatch_released_at":"2020-01-01T00:00:00Z","recovery_outcome":{"State":"published","CleanupPending":false},"initial_authorization":{"status":"issued"}}`
	if err := db.Model(&unknown).UpdateColumn("publication_json", settled).Error; err != nil {
		t.Fatal(err)
	}
	if err := check(); err == nil || strings.Contains(err.Error(), unknown.ExecutionID) || !strings.Contains(err.Error(), pending.ExecutionID) {
		t.Fatal("did not distinguish independent old records", err)
	}
	if err := db.Model(&pending).UpdateColumn("publication_json", `{"outcome":{"State":"published"},"initial_authorization":{"status":"denied"}}`).Error; err != nil {
		t.Fatal(err)
	}
	var before, after models.FileExecution
	if err := db.First(&before, "execution_id=?", unknown.ExecutionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := check(); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&after, "execution_id=?", unknown.ExecutionID).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("cutover altered the original evidence", err)
	}
}
