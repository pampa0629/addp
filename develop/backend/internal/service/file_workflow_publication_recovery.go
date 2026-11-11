package service

import (
	"context"
	"errors"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/develop/backend/internal/repository"
)

type filePublicationRecoveryAuthority interface {
	RecoverDevelopFilePublication(context.Context, client.IssuedDevelopFileAuthorization, string, plugin.NewFilePublicationRequest) (*client.DevelopFilePublicationRecoveryAccess, error)
}

func (f *privateFileWorkflowExecutor) recoverPrivatePublications(ctx context.Context) error {
	ctx, stop := context.WithTimeout(ctx, 30*time.Second)
	defer stop()
	f.mu.Lock()
	cursor := f.publicationCursor
	f.mu.Unlock()
	rows, err := f.repository.ListPrivatePublicationRecoveries(ctx, cursor)
	if err != nil {
		return err
	}
	var failures []error
	for _, row := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := f.recoverPrivatePublication(ctx, row); err != nil {
			failures = append(failures, err)
		}
		f.mu.Lock()
		f.publicationCursor = row.ExecutionID
		f.mu.Unlock()
	}
	if len(rows) < 100 {
		f.mu.Lock()
		f.publicationCursor = ""
		f.mu.Unlock()
	}
	return errors.Join(failures...)
}

func (f *privateFileWorkflowExecutor) recoverPrivatePublication(ctx context.Context, id repository.PrivatePublicationIdentity) error {
	original, err := f.repository.ReadPrivatePublication(ctx, id.TenantID, id.ExecutionID)
	if err != nil {
		return errPrivatePublication
	}
	outcome := original.Outcome
	if original.RecoveryOutcome != nil {
		outcome = original.RecoveryOutcome
	}
	if outcome != nil && !outcome.CleanupPending {
		return nil
	}
	if original.PreparationDigest == "" || f.publication == nil {
		return errPrivatePublication
	}
	provider, ok := f.provider.(plugin.NewFilePublicationProvider)
	if !ok {
		return errPrivatePublication
	}
	authority, ok := f.publication(id.TenantID).(filePublicationRecoveryAuthority)
	if !ok {
		return errPrivatePublication
	}
	access, err := authority.RecoverDevelopFilePublication(ctx, original.Issued, original.ApprovalID, original.Prepared.NewFilePublicationRequest)
	if err != nil || access == nil || access.Engine == nil {
		return errPrivatePublication
	}
	// The SDK already binds history, target version and current machine identity.
	// A claimed dispatch additionally needs the original System release proof.
	if original.DispatchState != plugin.NewFileNotPublished &&
		(access.History.ReleasedAt == nil || access.History.PreparationDigest != original.PreparationDigest) {
		return errPrivatePublication
	}
	observe, stop := context.WithDeadline(ctx, access.ExpiresAt)
	defer stop()
	if observe.Err() != nil {
		return errPrivatePublication
	}
	connection := plugin.ConnectionInfo(access.Engine.ConnectionInfo)
	if outcome == nil {
		if original.DispatchState != plugin.NewFileUnknown {
			return errPrivatePublication
		}
		resolved, err := provider.ResolveNewFile(observe, connection, original.Prepared)
		if err != nil || resolved.State != plugin.NewFilePublished || resolved.Validate(original.Prepared) != nil {
			return errPrivatePublication
		}
		outcome = &resolved
	}
	// Once the RPC has returned, a late observation may be stored independently
	// of both the business lease and the observation deadline.
	if err := f.repository.RecordPrivatePublicationRecovery(ctx, id.TenantID, id.ExecutionID, original.Prepared, *outcome); err != nil {
		return errPrivatePublication
	}
	if outcome.CleanupPending {
		if observe.Err() != nil {
			return errPrivatePublication
		}
		if err := provider.CleanupNewFile(observe, connection, original.Prepared, outcome.State); err != nil {
			return errPrivatePublication
		}
		cleaned := *outcome
		cleaned.CleanupPending = false
		return f.repository.RecordPrivatePublicationRecovery(ctx, id.TenantID, id.ExecutionID, original.Prepared, cleaned)
	}
	return nil
}
