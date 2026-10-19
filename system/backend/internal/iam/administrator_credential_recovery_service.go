package iam

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	passwordutils "github.com/addp/system/pkg/utils"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

type AdministratorCredentialRecoveryService struct {
	repository *Repository
	cipher     *MFACredentialCipher
	generate   OpaqueTokenGenerator
}

func NewAdministratorCredentialRecoveryService(repository *Repository, cipher *MFACredentialCipher) *AdministratorCredentialRecoveryService {
	return &AdministratorCredentialRecoveryService{repository: repository, cipher: cipher, generate: generateOpaqueToken}
}
func (s *AdministratorCredentialRecoveryService) issue(ctx context.Context, tx *Repository, req *PrivilegedChangeRequest, now time.Time) error {
	token, err := s.generate("addp_pcr_")
	if err != nil {
		return err
	}
	approval, err := tx.administratorChangeApproval(ctx, req.ID)
	if err != nil {
		return err
	}
	reviewer, err := tx.GetPrincipal(ctx, approval.ReviewerPrincipalID)
	if err != nil {
		return err
	}
	if !req.ExpiresAt.After(now) {
		return commonapi.ErrConflict
	}
	expiresAt := now.Add(15 * time.Minute)
	if req.ExpiresAt.Before(expiresAt) {
		expiresAt = req.ExpiresAt
	}
	hash := hashOpaqueToken(token)
	record := &administratorCredentialRecovery{RequestID: req.ID, TicketHash: &hash, ReviewerPrincipalID: reviewer.ID, ReviewerAuthorizationVersion: reviewer.AuthorizationVersion, ExpiresAt: expiresAt}
	if err := tx.createAdministratorRecovery(ctx, record); err != nil {
		return err
	}
	req.RecoveryToken = token
	return nil
}

func (s *AdministratorCredentialRecoveryService) find(ctx context.Context, proof string, enrollment bool) (*administratorCredentialRecovery, error) {
	prefix, column := "addp_pcr_", "ticket_hash"
	if enrollment {
		prefix, column = "addp_pce_", "enrollment_hash"
	}
	if !strings.HasPrefix(proof, prefix) || len(proof) > 256 {
		return nil, commonapi.ErrUnauthorized
	}
	record, err := s.repository.administratorRecoveryByHash(ctx, column, hashOpaqueToken(proof))
	if err != nil {
		return nil, hideMFAStorageError(err)
	}
	return record, nil
}
func (s *AdministratorCredentialRecoveryService) lock(ctx context.Context, tx *Repository, source *administratorCredentialRecovery) (*administratorCredentialRecovery, *PrivilegedChangeRequest, time.Time, error) {
	req, err := tx.GetPrivilegedChangeRequest(ctx, source.RequestID)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	if _, err := tx.lockPrincipalsInOrder(ctx, req.TargetPrincipalID, req.RequestedByPrincipalID, source.ReviewerPrincipalID); err != nil {
		return nil, nil, time.Time{}, err
	}
	req, err = tx.LockPrivilegedChangeRequest(ctx, source.RequestID)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	record, err := tx.lockAdministratorRecovery(ctx, source.RequestID)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	now, err := tx.CurrentDatabaseTime(ctx)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	target, err := tx.GetPrincipal(ctx, req.TargetPrincipalID)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	requester, err := tx.GetPrincipal(ctx, req.RequestedByPrincipalID)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	reviewer, err := tx.GetPrincipal(ctx, record.ReviewerPrincipalID)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	if req.Status != PrivilegedChangeStatusApproved || req.ChangeType != PrivilegedChangePlatformCredentialRecovery || record.ConsumedAt != nil || record.FailedAttempts >= maxMFAFailedAttempts || !record.ExpiresAt.After(now) || !req.ExpiresAt.After(now) || target.AuthorizationVersion != req.TargetAuthorizationVersion || target.Status == PrincipalStatusDeactivated || requester.Status != PrincipalStatusActive || requester.AuthorizationVersion != req.RequesterAuthorizationVersion || reviewer.Status != PrincipalStatusActive || reviewer.AuthorizationVersion != record.ReviewerAuthorizationVersion {
		return nil, nil, time.Time{}, commonapi.ErrUnauthorized
	}
	eligible, err := tx.administratorRecoveryEligible(ctx, req, record)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	if !eligible {
		return nil, nil, time.Time{}, commonapi.ErrUnauthorized
	}
	return record, req, now, nil
}
func (s *AdministratorCredentialRecoveryService) Begin(ctx context.Context, ticket string, audit AuditMetadata) (*IssuedMFAEnrollment, error) {
	source, err := s.find(ctx, ticket, false)
	if err != nil {
		return nil, err
	}
	proof, err := s.generate("addp_pce_")
	if err != nil {
		return nil, err
	}
	var result *IssuedMFAEnrollment
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		record, req, _, err := s.lock(ctx, tx, source)
		if err != nil {
			return err
		}
		if record.TicketHash == nil || *record.TicketHash != hashOpaqueToken(ticket) {
			return commonapi.ErrUnauthorized
		}
		account, err := tx.LockLocalAccountByUserID(ctx, req.TargetPrincipalID)
		if err != nil {
			return err
		}
		if account.Status != LocalAccountStatusActive && account.Status != LocalAccountStatusLocked {
			return commonapi.ErrConflict
		}
		key, err := totp.Generate(totp.GenerateOpts{Issuer: "ADDP", AccountName: account.Username})
		if err != nil {
			return err
		}
		ciphertext, nonce, version, err := s.cipher.EncryptTOTPSecret(req.TargetPrincipalID, key.Secret())
		if err != nil {
			return err
		}
		if err := tx.startAdministratorRecoveryEnrollment(ctx, record, hashOpaqueToken(proof), ciphertext, nonce, version); err != nil {
			return err
		}
		if err := writeAdministratorChangeAudit(ctx, tx, authenticationAuditMetadata(audit, req.TargetPrincipalID), "credential_enrollment_started", req); err != nil {
			return err
		}
		result = &IssuedMFAEnrollment{EnrollmentToken: proof, Secret: key.Secret(), OTPAuthURI: key.URL(), ExpiresAt: record.ExpiresAt}
		return nil
	})
	return result, err
}
func (s *AdministratorCredentialRecoveryService) Complete(ctx context.Context, enrollment, password, code string, audit AuditMetadata) error {
	if len([]rune(password)) < minimumBootstrapPasswordLength || !totpCodePattern.MatchString(code) {
		return commonapi.ErrBadRequest
	}
	source, err := s.find(ctx, enrollment, true)
	if err != nil {
		return err
	}
	passwordHash, err := passwordutils.HashPassword(password)
	if errors.Is(err, bcrypt.ErrPasswordTooLong) {
		return commonapi.ErrBadRequest
	}
	if err != nil {
		return err
	}
	var denied bool
	err = s.repository.Transaction(ctx, func(tx *Repository) error {
		record, req, now, err := s.lock(ctx, tx, source)
		if err != nil {
			return err
		}
		if record.EnrollmentHash == nil || *record.EnrollmentHash != hashOpaqueToken(enrollment) {
			return commonapi.ErrUnauthorized
		}
		account, err := tx.LockLocalAccountByUserID(ctx, req.TargetPrincipalID)
		if err != nil {
			return err
		}
		if account.Status != LocalAccountStatusActive && account.Status != LocalAccountStatusLocked {
			return commonapi.ErrConflict
		}
		if passwordutils.CheckPassword(password, account.PasswordHash) {
			return ErrPasswordUnchanged
		}
		secret, err := s.cipher.DecryptTOTPSecret(&MFACredential{UserID: req.TargetPrincipalID, SecretCiphertext: record.SecretCiphertext, SecretNonce: record.SecretNonce, KeyVersion: record.KeyVersion})
		if err != nil {
			return err
		}
		counter, valid := matchTOTPCode(secret, code, now, nil)
		if !valid {
			denied = true
			if err := tx.recordAdministratorRecoveryFailure(ctx, record); err != nil {
				return err
			}
			return writeAdministratorChangeAudit(ctx, tx, authenticationAuditMetadata(audit, req.TargetPrincipalID), "credential_enrollment_denied", req)
		}
		credential, err := tx.LockActiveMFACredential(ctx, req.TargetPrincipalID)
		if err != nil && !errors.Is(err, commonapi.ErrNotFound) {
			return err
		}
		if err == nil {
			if err := tx.DisableMFACredential(ctx, credential.ID); err != nil {
				return err
			}
		}
		if err := tx.CreateMFACredential(ctx, &MFACredential{UserID: req.TargetPrincipalID, Method: "totp", Status: MFACredentialStatusActive, SecretCiphertext: record.SecretCiphertext, SecretNonce: record.SecretNonce, KeyVersion: record.KeyVersion, LastAcceptedCounter: &counter}); err != nil {
			return err
		}
		if err := tx.ResetLocalAccountPassword(ctx, account.ID, passwordHash, now); err != nil {
			return err
		}
		if _, err := tx.IncrementPrincipalAuthorizationVersion(ctx, req.TargetPrincipalID); err != nil {
			return err
		}
		if _, err := tx.RevokeActiveTokenFamilies(ctx, req.TargetPrincipalID, now, "administrator_credentials_recovered"); err != nil {
			return err
		}
		if _, err := tx.ConsumePendingMFAChallenges(ctx, req.TargetPrincipalID, now); err != nil {
			return err
		}
		if _, err := tx.ConsumePendingMFAEnrollments(ctx, req.TargetPrincipalID, now); err != nil {
			return err
		}
		if _, err := tx.ConsumeActiveContextSelectionTickets(ctx, req.TargetPrincipalID, now); err != nil {
			return err
		}
		if err := tx.completeAdministratorRecovery(ctx, record, req, now); err != nil {
			return err
		}
		return writeAdministratorChangeAudit(ctx, tx, authenticationAuditMetadata(audit, req.TargetPrincipalID), "credentials_recovered", req)
	})
	if err != nil {
		return err
	}
	if denied {
		return fmt.Errorf("%w: recovery proof rejected", commonapi.ErrUnauthorized)
	}
	return nil
}
