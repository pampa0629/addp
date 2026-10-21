package iam

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	passwordutils "github.com/addp/system/pkg/utils"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pquerna/otp/totp"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// A wire fault, not a fabricated Tx.Commit error: consume the real server's
// CommandComplete(COMMIT), then close without delivering that frame to pgx.
// This helper is used only inside the opt-in disposable PostgreSQL test.
type recoveryCommitAckConn struct {
	net.Conn
	reader  *bufio.Reader
	pending []byte
	armed   *atomic.Bool
	dropped *atomic.Bool
}

func (c *recoveryCommitAckConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(c.pending) == 0 {
		header := make([]byte, 5)
		if _, err := io.ReadFull(c.reader, header); err != nil {
			return 0, err
		}
		size := int(binary.BigEndian.Uint32(header[1:]))
		if size < 4 || size > 16*1024*1024 {
			return 0, io.ErrUnexpectedEOF
		}
		payload := make([]byte, size-4)
		if _, err := io.ReadFull(c.reader, payload); err != nil {
			return 0, err
		}
		if header[0] == 'C' && string(payload) == "COMMIT\x00" && c.armed.CompareAndSwap(true, false) {
			c.dropped.Store(true)
			_ = c.Conn.Close()
			return 0, io.ErrUnexpectedEOF
		}
		c.pending = append(header, payload...)
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

func recoveryDBDroppingCommitConfirmation(t *testing.T, dsn string) (*gorm.DB, *atomic.Bool, *atomic.Bool) {
	t.Helper()
	config, err := pgx.ParseConfig(dsn)
	if err != nil || config.TLSConfig != nil {
		t.Fatal("wire confirmation test requires the disposable gate's non-TLS connection")
	}
	armed, dropped := &atomic.Bool{}, &atomic.Bool{}
	dial := config.DialFunc
	config.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &recoveryCommitAckConn{Conn: conn, reader: bufio.NewReader(conn), armed: armed, dropped: dropped}, nil
	}
	sqlDB := sql.OpenDB(stdlib.GetConnector(*config))
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal("open disposable wire-fault connection")
	}
	return db, armed, dropped
}

func exerciseRecoveryResolutionBeforeApply(t *testing.T, ctx context.Context, db *gorm.DB, service *RecoveryService, attemptID int64, principalIDs []int64) {
	t.Helper()
	submission := uuid.New()
	before := snapshotRecoveryState(t, db, principalIDs)
	resolved, err := service.Resolve(ctx, attemptID, submission)
	if err != nil || resolved.State != RecoveryResolutionPrepared {
		t.Fatalf("prepared resolution: %v", err)
	}
	unknown, err := service.Resolve(ctx, attemptID+1000000, submission)
	if err == nil || unknown.State != RecoveryResolutionUnknown {
		t.Fatal("missing attempt was treated as a retry permission")
	}
	blocker := db.WithContext(ctx).Begin()
	defer blocker.Rollback()
	if err := NewRepository(blocker).LockIAMRecoveryTable(ctx); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	unknown, err = service.Resolve(short, attemptID, submission)
	cancel()
	if err == nil || unknown.State != RecoveryResolutionUnknown {
		t.Fatal("lock timeout was treated as rollback or retry permission")
	}
	type outcome struct {
		resolution *RecoveryResolution
		err        error
	}
	done := make(chan outcome, 1)
	go func() { r, e := service.Resolve(ctx, attemptID, submission); done <- outcome{r, e} }()
	waitForRecoveryLockWaiters(t, ctx, db, 1)
	if err := blocker.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-done:
		if out.err != nil || out.resolution.State != RecoveryResolutionPrepared {
			t.Fatalf("resolution after lock release: %v", out.err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("resolution did not finish after lock release")
	}
	assertRecoveryStateUnchanged(t, db, principalIDs, before)
	t.Log("recovery resolution waited for the real PostgreSQL recovery lock; timeout and missing records remained unknown without writes")
}

func exerciseRecoveryCommitConfirmationLoss(t *testing.T, ctx context.Context, dsn string, db *gorm.DB, service *RecoveryService, cipher *MFACredentialCipher, now func() time.Time, previousInputs []RecoveryAdministratorInput) {
	t.Helper()
	prepared, err := service.Prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := service.Validate(ctx, prepared.AttemptID, prepared.Secret)
	if err != nil {
		t.Fatal(err)
	}
	principalIDs := make([]int64, 0, len(validation.Targets))
	for _, target := range validation.Targets {
		principalIDs = append(principalIDs, target.PrincipalID)
	}
	exerciseRecoveryResolutionBeforeApply(t, ctx, db, service, prepared.AttemptID, principalIDs)
	roleAssignments := snapshotRecoveryState(t, db, principalIDs)["system.role_assignments"]
	inputs := make([]RecoveryAdministratorInput, len(previousInputs))
	versions := map[int64]int64{}
	for i, old := range previousInputs {
		key, err := newRecoveryTestTOTPSecret()
		if err != nil {
			t.Fatal(err)
		}
		inputs[i] = recoveryAdministratorTestInput(t, old.RoleKey, old.Password+"-confirmation", key, now())
		inputs[i].PrincipalID = old.PrincipalID
		p, err := NewRepository(db).GetPrincipal(ctx, old.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		versions[old.PrincipalID] = p.AuthorizationVersion
	}
	faultDB, armed, dropped := recoveryDBDroppingCommitConfirmation(t, dsn)
	faultService, err := NewRecoveryService(NewRepository(faultDB), cipher, time.Hour, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	submission := uuid.New()
	input := RecoveryApplyInput{AttemptID: prepared.AttemptID, SubmissionID: submission, Targets: validation.Targets, RecoverySecret: prepared.Secret, Administrators: inputs}
	armed.Store(true)
	result, err := faultService.Apply(ctx, input)
	if err == nil || result != nil || !dropped.Load() {
		t.Fatal("Apply did not lose the actual PostgreSQL COMMIT success confirmation")
	}
	t.Log("PostgreSQL CommandComplete(COMMIT) observed and withheld from the client; Apply returned an error and no result after the real commit")
	resolved, err := service.Resolve(ctx, prepared.AttemptID, submission)
	if err != nil || resolved.State != RecoveryResolutionCompletedThis || resolved.SubmissionID != submission || resolved.CompletedAt == nil || len(resolved.Principals) != len(inputs) {
		t.Fatalf("committed recovery could not be resolved: %v", err)
	}
	other, err := service.Resolve(ctx, prepared.AttemptID, uuid.New())
	if err != nil || other.State != RecoveryResolutionCompletedOther || other.SubmissionID != submission {
		t.Fatal("another submission was falsely reported as the winner")
	}
	ids := make([]int64, 0, len(inputs))
	for _, a := range inputs {
		ids = append(ids, a.PrincipalID)
	}
	committed := snapshotRecoveryState(t, db, ids)
	input.SubmissionID = uuid.New()
	if result, err := service.Apply(ctx, input); !errors.Is(err, commonapi.ErrUnauthorized) || result != nil {
		t.Fatal("completed recovery was applied again after confirmation loss")
	}
	assertRecoveryStateUnchanged(t, db, ids, committed)
	for _, a := range inputs {
		assertResolvedRecoveryCredential(t, ctx, db, cipher, a, versions[a.PrincipalID]+1, resolved)
	}
	if snapshotRecoveryState(t, db, principalIDs)["system.role_assignments"] != roleAssignments {
		t.Fatal("recovery changed role assignments")
	}
	for _, target := range validation.Targets {
		p, e := NewRepository(db).GetPrincipal(ctx, target.PrincipalID)
		if e != nil || p.Status != target.PrincipalStatus {
			t.Fatal("recovery changed principal suspension")
		}
	}
	// Historical receipt lookup deliberately does not re-evaluate current roles,
	// credentials, suspension or authorization versions.
	if _, err := NewRepository(db).IncrementPrincipalAuthorizationVersion(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	again, err := service.Resolve(ctx, prepared.AttemptID, submission)
	if err != nil || again.State != RecoveryResolutionCompletedThis || again.AuthorizationVersions[ids[0]] != versions[ids[0]]+1 {
		t.Fatal("later identity change rewrote the recovery receipt")
	}
	// Expiry is observed without consuming or renewing the prepared Secret.
	expiredSecret := "addp_ir_expired-confirmation-test"
	hash := hashOpaqueToken(expiredSecret)
	expired := &IAMRecoveryAttempt{SecretHash: &hash, Status: IAMRecoveryStatusPrepared,
		PreparedAt: time.Now().UTC().Add(-2 * time.Hour), ExpiresAt: time.Now().UTC().Add(-time.Hour)}
	if err := NewRepository(db).Transaction(ctx, func(tx *Repository) error {
		if err := tx.CreateIAMRecoveryAttempt(ctx, expired); err != nil {
			return err
		}
		return writeRecoveryAudit(ctx, tx, "iam.recovery.prepared", expired.ID, nil, map[string]any{"administrator_count": len(inputs)})
	}); err != nil {
		t.Fatal(err)
	}
	beforeExpiry := snapshotRecoveryState(t, db, ids)
	expiry, err := service.Resolve(ctx, expired.ID, uuid.New())
	if err != nil || expiry.State != RecoveryResolutionExpired {
		t.Fatal("expired recovery was treated as retryable")
	}
	input.AttemptID, input.RecoverySecret = expired.ID, expiredSecret
	if r, e := service.Apply(ctx, input); r != nil || !errors.Is(e, commonapi.ErrUnauthorized) {
		t.Fatal("expired recovery was applied")
	}
	assertRecoveryStateUnchanged(t, db, ids, beforeExpiry)
	t.Log("independent connection resolved the original winning submission and all four credentials; retry changed none of the ten persisted state sets and historical receipt survived a later version change")
}

func newRecoveryTestTOTPSecret() (string, error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "ADDP test", AccountName: "commit-confirmation"})
	if err != nil {
		return "", err
	}
	return key.Secret(), nil
}

func assertResolvedRecoveryCredential(t *testing.T, ctx context.Context, db *gorm.DB, cipher *MFACredentialCipher, input RecoveryAdministratorInput, wantVersion int64, receipt *RecoveryResolution) {
	t.Helper()
	var account LocalAccount
	if err := db.Where("user_id = ?", input.PrincipalID).Take(&account).Error; err != nil {
		t.Fatal(err)
	}
	if !passwordutils.CheckPassword(input.Password, account.PasswordHash) {
		t.Fatal("confirmation-lost recovery password did not persist")
	}
	credential, err := NewRepository(db).LockActiveMFACredential(ctx, input.PrincipalID)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := cipher.DecryptTOTPSecret(credential)
	if err != nil || secret != input.TOTPSecret {
		t.Fatal("confirmation-lost recovery MFA did not persist")
	}
	principal, err := NewRepository(db).GetPrincipal(ctx, input.PrincipalID)
	if err != nil {
		t.Fatal(err)
	}
	if principal.AuthorizationVersion != wantVersion || receipt.AuthorizationVersions[input.PrincipalID] != wantVersion || receipt.Principals[input.PrincipalID] != input.RoleKey {
		t.Fatal("confirmation-lost recovery did not advance each holder exactly once")
	}
	var disabled, activeSessions int64
	if err := db.Model(&MFACredential{}).Where("user_id = ? AND status = 'disabled'", input.PrincipalID).Count(&disabled).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("system.refresh_token_families").Where("principal_id = ? AND revoked_at IS NULL", input.PrincipalID).Count(&activeSessions).Error; err != nil {
		t.Fatal(err)
	}
	if disabled != 2 || activeSessions != 0 {
		t.Fatal("confirmation-lost recovery duplicated credential replacement or retained old sessions")
	}
	assertRecoveryPendingFactsConsumed(t, db, input.PrincipalID)
}
